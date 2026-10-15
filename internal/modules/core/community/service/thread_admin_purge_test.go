package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func adminPurgeFixture(t *testing.T) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore, *identityrepo.MemoryAdminDomainRepository, *identityrepo.MemoryAdminSessionRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, gov, store, admins, sessions, proof := adminLifecycleFixture(t, "restore")
	if err := admins.ReplaceAdminRolePermissionFacts(context.Background(), "security_admin", []string{"community.thread.purge"}, "purge-test"); err != nil {
		t.Fatal(err)
	}
	return s, threads, gov, store, admins, sessions, proof
}

func TestThreadAdminPurgeCurrentAuthorityTombstoneAndSafeResult(t *testing.T) {
	for _, withCase := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "case"}[withCase], func(t *testing.T) {
			s, threads, gov, store, _, _, proof := adminPurgeFixture(t)
			ctx := context.Background()
			if withCase {
				if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "taken_down", OpenedBy: "190102", OpenedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := threads.GetByID(ctx, "190101")
			result, err := s.AdminPurgeThread(ctx, before.ID, "purge", "actual purge", proof)
			if err != nil || result == nil || result.DeletionStatus != "purged" {
				t.Fatal(result, err)
			}
			if _, err := threads.GetByID(ctx, before.ID); !errors.Is(err, repository.ErrThreadNotFound) {
				t.Fatal("normal read remained open", err)
			}
			if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
				row, err := threads.LockThreadGovernanceSnapshot(tx, before.ID)
				if err != nil {
					return err
				}
				if row.DeletedAt == nil || row.FactsVersion <= before.FactsVersion || row.CurrentRevision != before.CurrentRevision+1 || row.Content != before.Content || row.PublicationStatus != before.PublicationStatus || row.ModerationStatus != before.ModerationStatus || row.ModerationActorID != before.ModerationActorID {
					t.Fatal(row)
				}
				if withCase {
					c, err := gov.LockModerationCase(tx, "190112")
					if err != nil {
						return err
					}
					if c.ResolvedAt != nil || c.OpenedBy != "190102" {
						t.Fatal("purge rewrote case", c)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(result)
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			if len(fields) != 2 || strings.Contains(string(raw), before.Content) || strings.Contains(string(raw), before.Title) {
				t.Fatal("unsafe purge result", string(raw))
			}
			actions, err := gov.ListModerationActions(ctx, before.ID)
			if err != nil || len(actions) != 1 || actions[0].ActorKind != "admin" || actions[0].ActorRefID != "190102" || actions[0].ActorID != "" {
				t.Fatal(actions, err)
			}
			audits, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
			if err != nil || len(audits) != 1 || audits[0].ActorType != "admin" {
				t.Fatal(audits, err)
			}
			if got, err := s.AdminPurgeThread(ctx, before.ID, "purge", "repeat", proof); err == nil || got != nil {
				t.Fatal("repeat purge accepted", got, err)
			}
		})
	}
}

func TestThreadAdminPurgeRevocationTombstoneAndRequiredFailureRollback(t *testing.T) {
	for _, mode := range []string{"permission_aba", "session_revoke", "credential", "body_aba", "board_change", "state_change", "date_aba", "final_permission", "final_body", "final_date", "new_case", "audit", "history", "missing", "no_code", "user", "invalid_state", "reason", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, gov, store, admins, sessions, proof := adminPurgeFixture(t)
			ctx := context.Background()
			op := "purge"
			reason := "actual purge"
			if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "taken_down", OpenedBy: "190102", OpenedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "audit":
				s.SetReliability(reliability.NewService(transaction.NewMemory(threads, gov, store), authorFailAudit{Store: store}))
			case "history":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(context.Context) error { return errors.New("required purge history failed") }})
			case "new_case":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(ctx context.Context) error {
					return gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190113", ThreadID: "190101", Status: "pending", OpenedBy: "190102", OpenedAt: time.Now().Add(time.Hour)})
				}})
			case "missing":
				s.SetAdminExecutionAuthority(nil)
			case "no_code":
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.restore"}, "test"); err != nil {
					t.Fatal(err)
				}
			case "user":
				proof = authorUpdateProof("190102")
			case "invalid_state":
				row, _ := threads.GetByID(ctx, "190101")
				row.DeletionStatus = "active"
				if err := threads.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			case "reason":
				reason = " "
			case "unknown":
				op = "restore"
			}
			before, _ := threads.GetByID(ctx, "190101")
			history := gov.Snapshot()
			calls := 0
			changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 || calls == 5 {
					switch {
					case calls == 3 && mode == "permission_aba" || calls == 5 && mode == "final_permission":
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
							t.Fatal(err)
						}
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.purge"}, "test"); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && mode == "session_revoke":
						if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && (mode == "body_aba" || mode == "board_change" || mode == "state_change" || mode == "date_aba") || calls == 5 && (mode == "final_body" || mode == "final_date"):
						var row *domain.Thread
						var err error
						if calls == 5 {
							row, err = threads.LockThreadGovernanceSnapshot(ctx, before.ID)
						} else {
							row, err = threads.GetByID(ctx, before.ID)
						}
						if err != nil {
							t.Fatal(err)
						}
						switch mode {
						case "board_change":
							row.CategoryID = "other"
						case "state_change":
							row.DeletionStatus = "active"
						case "date_aba", "final_date":
							at := time.Now().UTC().Add(time.Hour)
							row.DeletedAt = &at
						default:
							row.Content = "changed"
						}
						if err := threads.Update(ctx, row); err != nil {
							t.Fatal(err)
						}
						if mode == "body_aba" || mode == "date_aba" {
							row.Content = before.Content
							row.DeletedAt = nil
							if err := threads.Update(ctx, row); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if mode == "credential" && calls >= 3 {
					return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
				}
				return proof(ctx)
			}
			result, err := s.AdminPurgeThread(ctx, before.ID, op, reason, changing)
			if err == nil || result != nil {
				t.Fatal("invalid purge accepted", mode, result, err)
			}
			after, getErr := threads.GetByID(ctx, before.ID)
			if getErr != nil || after.DeletedAt != nil || after.DeletionStatus != before.DeletionStatus || after.ModerationStatus != before.ModerationStatus || !reflect.DeepEqual(history, gov.Snapshot()) {
				t.Fatal("partial purge persisted", after, getErr)
			}
		})
	}
}
