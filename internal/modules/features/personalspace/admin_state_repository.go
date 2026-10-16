package space

import (
	"context"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"time"
)

// The command has already bound the actual owner and complete row. This
// adapter changes only availability and its typed provenance.
type adminSpaceStateWriter interface {
	UpdateAdminSpaceState(context.Context, string, int64, *time.Time, string, string, time.Time) (int64, error)
}

func (r *MemoryRepository) UpdateAdminSpaceState(ctx context.Context, owner string, version int64, disabledAt *time.Time, actor, reason string, updatedAt time.Time) (int64, error) {
	if !transaction.Active(ctx) {
		return 0, ErrSpaceFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.spaces[owner]
	if old == nil || old.FactsVersion != version {
		return 0, identityport.ErrPolicyFactsChanged
	}
	candidate := cloneSpace(old)
	candidate.DisabledAt, candidate.DisabledBy, candidate.DisabledReason = disabledAt, "", reason
	candidate.DisabledActorKind, candidate.DisabledActorID = "legacy_unknown", ""
	if disabledAt != nil {
		candidate.DisabledActorKind, candidate.DisabledActorID = "admin", actor
	}
	candidate.UpdatedAt = updatedAt
	if !sameSpaceMetadata(old, candidate) {
		var err error
		candidate.FactsVersion, err = r.nextFactsVersion()
		if err != nil {
			return 0, err
		}
	}
	r.spaces[owner] = cloneSpace(candidate)
	return candidate.FactsVersion, nil
}
func (r *PgRepository) UpdateAdminSpaceState(ctx context.Context, owner string, version int64, disabledAt *time.Time, actor, reason string, updatedAt time.Time) (int64, error) {
	if !transaction.Active(ctx) {
		return 0, ErrSpaceFactsUnavailable
	}
	kind, id := "legacy_unknown", ""
	if disabledAt != nil {
		kind, id = "admin", actor
	}
	var receipt int64
	err := r.db(ctx).QueryRow(ctx, `UPDATE public.user_spaces SET disabled_at=$3, disabled_by='', disabled_reason=$4,
 disabled_actor_kind=$5, disabled_actor_id=$6, updated_at=$7
 WHERE user_id=$1 AND facts_version=$2 AND deleted_at IS NULL RETURNING facts_version`, owner, version, disabledAt, reason, kind, id, updatedAt).Scan(&receipt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, identityport.ErrPolicyFactsChanged
	}
	return receipt, err
}
