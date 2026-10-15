package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type featureGovernanceTestPart struct {
	kind  domain.ThreadType
	load  func(context.Context, *domain.Thread, bool) (any, error)
	apply func(context.Context, *domain.Thread, identityport.PrincipalRef) error
	check func(context.Context, *domain.Thread, identityport.PrincipalRef) error
}

func (p *featureGovernanceTestPart) ThreadType() domain.ThreadType { return p.kind }
func (p *featureGovernanceTestPart) LoadGovernanceFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	return p.load(ctx, t, lock)
}
func (p *featureGovernanceTestPart) ApplyGovernanceCommand(ctx context.Context, t *domain.Thread, a identityport.PrincipalRef) error {
	if p.apply != nil {
		return p.apply(ctx, t, a)
	}
	return nil
}
func (p *featureGovernanceTestPart) CheckGovernanceResult(ctx context.Context, t *domain.Thread, a identityport.PrincipalRef) error {
	if p.check != nil {
		return p.check(ctx, t, a)
	}
	return nil
}

func TestFeatureGovernanceParticipantCurrentTransactionAndDetachedCore(t *testing.T) {
	for _, op := range []string{"take_down", "direct_restore", "trash"} {
		t.Run(op, func(t *testing.T) {
			s, threads, gov, _, admins, _, proof := adminFlagFixture(t)
			ctx := context.Background()
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down", "community.thread.direct_restore", "community.thread.trash"}, "feature-test"); err != nil {
				t.Fatal(err)
			}
			s.SetGovernanceAuthority(s.adminExecution.(identityport.BoardGovernanceAuthorityReader))
			before, _ := threads.GetByID(ctx, "190101")
			before.ThreadType = domain.ThreadTypeArticle
			before.Tags = []string{"original"}
			at := time.Now().UTC()
			before.ModerationAt = &at
			if op == "direct_restore" {
				before.ModerationStatus = "taken_down"
			}
			if err := threads.Update(ctx, before); err != nil {
				t.Fatal(err)
			}
			before, _ = threads.GetByID(ctx, before.ID)
			locked, applied, checked := false, false, false
			mutateDetached := func(t *domain.Thread) {
				t.Content = "injected"
				t.Tags[0] = "injected"
				*t.ModerationAt = time.Unix(0, 0)
			}
			p := &featureGovernanceTestPart{kind: domain.ThreadTypeArticle, load: func(tx context.Context, t *domain.Thread, lock bool) (any, error) {
				if lock {
					if !transaction.Active(tx) {
						return nil, errors.New("missing transaction")
					}
					locked = true
				}
				mutateDetached(t)
				return "actual detached Feature facts", nil
			}, apply: func(tx context.Context, t *domain.Thread, actor identityport.PrincipalRef) error {
				if !transaction.Active(tx) || !locked || actor.Kind != "admin" || actor.ID != "190102" {
					return errors.New("wrong command authority")
				}
				applied = true
				mutateDetached(t)
				return nil
			}, check: func(tx context.Context, t *domain.Thread, actor identityport.PrincipalRef) error {
				if !transaction.Active(tx) || !applied {
					return errors.New("missing atomic participant")
				}
				checked = true
				mutateDetached(t)
				return nil
			}}
			if err := s.ExecuteThreadGovernanceCommand(ctx, before.ID, op, "actual Feature command", proof, p); err != nil {
				t.Fatal(err)
			}
			after, _ := threads.GetByID(ctx, before.ID)
			if !checked || after.Content != before.Content || !reflect.DeepEqual(after.Tags, before.Tags) || after.CurrentRevision != before.CurrentRevision+1 || after.FactsVersion <= before.FactsVersion || after.ModerationAt == nil || after.ModerationAt.Equal(time.Unix(0, 0)) {
				t.Fatal("Feature mutated Core-owned facts", after, checked)
			}
			actions, err := gov.ListModerationActions(ctx, before.ID)
			if err != nil || len(actions) != 1 || actions[0].ActorKind != "admin" {
				t.Fatal(actions, err)
			}
		})
	}
}

func TestFeatureGovernanceParticipantMissingDriftAndFailureRollback(t *testing.T) {
	for _, mode := range []string{"nil", "discussion", "unknown_operation", "locked_facts", "apply", "final", "locked_core", "final_core"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, gov, events, admins, _, proof := adminFlagFixture(t)
			ctx := context.Background()
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.take_down"}, "feature-test"); err != nil {
				t.Fatal(err)
			}
			s.SetGovernanceAuthority(s.adminExecution.(identityport.BoardGovernanceAuthorityReader))
			row, _ := threads.GetByID(ctx, "190101")
			row.ThreadType = domain.ThreadTypeArticle
			if err := threads.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
			beforeThread, beforeGov, beforeEvents := threads.Snapshot(), gov.Snapshot(), events.Snapshot()
			part := &featureGovernanceTestPart{kind: domain.ThreadTypeArticle, load: func(tx context.Context, t *domain.Thread, lock bool) (any, error) {
				if lock && mode == "locked_facts" {
					return "changed", nil
				}
				if lock && mode == "locked_core" {
					actual, _ := threads.GetByID(tx, row.ID)
					actual.Content = "changed"
					if err := threads.Update(tx, actual); err != nil {
						return nil, err
					}
				}
				return "current", nil
			}, apply: func(context.Context, *domain.Thread, identityport.PrincipalRef) error {
				if mode == "apply" {
					return errors.New("required Feature write failed")
				}
				return nil
			}, check: func(tx context.Context, t *domain.Thread, a identityport.PrincipalRef) error {
				if mode == "final" {
					return errors.New("required Feature final proof failed")
				}
				if mode == "final_core" {
					actual, _ := threads.GetByID(tx, row.ID)
					actual.Content = "changed"
					return threads.Update(tx, actual)
				}
				return nil
			}}
			var p communityport.ThreadGovernanceParticipant = part
			op := "take_down"
			if mode == "nil" {
				p = (*featureGovernanceTestPart)(nil)
			}
			if mode == "discussion" {
				part.kind = domain.ThreadTypeDiscussion
			}
			if mode == "unknown_operation" {
				op = "edit"
			}
			if err := s.ExecuteThreadGovernanceCommand(ctx, row.ID, op, "actual Feature command", proof, p); err == nil {
				t.Fatal("invalid participant accepted")
			}
			if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeGov, gov.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("partial Core/history/audit/outbox persisted")
			}
		})
	}
}
