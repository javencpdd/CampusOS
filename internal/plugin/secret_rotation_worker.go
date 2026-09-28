package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const secretRotationOperationKind = "plugin.secret_rewrap.background"

var (
	ErrSecretRotationConfig    = errors.New("plugin secret rotation worker configuration is invalid")
	ErrSecretRotationLease     = errors.New("plugin secret rotation lease is unavailable")
	ErrSecretRotationAudit     = errors.New("plugin secret rotation audit is unavailable")
	ErrSecretRotationInventory = errors.New("plugin secret rotation inventory is unavailable")
)

// SecretRotationInventory exposes only IDs for the cross-plugin cursor. Each
// pass continues the process-local cursor. At the end of inventory it wraps
// to zero, so persistent failures cannot starve higher plugin IDs. This is not a database-wide snapshot or a key
// retirement certificate.
type SecretRotationInventory interface {
	SecretRewrapInventory
	ListActiveSecretRewrapPluginIDs(context.Context, string, int64, int) ([]int64, error)
}

type SecretRotationAuditStore interface {
	StartOperation(context.Context, reliability.Operation) (*reliability.Operation, error)
	UpdateOperation(context.Context, reliability.Operation) error
}

type SecretRotationGuard interface {
	Check(context.Context) error
	Release(context.Context) error
}

type SecretRotationLocker interface {
	TryAcquire(context.Context) (SecretRotationGuard, bool, error)
}

type SecretRotationConfig struct {
	Enabled             bool
	Interval            time.Duration
	BatchSize           int
	MaxBatchesPerPlugin int
	MaxPluginsPerRun    int
}

type SecretRotationRunResult struct {
	Disabled         bool `json:"disabled"`
	LeaseBusy        bool `json:"lease_busy"`
	ScannedPlugins   int  `json:"scanned_plugins"`
	SucceededPlugins int  `json:"succeeded_plugins"`
	FailedPlugins    int  `json:"failed_plugins"`
	Rewrapped        int  `json:"rewrapped"`
}

type SecretRotationWorker struct {
	mu            sync.Mutex
	afterPluginID int64
	cfg           SecretRotationConfig
	service       *SecretService
	inventory     SecretRotationInventory
	audit         SecretRotationAuditStore
	locker        SecretRotationLocker
}

func NewSecretRotationWorker(service *SecretService, inventory SecretRotationInventory, audit SecretRotationAuditStore, locker SecretRotationLocker, cfg SecretRotationConfig) (*SecretRotationWorker, error) {
	if !cfg.Enabled {
		return &SecretRotationWorker{cfg: cfg}, nil
	}
	if service == nil || !service.Available() || service.ActiveKeyID() == "" || inventory == nil || audit == nil || locker == nil ||
		cfg.Interval < time.Second || cfg.Interval > 24*time.Hour || cfg.BatchSize < 1 || cfg.BatchSize > 500 ||
		cfg.MaxBatchesPerPlugin < 1 || cfg.MaxBatchesPerPlugin > 10 || cfg.MaxPluginsPerRun < 1 || cfg.MaxPluginsPerRun > 100 {
		return nil, ErrSecretRotationConfig
	}
	return &SecretRotationWorker{cfg: cfg, service: service, inventory: inventory, audit: audit, locker: locker}, nil
}

// Run starts only when explicitly enabled. It waits one interval before the
// first round, so server startup never implicitly rewrites shared rows.
func (w *SecretRotationWorker) Run(ctx context.Context) error {
	if w == nil {
		return ErrSecretRotationConfig
	}
	if !w.cfg.Enabled {
		return nil
	}
	if ctx == nil {
		return ErrSecretRotationConfig
	}
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// RunOnce is one bounded, leased cross-plugin pass. Per-plugin row failures
// are audited and isolated; lease or audit infrastructure failures stop the
// pass. No Secret name, value, ciphertext or DSN enters the result or audit.
func (w *SecretRotationWorker) RunOnce(ctx context.Context) (result SecretRotationRunResult, err error) {
	if w == nil {
		return result, ErrSecretRotationConfig
	}
	if !w.cfg.Enabled {
		result.Disabled = true
		return result, nil
	}
	if ctx == nil {
		return result, ErrSecretRotationConfig
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	guard, acquired, acquireErr := w.locker.TryAcquire(ctx)
	if acquireErr != nil {
		return result, ErrSecretRotationLease
	}
	if !acquired {
		result.LeaseBusy = true
		return result, nil
	}
	if guard == nil {
		return result, ErrSecretRotationLease
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := guard.Release(releaseCtx); releaseErr != nil && err == nil {
			err = ErrSecretRotationLease
		}
	}()

	activeKeyID := w.service.ActiveKeyID()
	afterPluginID := w.afterPluginID
	for result.ScannedPlugins < w.cfg.MaxPluginsPerRun {
		if checkErr := guard.Check(ctx); checkErr != nil {
			return result, ErrSecretRotationLease
		}
		limit := min(25, w.cfg.MaxPluginsPerRun-result.ScannedPlugins)
		pluginIDs, listErr := w.inventory.ListActiveSecretRewrapPluginIDs(ctx, activeKeyID, afterPluginID, limit)
		if listErr != nil || len(pluginIDs) > limit {
			return result, ErrSecretRotationInventory
		}
		if len(pluginIDs) == 0 {
			w.afterPluginID = 0
			break
		}
		for _, pluginID := range pluginIDs {
			if pluginID <= afterPluginID {
				return result, ErrSecretRotationInventory
			}
			afterPluginID = pluginID
			w.afterPluginID = pluginID
			result.ScannedPlugins++
			if checkErr := guard.Check(ctx); checkErr != nil {
				return result, ErrSecretRotationLease
			}
			pluginResult, processErr := w.processPlugin(ctx, guard, pluginID, activeKeyID)
			result.Rewrapped += pluginResult.rewrapped
			if processErr != nil {
				return result, processErr
			}
			if pluginResult.failed {
				result.FailedPlugins++
			} else {
				result.SucceededPlugins++
			}
		}
	}
	return result, nil
}

type secretRotationPluginResult struct {
	rewrapped int
	failed    bool
}

type secretRotationAuditDetails struct {
	PluginID    int64  `json:"plugin_id"`
	ActiveKeyID string `json:"active_key_id"`
	BatchSize   int    `json:"batch_size"`
	Batches     int    `json:"batches"`
	Selected    int    `json:"selected"`
	Rewrapped   int    `json:"rewrapped"`
	Remaining   *int64 `json:"remaining,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
}

func (w *SecretRotationWorker) processPlugin(ctx context.Context, guard SecretRotationGuard, pluginID int64, activeKeyID string) (secretRotationPluginResult, error) {
	var result secretRotationPluginResult
	details := secretRotationAuditDetails{PluginID: pluginID, ActiveKeyID: activeKeyID, BatchSize: w.cfg.BatchSize}
	audit, err := w.audit.StartOperation(ctx, reliability.Operation{
		Kind: secretRotationOperationKind, SubjectType: "plugin", SubjectID: fmt.Sprint(pluginID),
		Status: reliability.OperationRunning, ActorID: "system:secret-rotation", Details: secretRotationDetails(details),
	})
	if err != nil || audit == nil {
		return result, ErrSecretRotationAudit
	}
	for batchIndex := 0; batchIndex < w.cfg.MaxBatchesPerPlugin; batchIndex++ {
		if err := guard.Check(ctx); err != nil {
			details.ErrorCode = "lease_lost"
			break
		}
		batch, batchErr := RewrapActiveSecretBatch(ctx, w.service, w.inventory, pluginID, w.cfg.BatchSize)
		details.Batches++
		details.Selected += batch.Selected
		details.Rewrapped += batch.Rewrapped
		result.rewrapped += batch.Rewrapped
		if batchErr != nil {
			details.ErrorCode = "secret_rewrap_failed"
			break
		}
		details.Remaining = &batch.Remaining
		if batch.Remaining == 0 {
			break
		}
		if batch.Selected == 0 || batch.Rewrapped == 0 {
			details.ErrorCode = "inventory_inconsistent"
			break
		}
	}
	if details.ErrorCode != "" {
		result.failed = true
		// A failure may follow earlier successful row commits. The audit records
		// the partial count and best-effort remaining inventory without payloads.
		details.Remaining = nil
		finalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if remaining, countErr := w.inventory.CountActiveSecretRewrapCandidates(finalCtx, pluginID, activeKeyID); countErr == nil {
			details.Remaining = &remaining
		}
		cancel()
		audit.Status = reliability.OperationFailed
		audit.Error = "secret rotation failed"
	} else {
		audit.Status = reliability.OperationSucceeded
	}
	audit.Details = secretRotationDetails(details)
	finalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.audit.UpdateOperation(finalCtx, *audit); err != nil {
		return result, ErrSecretRotationAudit
	}
	if details.ErrorCode == "lease_lost" {
		return result, ErrSecretRotationLease
	}
	return result, nil
}

func secretRotationDetails(details secretRotationAuditDetails) json.RawMessage {
	encoded, _ := json.Marshal(details)
	return encoded
}
