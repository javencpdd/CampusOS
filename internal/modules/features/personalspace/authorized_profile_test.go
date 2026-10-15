package space

import (
	"context"
	"encoding/json"
	"errors"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"strings"
	"testing"
	"time"
)

type spaceOwnerAuditFailure struct {
	reliability.Store
	mode string
}

func (s spaceOwnerAuditFailure) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.mode == "retention_pending" && strings.HasPrefix(a.CommandCode, avatarRetentionKind+".") {
		return errors.New("required retention audit failed")
	}
	if s.mode == "audit" {
		return errors.New("required space audit failed")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}

func (s spaceOwnerAuditFailure) ListSubjectOperations(ctx context.Context, kind, subject, owner string, limit int) ([]reliability.Operation, error) {
	reader, ok := s.Store.(reliability.ScopedOperationReader)
	if !ok {
		return nil, reliability.ErrOperationFactsUnavailable
	}
	return reader.ListSubjectOperations(ctx, kind, subject, owner, limit)
}
func (s spaceOwnerAuditFailure) GetSubjectOperation(ctx context.Context, id, kind, subject, owner string, lock bool) (*reliability.Operation, error) {
	reader, ok := s.Store.(reliability.ScopedOperationReader)
	if !ok {
		return nil, reliability.ErrOperationFactsUnavailable
	}
	return reader.GetSubjectOperation(ctx, id, kind, subject, owner, lock)
}
func (s spaceOwnerAuditFailure) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.mode == "outbox" {
		return nil, errors.New("required space event failed")
	}
	return s.Store.Enqueue(ctx, e)
}

type spaceOwnerRepoHook struct {
	*MemoryRepository
	after func(context.Context, *Space)
}

func (r *spaceOwnerRepoHook) Upsert(ctx context.Context, s *Space) error {
	if err := r.MemoryRepository.Upsert(ctx, s); err != nil {
		return err
	}
	if r.after != nil {
		r.after(ctx, s)
	}
	return nil
}
func spaceOwnerProof(_ context.Context) (identityport.PrincipalContext, error) {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "space-session"}, nil
}
func spaceOwnerFixture(t *testing.T, existing bool, mode string) (*Service, *MemoryRepository, *reliability.MemoryStore) {
	t.Helper()
	repo := NewMemoryRepository()
	users := newFakeUserLookup(&identitydomain.User{ID: "1001", Username: "owner", Nickname: "Owner", Status: "active"}, &identitydomain.User{ID: "1002", Username: "other", Status: "active"})
	s := NewService(repo, users)
	if existing {
		space := defaultSpace(users.byID["1001"])
		space.SyncCategories, space.SyncTags = []string{"kept"}, []string{"tag"}
		space.ID = "2001"
		space.IsDefault = false
		if err := repo.Upsert(context.Background(), space); err != nil {
			t.Fatal(err)
		}
	}
	events := reliability.NewMemoryStore()
	s.SetReliability(reliability.NewService(transaction.NewMemory(repo, events), spaceOwnerAuditFailure{Store: events, mode: mode}))
	return s, repo, events
}
func TestSpaceOwnerProfileCurrentDefaultExistingAndAuditRollback(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "noop", "clear_filters", "admin", "missing", "credential", "disabled", "feature_disabled", "audit", "outbox", "missing_audit", "invalid_visibility"} {
			t.Run(map[bool]string{false: "default", true: "existing"}[existing]+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				s, repo, events := spaceOwnerFixture(t, existing, mode)
				proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
				title := " New title "
				bio := " Own bio "
				visibility := "private"
				req := UpsertSpaceRequest{Title: &title, Bio: &bio, Visibility: &visibility}
				switch mode {
				case "noop":
					req = UpsertSpaceRequest{}
				case "clear_filters":
					req = UpsertSpaceRequest{SyncCategories: []string{}, SyncTags: []string{}}
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						p.Actor.Kind, p.Audience = "admin", "admin"
						return p, e
					}
				case "missing":
					proof = nil
				case "credential":
					calls := 0
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						calls++
						if calls > 1 {
							p.CredentialID = "changed-session"
						}
						return p, e
					}
				case "disabled":
					if !existing {
						return
					}
					at := time.Now()
					if err := repo.SetDisabled(ctx, "1001", &at, "", "disabled"); err != nil {
						t.Fatal(err)
					}
				case "feature_disabled":
					s.SetPluginEnabledChecker(func() bool { return false })
				case "missing_audit":
					s.reliable = nil
				case "invalid_visibility":
					visibility = "invalid"
				}
				before, beforeEvents := repo.Snapshot(), events.Snapshot()
				ack, err := s.UpsertOwnSpaceForPrincipal(ctx, req, proof)
				allowed := mode == "current" || mode == "noop" || mode == "clear_filters"
				if (err == nil) != allowed {
					t.Fatal(mode, ack, err)
				}
				if !allowed {
					if ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
						t.Fatal("failed profile retained mutation", mode, ack, err)
					}
					return
				}
				if ack.OwnerID != "1001" || ack.SpaceID == "" {
					t.Fatal(ack)
				}
				space, err := s.GetOwnSpaceForPrincipal(ctx, spaceOwnerProof)
				if err != nil || space.Space.ID != ack.SpaceID {
					t.Fatal(space, err)
				}
				if mode == "current" && (space.Space.Title != "New title" || space.Space.Bio != "Own bio" || space.Space.Visibility != VisibilityPrivate) {
					t.Fatal(space)
				}
				if mode == "clear_filters" && (len(space.Space.SyncCategories) != 0 || len(space.Space.SyncTags) != 0) {
					t.Fatal("explicit empty filters not cleared", space)
				}
				raw, _ := json.Marshal(ack)
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
					t.Fatal("unsafe ack", string(raw))
				}
			})
		}
	}
}
func TestSpaceOwnerProfileActualExtraWritesCurrentProofAndFinalRollback(t *testing.T) {
	for _, mode := range []string{"title", "actor", "session", "strength", "feature", "sync_only"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, events := spaceOwnerFixture(t, true, "")
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ran, changed := false, false
			s.repo = &spaceOwnerRepoHook{MemoryRepository: repo, after: func(ctx context.Context, space *Space) {
				ran, changed = true, true
				switch mode {
				case "title":
					space.Title = "extra title"
					if err := repo.Upsert(ctx, space); err != nil {
						t.Fatal(err)
					}
				case "actor":
					space.DisabledActorKind, space.DisabledActorID = "admin", "admin:extra"
					if err := repo.Upsert(ctx, space); err != nil {
						t.Fatal(err)
					}
				case "feature":
					s.SetPluginEnabledChecker(func() bool { return false })
				case "sync_only":
					at := time.Now()
					if err := repo.UpdateSyncStatus(ctx, "1001", &at, "operational"); err != nil {
						t.Fatal(err)
					}
				}
			}}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if changed {
					switch mode {
					case "session":
						return p, errors.New("session revoked")
					case "strength":
						p.AuthenticationStrength = "mfa"
					}
				}
				return p, e
			}
			bio := "new bio"
			ack, err := s.UpsertOwnSpaceForPrincipal(ctx, UpsertSpaceRequest{Bio: &bio}, proof)
			if !ran {
				t.Fatal("actual extra write did not execute", mode)
			}
			// Whole command output includes current operational fields, so an extra
			// sync write is conservatively rejected even though it keeps the nonce.
			if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("extra mutation not rolled back", mode, ack, err)
			}
		})
	}
}
func TestSpaceOwnerReadWholeFactsDefaultEmptyAndCurrentCredential(t *testing.T) {
	for _, existing := range []bool{false, true} {
		s, repo, _ := spaceOwnerFixture(t, existing, "")
		ctx := context.Background()
		calls := 0
		proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
			p, e := spaceOwnerProof(ctx)
			calls++
			if calls > 1 {
				p.CredentialID = "changed-current-session"
			}
			return p, e
		}
		result, err := s.GetOwnSpaceForPrincipal(ctx, proof)
		if err == nil || result != nil {
			t.Fatal("empty/default/current proof skipped", existing, result, err)
		}
		before := repo.Snapshot()
		result, err = s.GetOwnSpaceForPrincipal(ctx, spaceOwnerProof)
		if err != nil || result == nil || result.Owner.ID != "1001" || !reflect.DeepEqual(before, repo.Snapshot()) {
			t.Fatal(result, err)
		}
	}
}
