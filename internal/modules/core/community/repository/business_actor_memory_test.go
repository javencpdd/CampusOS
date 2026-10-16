package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func TestThreadGovernanceMemoryHistoryActors(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryContentGovernanceRepository()
	if err := r.CreateRevision(ctx, &domain.ContentRevision{ID: "revision", ThreadID: "thread", CreatedBy: "123"}); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateModerationCase(ctx, &domain.ModerationCase{ID: "case", ThreadID: "thread", OpenedBy: "123"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveModerationCase(ctx, "case", "resolved", "124", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateModerationAction(ctx, &domain.ModerationAction{ID: "action", ThreadID: "thread", ActorKind: "admin", ActorRefID: "123"}); err != nil {
		t.Fatal(err)
	}
	var snapshot memoryContentGovernanceSnapshot
	if err := json.Unmarshal(r.Snapshot().([]byte), &snapshot); err != nil {
		t.Fatal(err)
	}
	revision := snapshot.Revisions["thread"][0]
	c := snapshot.Cases["thread"][0]
	action := snapshot.Actions["thread"][0]
	if revision.CreatedActorKind != "user" || revision.CreatedActorID != "123" || c.OpenedActorKind != "user" || c.OpenedActorID != "123" || c.ResolvedActorKind != "user" || c.ResolvedActorID != "124" || action.ActorKind != "admin" || action.ActorRefID != "123" || action.ActorID != "" {
		t.Fatal("legacy and typed domains diverged", revision, c, action)
	}
}
