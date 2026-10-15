package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestThreadAdminReviewCaseCommandMemoryBindingAndDomain(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryContentGovernanceRepository()
	now := time.Now().UTC()
	row := &domain.ModerationCase{ID: "case.review", ThreadID: "thread.review", Status: "pending", Reason: "before", OpenedBy: "user.owner", OpenedAt: now}
	if err := r.CreateModerationCase(ctx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LockLatestOpenCase(ctx, row.ThreadID); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	err := transaction.NewMemory(r).Within(ctx, func(tx context.Context) error {
		actual, err := r.LockLatestOpenCase(tx, row.ThreadID)
		if err != nil || actual == nil {
			t.Fatal(actual, err)
		}
		if actual.OpenedActorKind != "user" || actual.OpenedActorID != row.OpenedBy {
			t.Fatal(actual)
		}
		changed := *actual
		changed.Reason = "stale"
		if _, err := r.ResolveCaseForActor(tx, &changed, "resolved_approve", "admin", "adm.opaque", now); !errors.Is(err, ErrModerationCaseChanged) {
			t.Fatal("stale case accepted", err)
		}
		if _, err := r.ResolveCaseForActor(tx, actual, "resolved_approve", "user", "user.owner", now); !errors.Is(err, ErrModerationCaseChanged) {
			t.Fatal("domain fallback", err)
		}
		resolved, err := r.ResolveCaseForActor(tx, actual, "resolved_approve", "admin", "adm.opaque", now)
		if err != nil || resolved.ResolvedBy != "" || resolved.ResolvedActorKind != "admin" || resolved.ResolvedActorID != "adm.opaque" || resolved.OpenedBy != row.OpenedBy {
			t.Fatal(resolved, err)
		}
		*resolved.ResolvedAt = now.Add(time.Hour)
		current, err := r.LockModerationCase(tx, actual.ID)
		if err != nil || !current.ResolvedAt.Equal(now) {
			t.Fatal("returned case alias", current, err)
		}
		if open, err := r.LockLatestOpenCase(tx, row.ThreadID); err != nil || open != nil {
			t.Fatal(open, err)
		}
		if _, err := r.ResolveCaseForActor(tx, actual, "resolved_approve", "admin", "adm.other", now); !errors.Is(err, ErrModerationCaseChanged) {
			t.Fatal("resolved case replay", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
