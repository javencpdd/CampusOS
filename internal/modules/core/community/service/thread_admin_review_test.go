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

func adminReviewFixture(t *testing.T) (*ThreadService, *repository.MemoryThreadRepository, *repository.MemoryContentGovernanceRepository, *reliability.MemoryStore, *identityrepo.MemoryAdminDomainRepository, *identityrepo.MemoryAdminSessionRepository, identityport.SessionPrincipalResolver) {
	t.Helper()
	s, threads, gov, store, admins, sessions, proof := adminFlagFixture(t)
	ctx := context.Background()
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.review"}, "review-test"); err != nil {
		t.Fatal(err)
	}
	row, _ := threads.GetByID(ctx, "190101")
	row.ModerationStatus = "pending"
	if err := threads.Update(ctx, row); err != nil {
		t.Fatal(err)
	}
	return s, threads, gov, store, admins, sessions, proof
}

type reviewHistoryHook struct {
	repository.ContentGovernanceRepository
	repository.ModerationCaseCommandRepository
	afterAction func(context.Context) error
}

func (r reviewHistoryHook) CreateModerationAction(ctx context.Context, value *domain.ModerationAction) error {
	if err := r.ContentGovernanceRepository.CreateModerationAction(ctx, value); err != nil {
		return err
	}
	return r.afterAction(ctx)
}

type reviewFailCase struct {
	repository.ContentGovernanceRepository
	repository.ModerationCaseCommandRepository
}

func (r reviewFailCase) ResolveCaseForActor(ctx context.Context, expected *domain.ModerationCase, status, kind, id string, at time.Time) (*domain.ModerationCase, error) {
	value, err := r.ModerationCaseCommandRepository.ResolveCaseForActor(ctx, expected, status, kind, id, at)
	if err != nil {
		return nil, err
	}
	return value, errors.New("required case resolution failed")
}
func (r reviewFailCase) CreateModerationCase(ctx context.Context, value *domain.ModerationCase) error {
	if err := r.ContentGovernanceRepository.CreateModerationCase(ctx, value); err != nil {
		return err
	}
	return errors.New("required case creation failed")
}

func TestThreadAdminReviewCurrentStateTypedHistoryAndSafeResult(t *testing.T) {
	for _, op := range []string{"approve", "reject"} {
		for _, mode := range []string{"none", "single", "multiple"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, threads, gov, store, _, _, proof := adminReviewFixture(t)
				ctx := context.Background()
				now := time.Now().UTC()
				if mode != "none" {
					if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "pending", Reason: "current case", OpenedBy: "190102", OpenedAt: now}); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "multiple" {
					if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190111", ThreadID: "190101", Status: "taken_down", Reason: "older case", OpenedBy: "190102", OpenedAt: now.Add(-time.Hour)}); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := threads.GetByID(ctx, "190101")
				result, err := s.AdminReviewThread(ctx, before.ID, op, "actual review reason", proof)
				if err != nil || result == nil {
					t.Fatal(op, mode, result, err)
				}
				after, _ := threads.GetByID(ctx, before.ID)
				want := "clear"
				if op == "reject" {
					want = "rejected"
				}
				if string(after.ModerationStatus) != want || after.PublicationStatus != before.PublicationStatus || after.Content != before.Content || after.CurrentRevision != before.CurrentRevision+1 || after.FactsVersion <= before.FactsVersion || after.ModerationActorKind != "admin" || after.ModerationActorID != "190102" || after.ModerationBy != "" {
					t.Fatal(after)
				}
				raw, _ := json.Marshal(result)
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				if len(fields) != 2 || strings.Contains(string(raw), before.Content) || strings.Contains(string(raw), before.Title) {
					t.Fatal("unsafe result", string(raw))
				}
				if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
					open, err := gov.LockLatestOpenCase(tx, before.ID)
					if err != nil {
						return err
					}
					if op == "approve" && mode != "multiple" && open != nil {
						t.Fatal(open)
					}
					if op == "approve" && mode == "multiple" && (open == nil || open.ID != "190111") {
						t.Fatal(open)
					}
					if op == "reject" && (open == nil || mode == "none" && (open.OpenedActorKind != "admin" || open.OpenedActorID != "190102" || open.OpenedBy != "")) {
						t.Fatal(open)
					}
					if op == "approve" && mode != "none" {
						c, err := gov.LockModerationCase(tx, "190112")
						if err != nil {
							return err
						}
						if c.ResolvedAt == nil || c.ResolvedBy != "" || c.ResolvedActorKind != "admin" || c.ResolvedActorID != "190102" || c.OpenedBy != "190102" {
							t.Fatal(c)
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				actions, err := gov.ListModerationActions(ctx, before.ID)
				if err != nil || len(actions) != 1 || actions[0].ActorKind != "admin" || actions[0].ActorID != "" || actions[0].ActorRefID != "190102" {
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

func TestThreadAdminReviewRevocationCaseAndRequiredFailureRollback(t *testing.T) {
	for _, mode := range []string{"permission_aba", "session_revoke", "credential", "body_aba", "board_change", "state_change", "final_permission", "final_body", "new_case", "audit", "history", "case_resolution", "case_creation", "missing", "no_code", "user", "trash", "not_pending", "reason", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, gov, store, admins, sessions, proof := adminReviewFixture(t)
			ctx := context.Background()
			op := "approve"
			reason := "actual reason"
			if mode == "case_creation" {
				op = "reject"
			} else {
				if err := gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190112", ThreadID: "190101", Status: "pending", OpenedBy: "190102", OpenedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "audit":
				s.SetReliability(reliability.NewService(transaction.NewMemory(threads, gov, store), authorFailAudit{Store: store}))
			case "history":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(context.Context) error { return errors.New("required history error") }})
			case "new_case":
				s.SetGovernanceRepository(reviewHistoryHook{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov, afterAction: func(ctx context.Context) error {
					return gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: "190113", ThreadID: "190101", Status: "pending", OpenedBy: "190102", OpenedAt: time.Now().UTC().Add(time.Hour)})
				}})
			case "case_resolution", "case_creation":
				s.SetGovernanceRepository(reviewFailCase{ContentGovernanceRepository: gov, ModerationCaseCommandRepository: gov})
			case "missing":
				s.SetAdminExecutionAuthority(nil)
			case "no_code":
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.pin"}, "test"); err != nil {
					t.Fatal(err)
				}
			case "user":
				proof = authorUpdateProof("190102")
			case "trash", "not_pending":
				r, _ := threads.GetByID(ctx, "190101")
				if mode == "trash" {
					r.DeletionStatus = "trashed"
				} else {
					r.ModerationStatus = "clear"
				}
				if err := threads.Update(ctx, r); err != nil {
					t.Fatal(err)
				}
			case "reason":
				reason = " "
			case "unknown":
				op = "direct_restore"
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
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.review"}, "test"); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && mode == "session_revoke":
						if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					case calls == 3 && (mode == "body_aba" || mode == "board_change" || mode == "state_change") || calls == 5 && mode == "final_body":
						r, _ := threads.GetByID(ctx, before.ID)
						switch mode {
						case "board_change":
							r.CategoryID = "other"
						case "state_change":
							r.ModerationStatus = "clear"
						default:
							r.Content = "changed"
						}
						if err := threads.Update(ctx, r); err != nil {
							t.Fatal(err)
						}
						if mode == "body_aba" {
							r.Content = before.Content
							if err := threads.Update(ctx, r); err != nil {
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
			result, err := s.AdminReviewThread(ctx, before.ID, op, reason, changing)
			if err == nil || result != nil {
				t.Fatal("invalid review accepted", mode, result, err)
			}
			after, _ := threads.GetByID(ctx, before.ID)
			if after.ModerationStatus != before.ModerationStatus || after.ModerationActorID != before.ModerationActorID || !reflect.DeepEqual(history, gov.Snapshot()) {
				t.Fatal("partial review persisted", after)
			}
		})
	}
}
