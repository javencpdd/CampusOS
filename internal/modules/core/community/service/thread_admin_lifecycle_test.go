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

func adminLifecycleFixture(t *testing.T, op string) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore, *identityrepo.MemoryAdminDomainRepository, *identityrepo.MemoryAdminSessionRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, gov, store, admins, sessions, proof := adminFlagFixture(t)
	ctx := context.Background()
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.trash", "community.thread.restore", "community.thread.direct_restore"}, "lifecycle-test"); err != nil {
		t.Fatal(err)
	}
	row, _ := threads.GetByID(ctx, "190101")
	row.ModerationStatus = "taken_down"
	if op == "restore" {
		row.DeletionStatus = "trashed"
	}
	row.SetModerationActor("user", row.AuthorID)
	if err := threads.Update(ctx, row); err != nil {
		t.Fatal(err)
	}
	return s, threads, gov, store, admins, sessions, proof
}

type lifecycleNotification struct {
	ThreadNotificationWriter
	after func(context.Context, string) error
}

func (w lifecycleNotification) NotifyThreadTrashed(ctx context.Context, owner, id, title, reason string) error {
	return w.after(ctx, owner)
}

func TestThreadAdminLifecyclePreservedDimensionsTypedHistoryAndSafeResult(t *testing.T) {
	for _, op := range []string{"trash", "restore", "direct_restore"} {
		for _, withCase := range []bool{false, true} {
			t.Run(op+"/"+map[bool]string{false: "empty", true: "case"}[withCase], func(t *testing.T) {
				s, threads, gov, store, _, _, proof := adminLifecycleFixture(t, op)
				ctx := context.Background()
				if withCase {
					if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "taken_down", OpenedBy: "190102", OpenedAt: time.Now().UTC()}); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := threads.GetByID(ctx, "190101")
				notified := 0
				s.SetNotificationWriter(lifecycleNotification{after: func(_ context.Context, owner string) error {
					notified++
					if owner != before.AuthorID {
						t.Fatal(owner)
					}
					return nil
				}})
				result, err := s.AdminLifecycleThread(ctx, before.ID, op, "actual lifecycle", proof)
				if err != nil || result == nil {
					t.Fatal(op, result, err)
				}
				after, _ := threads.GetByID(ctx, before.ID)
				deletion, moderation := before.DeletionStatus, before.ModerationStatus
				switch op {
				case "trash":
					deletion = "trashed"
				case "restore":
					deletion = "active"
				case "direct_restore":
					moderation = "clear"
				}
				if after.DeletionStatus != deletion || after.ModerationStatus != moderation || after.PublicationStatus != before.PublicationStatus || after.Content != before.Content || after.CurrentRevision != before.CurrentRevision+1 || after.FactsVersion <= before.FactsVersion {
					t.Fatal(after)
				}
				if op == "direct_restore" {
					if after.ModerationActorKind != "admin" || after.ModerationActorID != "190102" || after.ModerationBy != "" {
						t.Fatal(after)
					}
				} else if after.ModerationActorKind != before.ModerationActorKind || after.ModerationActorID != before.ModerationActorID || after.ModerationBy != before.ModerationBy {
					t.Fatal("unrelated actor changed", after)
				}
				if (notified == 1) != (op == "trash") {
					t.Fatal("same numeric Admin/User suppressed owner notification", op, notified)
				}
				raw, _ := json.Marshal(result)
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				if len(fields) != 3 || strings.Contains(string(raw), before.Content) || strings.Contains(string(raw), before.Title) {
					t.Fatal("private acknowledgement", string(raw))
				}
				if withCase {
					if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
						c, err := gov.LockModerationCase(tx, "190112")
						if err != nil {
							return err
						}
						if op == "direct_restore" {
							if c.ResolvedAt == nil || c.ResolvedActorKind != "admin" || c.ResolvedActorID != "190102" || c.ResolvedBy != "" {
								t.Fatal(c)
							}
						} else if c.ResolvedAt != nil {
							t.Fatal("unrelated case closed", c)
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				actions, err := gov.ListModerationActions(ctx, before.ID)
				if err != nil || len(actions) != 1 || actions[0].ActorKind != "admin" || actions[0].ActorRefID != "190102" || actions[0].ActorID != "" {
					t.Fatal(actions, err)
				}
				audits, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
				if err != nil || len(audits) != 1 || audits[0].ActorType != "admin" {
					t.Fatal(audits, err)
				}
			})
		}
	}
}

func TestThreadAdminLifecycleRevocationAndRequiredFailureRollback(t *testing.T) {
	for _, mode := range []string{"permission_aba", "session_revoke", "credential", "body_aba", "board_change", "state_change", "final_permission", "final_body", "new_case", "audit", "history", "case_resolution", "notification", "missing", "no_code", "user", "invalid_state", "reason", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			op := "direct_restore"
			if mode == "notification" {
				op = "trash"
			}
			s, threads, gov, store, admins, sessions, proof := adminLifecycleFixture(t, op)
			ctx := context.Background()
			reason := "actual lifecycle"
			if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "taken_down", OpenedBy: "190102", OpenedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			codes := []string{"community.thread.trash", "community.thread.restore", "community.thread.direct_restore"}
			switch mode {
			case "audit":
				s.SetReliability(reliability.NewService(transaction.NewMemory(threads, gov, store), authorFailAudit{Store: store}))
			case "history":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(context.Context) error { return errors.New("required lifecycle history failed") }})
			case "new_case":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(ctx context.Context) error {
					return gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190113", ThreadID: "190101", Status: "pending", OpenedBy: "190102", OpenedAt: time.Now().Add(time.Hour)})
				}})
			case "case_resolution":
				s.SetGovernanceRepository(reviewFailCase{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov})
			case "notification":
				s.SetNotificationWriter(lifecycleNotification{after: func(context.Context, string) error { return errors.New("required notification failed") }})
			case "missing":
				s.SetAdminExecutionAuthority(nil)
			case "no_code":
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.pin"}, "test"); err != nil {
					t.Fatal(err)
				}
			case "user":
				proof = authorUpdateProof("190102")
			case "invalid_state":
				row, _ := threads.GetByID(ctx, "190101")
				row.DeletionStatus = "trashed"
				if err := threads.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			case "reason":
				reason = " "
			case "unknown":
				op = "purge"
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
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "test"); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && mode == "session_revoke":
						if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && (mode == "body_aba" || mode == "board_change" || mode == "state_change") || calls == 5 && mode == "final_body":
						row, _ := threads.GetByID(ctx, before.ID)
						switch mode {
						case "board_change":
							row.CategoryID = "other"
						case "state_change":
							row.DeletionStatus = "trashed"
						default:
							row.Content = "changed"
						}
						if err := threads.Update(ctx, row); err != nil {
							t.Fatal(err)
						}
						if mode == "body_aba" {
							row.Content = before.Content
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
			result, err := s.AdminLifecycleThread(ctx, before.ID, op, reason, changing)
			if err == nil || result != nil {
				t.Fatal("failed lifecycle accepted", mode, result, err)
			}
			after, _ := threads.GetByID(ctx, before.ID)
			if after.DeletionStatus != before.DeletionStatus || after.ModerationStatus != before.ModerationStatus || after.ModerationActorID != before.ModerationActorID || !reflect.DeepEqual(history, gov.Snapshot()) {
				t.Fatal("partial lifecycle persisted", after)
			}
		})
	}
}
