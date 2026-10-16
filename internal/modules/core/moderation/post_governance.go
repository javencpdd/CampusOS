package moderation

import (
	"context"
	"errors"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrPostGovernanceUnavailable = errors.New("current post governance unavailable")

type currentPostGovernance struct {
	s         *Service
	operation OperationContext
}

func (p currentPostGovernance) CheckPostGovernance(context.Context) error {
	if p.s == nil || p.s.audit == nil || !p.s.enabled() {
		return ErrPostGovernanceUnavailable
	}
	if !p.s.currentConfig().AllowDeletePost {
		return ErrActionDisabled
	}
	return nil
}
func (p currentPostGovernance) RecordPostGovernance(ctx context.Context, principal identityport.PrincipalContext, parent *communitydomain.Thread, post *communitydomain.Post) error {
	if principal.Validate() != nil || principal.Actor.Kind != "user" || parent == nil || post == nil || post.ThreadID != parent.ID {
		return ErrPostGovernanceUnavailable
	}
	return p.s.audit.Log(ctx, AuditRecord{TraceID: p.operation.TraceID, ActorID: principal.Actor.ID, Action: "post.delete", Resource: "post", ResourceID: post.ID, Before: map[string]any{"thread_id": post.ThreadID, "author_id": post.AuthorID, "floor_number": post.FloorNumber}, After: map[string]any{"deleted": true}, Metadata: map[string]any{"category_id": parent.CategoryID, "thread_id": parent.ID}, IPAddress: p.operation.IPAddress})
}
func (p currentPostGovernance) Snapshot() any {
	if s, ok := p.s.audit.(transaction.Snapshotter); ok {
		return s.Snapshot()
	}
	return nil
}
func (p currentPostGovernance) Restore(v any) {
	if s, ok := p.s.audit.(transaction.Snapshotter); ok {
		s.Restore(v)
	}
}
func (s *Service) DeletePostForPrincipal(ctx context.Context, threadID, postID string, resolve identityport.SessionPrincipalResolver, operation OperationContext) error {
	checked, ok := s.community.(communityport.CheckedPostGovernanceCommands)
	if !ok {
		return ErrPostGovernanceUnavailable
	}
	participant := currentPostGovernance{s: s, operation: operation}
	if err := participant.CheckPostGovernance(ctx); err != nil {
		return err
	}
	return checked.DeletePostForPrincipal(ctx, threadID, postID, resolve, participant)
}
