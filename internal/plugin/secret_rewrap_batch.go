package plugin

import (
	"context"
	"errors"
)

// SecretRewrapCandidate identifies an active row without reading its encrypted
// value. The row ID is used to reject a replacement between inventory and use.
type SecretRewrapCandidate struct {
	ID          int64
	PluginID    int64
	OwnerUserID *int64
	SecretName  string
}

type SecretRewrapInventory interface {
	CountActiveSecretRewrapCandidates(context.Context, int64, string) (int64, error)
	ListActiveSecretRewrapCandidates(context.Context, int64, string, int) ([]SecretRewrapCandidate, error)
}

type SecretRewrapBatchResult struct {
	Selected  int   `json:"selected"`
	Rewrapped int   `json:"rewrapped"`
	Remaining int64 `json:"remaining"`
}

var (
	ErrSecretRewrapInventory = errors.New("plugin secret rewrap inventory is unavailable")
	ErrSecretRewrapFailed    = errors.New("plugin secret rewrap failed")
)

// RewrapActiveSecretBatch processes at most limit current rows for one plugin.
// Repeated calls make progress because the inventory excludes the active key.
// A concurrent update of a selected identity fails rather than touching its
// replacement. Neither results nor errors contain secret names or key material.
func RewrapActiveSecretBatch(ctx context.Context, service *SecretService, inventory SecretRewrapInventory, pluginID int64, limit int) (SecretRewrapBatchResult, error) {
	var result SecretRewrapBatchResult
	if service == nil || !service.Available() || inventory == nil || pluginID <= 0 || limit < 1 || limit > 500 {
		return result, errors.New("plugin secret rewrap batch arguments are invalid")
	}
	activeKeyID := service.ActiveKeyID()
	if activeKeyID == "" {
		return result, errors.New("plugin secret active key is unavailable")
	}
	candidates, err := inventory.ListActiveSecretRewrapCandidates(ctx, pluginID, activeKeyID, limit)
	if err != nil {
		return result, secretRewrapInventoryError(err)
	}
	if len(candidates) > limit {
		return result, ErrSecretRewrapInventory
	}
	result.Selected = len(candidates)
	for _, candidate := range candidates {
		if candidate.ID <= 0 || candidate.PluginID != pluginID || candidate.SecretName == "" {
			return result, ErrSecretRewrapInventory
		}
		_, changed, err := service.RewrapActiveIfCurrent(ctx, candidate.PluginID, candidate.OwnerUserID, candidate.SecretName, candidate.ID)
		if err != nil {
			if errors.Is(err, ErrSecretChanged) || errors.Is(err, ErrMarketNotFound) {
				return result, ErrSecretChanged
			}
			return result, secretRewrapError(err)
		}
		if changed {
			result.Rewrapped++
		}
	}
	result.Remaining, err = inventory.CountActiveSecretRewrapCandidates(ctx, pluginID, activeKeyID)
	if err != nil {
		return result, secretRewrapInventoryError(err)
	}
	return result, nil
}

func secretRewrapInventoryError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrSecretRewrapInventory
}

func secretRewrapError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrSecretRewrapFailed
}
