package space

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const avatarRetentionKind = "feature.personal_space.avatar_retention"
const avatarRetentionLimit = 100

var ErrAvatarRetentionPending = errors.New("avatar retention pending")

type AvatarCleanupAck struct {
	OwnerID        string `json:"owner_id"`
	CleanupPending bool   `json:"cleanup_pending"`
}
type avatarRetentionDetails struct {
	Receipt     avatarRetentionReceipt `json:"receipt"`
	Disposition string                 `json:"disposition"`
}
type avatarRetentionScope struct {
	Principal identityport.PrincipalContext
	Facts     *spaceOwnerFacts
	Files     []avatarFile
}
type avatarRetentionRun struct {
	s           *Service
	store       *LocalFileStore
	cfg         FileStorageConfig
	reader      reliability.ScopedOperationReader
	account     corestorage.AvatarRetentionAccounting
	storage     corestorage.Port
	accountPort corestorage.InlineImageAccounting
	p           identityport.PrincipalContext
	resolve     identityport.SessionPrincipalResolver
}

type avatarRetentionCancelledRetryKey struct{}

func (s *Service) CleanupAvatarHistoryForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*AvatarCleanupAck, error) {
	if s == nil || s.fileStore == nil || s.reliable == nil || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	p, err := spaceOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	store := s.fileStore
	store.mu.Lock()
	defer store.mu.Unlock()
	pending, err := s.cleanupAvatarHistoryLocked(ctx, store, p, resolve)
	if err != nil && !errors.Is(err, ErrAvatarRetentionPending) {
		return nil, err
	}
	return &AvatarCleanupAck{OwnerID: p.Actor.ID, CleanupPending: pending || err != nil}, nil
}

// The caller already owns the process file mutex, before any SDK transaction.
func (s *Service) cleanupAvatarHistoryLocked(ctx context.Context, store *LocalFileStore, p identityport.PrincipalContext, resolve identityport.SessionPrincipalResolver) (bool, error) {
	reader, ok := s.reliable.Store().(reliability.ScopedOperationReader)
	account, accountOK := s.avatarAccount.(corestorage.AvatarRetentionAccounting)
	if !ok || !accountOK || corestorage.NilImageAdapter(reader) || corestorage.NilImageAdapter(account) || corestorage.NilImageAdapter(s.avatarStorage) {
		return true, ErrSpaceFactsUnavailable
	}
	r := &avatarRetentionRun{s: s, store: store, cfg: store.cfg, reader: reader, account: account, storage: s.avatarStorage, accountPort: s.avatarAccount, p: p, resolve: resolve}
	initial, err := r.scope(ctx, false)
	if err != nil {
		return true, err
	}
	var jobs []reliability.Operation
	// Admission covers empty inventories as well as existing pending jobs.
	err = r.command(ctx, "inventory", "", func(tx context.Context) error {
		if err := r.checkScope(tx, initial); err != nil {
			return err
		}
		var err error
		jobs, err = reader.ListSubjectOperations(tx, avatarRetentionKind, "personal_space", p.Actor.ID, avatarRetentionLimit+1)
		if err != nil {
			return err
		}
		if err := r.checkScope(tx, initial); err != nil {
			return err
		}
		final, err := reader.ListSubjectOperations(tx, avatarRetentionKind, "personal_space", p.Actor.ID, avatarRetentionLimit+1)
		if err != nil || !reflect.DeepEqual(jobs, final) {
			return errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	pending := len(jobs) > avatarRetentionLimit
	if len(jobs) > avatarRetentionLimit {
		jobs = jobs[:avatarRetentionLimit]
	}
	processed := map[string]bool{}
	work := 0
	for _, job := range jobs {
		work++
		details, err := decodeAvatarRetentionDetails(job)
		if err != nil {
			pending = true
			continue
		}
		processed[details.Receipt.FileName] = true
		if err := r.finish(ctx, job); err != nil {
			pending = true
		}
	}
	for work < avatarRetentionLimit {
		current, err := r.scope(ctx, false)
		if err != nil {
			return true, errors.Join(ErrAvatarRetentionPending, err)
		}
		candidates := r.candidates(current)
		name := ""
		for _, file := range candidates {
			if !processed[file.name] {
				name = file.name
				break
			}
		}
		if name == "" {
			break
		}
		processed[name] = true
		work++
		receipt, err := readAvatarRetentionReceipt(ctx, store, p.Actor.ID, name)
		if err != nil {
			pending = true
			continue
		}
		job, err := r.prepare(ctx, current, *receipt)
		if err != nil {
			pending = true
			continue
		}
		if err := r.finish(ctx, *job); err != nil {
			pending = true
		}
	}
	final, err := r.scope(ctx, false)
	if err != nil {
		return true, errors.Join(ErrAvatarRetentionPending, err)
	}
	if len(r.candidates(final)) > 0 {
		pending = true
	}
	if err := r.checkScope(ctx, final); err != nil {
		return true, errors.Join(ErrAvatarRetentionPending, err)
	}
	return pending, nil
}

func (r *avatarRetentionRun) scope(ctx context.Context, lock bool) (*avatarRetentionScope, error) {
	if r.s.fileStore != r.store || !reflect.DeepEqual(r.cfg, r.store.cfg) || !sameAvatarAdapter(r.storage, r.s.avatarStorage) || !sameAvatarAdapter(r.accountPort, r.s.avatarAccount) || !sameStoragePath(r.cfg.RootDir, r.storage.Root()) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	p, err := spaceOwnerPrincipal(ctx, r.resolve)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(p, r.p) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	facts, err := r.s.loadSpaceOwnerFacts(ctx, p.Actor.ID, lock)
	if err != nil {
		return nil, err
	}
	request := spaceOwnerRequest(facts, p, "avatar_retention", "space-avatar-retention")
	d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(request)
	if err != nil || d.Effect != "allow" {
		return nil, errors.Join(ErrSpaceOwnerDenied, err)
	}
	files, err := r.store.avatarFiles(p.Actor.ID)
	if err != nil {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	return &avatarRetentionScope{Principal: p, Facts: facts, Files: files}, nil
}
func (r *avatarRetentionRun) checkScope(ctx context.Context, expected *avatarRetentionScope) error {
	current, err := r.scope(ctx, transaction.Active(ctx))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, current) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (r *avatarRetentionRun) protected(scope *avatarRetentionScope, name string) bool {
	url := r.store.AvatarURL(r.p.Actor.ID, name)
	return scope.Facts.Space.Avatar == url || scope.Facts.Owner.Avatar == url
}
func (r *avatarRetentionRun) candidates(scope *avatarRetentionScope) []avatarFile {
	files := []avatarFile{}
	for index, file := range scope.Files {
		if index >= r.cfg.AvatarKeepLimit && !r.protected(scope, file.name) {
			files = append(files, file)
		}
	}
	return files
}
func (r *avatarRetentionRun) command(ctx context.Context, phase, id string, action func(context.Context) error) error {
	code := avatarRetentionKind + "." + phase
	return r.s.reliable.Execute(ctx, reliability.Command{Code: code, ActorType: "user", ActorID: r.p.Actor.ID, ResourceType: "personal_space", ResourceID: r.p.Actor.ID, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("personal_space.avatar_retention."+phase, "personal_space", r.p.Actor.ID, map[string]string{"owner_id": r.p.Actor.ID, "operation_id": id})
	}}, action)
}
func decodeAvatarRetentionDetails(op reliability.Operation) (*avatarRetentionDetails, error) {
	var d avatarRetentionDetails
	decoder := json.NewDecoder(bytes.NewReader(op.Details))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(&struct{}{}) != io.EOF || d.Receipt.Version != 1 || d.Receipt.Size < 1 || validateStorageSegment(d.Receipt.FileName) != nil || len(d.Receipt.Digest) != 64 || d.Receipt.Identity == "" || d.Disposition != "pending" && d.Disposition != "deleted" && d.Disposition != "retained" {
		return nil, ErrSpaceFactsUnavailable
	}
	if _, err := hex.DecodeString(d.Receipt.Digest); err != nil || d.Receipt.ModifiedAt.IsZero() || len(d.Receipt.Identity) > 160 || !os.FileMode(d.Receipt.Mode).IsRegular() {
		return nil, ErrSpaceFactsUnavailable
	}
	return &d, nil
}
func (r *avatarRetentionRun) prepare(ctx context.Context, scope *avatarRetentionScope, receipt avatarRetentionReceipt) (*reliability.Operation, error) {
	details, _ := json.Marshal(avatarRetentionDetails{Receipt: receipt, Disposition: "pending"})
	type inventoryEntry struct {
		Name       string
		Size       int64
		ModifiedAt time.Time
	}
	inventory := make([]inventoryEntry, 0, len(scope.Files))
	for _, file := range scope.Files {
		inventory = append(inventory, inventoryEntry{file.name, file.size, file.modTime.UTC()})
	}
	keyFacts, _ := json.Marshal([]any{r.p.Actor.ID, scope.Facts, receipt, r.cfg.AvatarKeepLimit, inventory})
	digest := sha256.Sum256(keyFacts)
	var result *reliability.Operation
	err := r.command(ctx, "prepare", "", func(tx context.Context) error {
		if err := r.checkScope(tx, scope); err != nil {
			return err
		}
		if err := checkAvatarRetentionReceipt(tx, r.store, r.p.Actor.ID, &receipt); err != nil {
			return err
		}
		if err := r.account.PrepareAvatarRetentionAccounting(tx, r.p.Actor.ID); err != nil {
			return err
		}
		op, err := r.s.reliable.Store().StartOperation(tx, reliability.Operation{ID: fmt.Sprint(idgen.New()), Kind: avatarRetentionKind, SubjectType: "personal_space", SubjectID: r.p.Actor.ID, ActorID: r.p.Actor.ID, Status: reliability.OperationPending, IdempotencyKey: fmt.Sprintf("avatar-retention:%x", digest), Details: details})
		if err != nil {
			return err
		}
		if op == nil {
			return ErrSpaceFactsUnavailable
		}
		result, err = r.operation(tx, op.ID, true)
		if err != nil {
			return err
		}
		d, err := decodeAvatarRetentionDetails(*result)
		if err != nil || !reflect.DeepEqual(receipt, d.Receipt) || result.ActorID != r.p.Actor.ID || result.IdempotencyKey != fmt.Sprintf("avatar-retention:%x", digest) {
			return errors.Join(ErrSpaceFactsUnavailable, err)
		}
		if err := r.checkScope(tx, scope); err != nil {
			return err
		}
		return nil
	})
	return result, err
}
func (r *avatarRetentionRun) finish(ctx context.Context, expected reliability.Operation) (returnErr error) {
	details, err := decodeAvatarRetentionDetails(expected)
	if err != nil {
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	if expected.Status == reliability.OperationSucceeded {
		return nil
	}
	scope, err := r.scope(ctx, false)
	if err != nil {
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	absent, err := avatarRetentionFileAbsent(r.store, r.p.Actor.ID, details.Receipt.FileName)
	if err != nil {
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	retained := r.protected(scope, details.Receipt.FileName)
	if !absent && !retained {
		retained = true
		for _, file := range r.candidates(scope) {
			if file.name == details.Receipt.FileName {
				retained = false
				break
			}
		}
	}
	err = r.command(ctx, "bytes", expected.ID, func(tx context.Context) error {
		if err := r.checkScope(tx, scope); err != nil {
			return err
		}
		current, err := r.operation(tx, expected.ID, true)
		if err != nil || !reflect.DeepEqual(expected, *current) {
			return errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		if retained {
			if absent {
				return ErrAvatarRetentionPending
			}
			details.Disposition = "retained"
			result, err := r.updateJob(tx, *current, *details)
			if err != nil {
				return err
			}
			if err := r.checkScope(tx, scope); err != nil {
				return err
			}
			return r.checkJob(tx, result)
		}
		if !absent {
			if err := checkAvatarRetentionReceipt(tx, r.store, r.p.Actor.ID, &details.Receipt); err != nil {
				return err
			}
			if err := r.checkScope(tx, scope); err != nil {
				return err
			}
			path, err := avatarRetentionPath(r.store, r.p.Actor.ID, details.Receipt.FileName)
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			expectedFiles := make([]avatarFile, 0, len(scope.Files)-1)
			for _, file := range scope.Files {
				if file.name != details.Receipt.FileName {
					expectedFiles = append(expectedFiles, file)
				}
			}
			after := *scope
			after.Files = expectedFiles
			if err := r.checkScope(tx, &after); err != nil {
				return err
			}
		} else if err := r.checkScope(tx, scope); err != nil {
			return err
		}
		gone, err := avatarRetentionFileAbsent(r.store, r.p.Actor.ID, details.Receipt.FileName)
		if err != nil || !gone {
			return errors.Join(ErrAvatarRetentionPending, err)
		}
		return nil
	})
	if err != nil {
		// A cancelled SQL commit can roll back the byte-phase audit after unlink.
		// Re-authorize the now-missing file once with a bounded fresh context.
		if ctx.Err() != nil && !retained && ctx.Value(avatarRetentionCancelledRetryKey{}) != true {
			gone, statErr := avatarRetentionFileAbsent(r.store, r.p.Actor.ID, details.Receipt.FileName)
			if statErr == nil && gone {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer stop()
				return r.finish(context.WithValue(cleanup, avatarRetentionCancelledRetryKey{}, true), expected)
			}
		}
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	if retained {
		return nil
	}
	finalCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	finalScope, err := r.scope(finalCtx, false)
	if err != nil {
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	undo := func() error { return nil }
	committed := false
	defer func() {
		if !committed {
			returnErr = errors.Join(returnErr, undo())
		}
	}()
	err = r.command(finalCtx, "finalize", expected.ID, func(tx context.Context) error {
		if err := r.checkScope(tx, finalScope); err != nil {
			return err
		}
		current, err := r.operation(tx, expected.ID, true)
		if err != nil {
			return err
		}
		if current.Status == reliability.OperationSucceeded {
			return nil
		}
		if !reflect.DeepEqual(expected, *current) {
			return identityport.ErrPolicyFactsChanged
		}
		gone, err := avatarRetentionFileAbsent(r.store, r.p.Actor.ID, details.Receipt.FileName)
		if err != nil || !gone || r.protected(finalScope, details.Receipt.FileName) {
			return errors.Join(ErrAvatarRetentionPending, err)
		}
		undo, err = r.account.ReleaseAvatarRetentionBytes(tx, r.p.Actor.ID, details.Receipt.Size)
		if err != nil {
			return err
		}
		details.Disposition = "deleted"
		result, err := r.updateJob(tx, *current, *details)
		if err != nil {
			return err
		}
		if err := r.checkScope(tx, finalScope); err != nil {
			return err
		}
		return r.checkJob(tx, result)
	})
	if err != nil {
		return errors.Join(ErrAvatarRetentionPending, err)
	}
	committed = true
	return nil
}

func (r *avatarRetentionRun) operation(ctx context.Context, id string, lock bool) (*reliability.Operation, error) {
	op, err := r.reader.GetSubjectOperation(ctx, id, avatarRetentionKind, "personal_space", r.p.Actor.ID, lock)
	if err != nil {
		return nil, err
	}
	if op == nil || op.ID != id || op.Kind != avatarRetentionKind || op.SubjectType != "personal_space" || op.SubjectID != r.p.Actor.ID || op.ActorID != r.p.Actor.ID || op.IdempotencyKey == "" || op.CreatedAt.IsZero() || op.UpdatedAt.IsZero() {
		return nil, ErrSpaceFactsUnavailable
	}
	switch op.Status {
	case reliability.OperationPending, reliability.OperationRunning, reliability.OperationCompensate, reliability.OperationFailed, reliability.OperationSucceeded:
	default:
		return nil, ErrSpaceFactsUnavailable
	}
	if _, err := decodeAvatarRetentionDetails(*op); err != nil {
		return nil, err
	}
	return op, nil
}
func (r *avatarRetentionRun) checkJob(ctx context.Context, expected *reliability.Operation) error {
	current, err := r.operation(ctx, expected.ID, true)
	if err != nil || !reflect.DeepEqual(expected, current) {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	return nil
}
func (r *avatarRetentionRun) updateJob(ctx context.Context, before reliability.Operation, details avatarRetentionDetails) (*reliability.Operation, error) {
	write := before
	write.Details, _ = json.Marshal(details)
	write.Status = reliability.OperationSucceeded
	write.Error = ""
	if err := r.s.reliable.Store().UpdateOperation(ctx, write); err != nil {
		return nil, err
	}
	current, err := r.operation(ctx, before.ID, true)
	if err != nil {
		return nil, err
	}
	actualDetails, err := decodeAvatarRetentionDetails(*current)
	if err != nil || !reflect.DeepEqual(details, *actualDetails) || current.UpdatedAt.Before(before.UpdatedAt) {
		return nil, errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	write.Details = append([]byte(nil), current.Details...)
	write.UpdatedAt = current.UpdatedAt
	if !reflect.DeepEqual(write, *current) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	return current, nil
}
