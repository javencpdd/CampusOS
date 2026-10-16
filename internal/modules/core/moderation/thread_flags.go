package moderation

import (
	"context"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type currentThreadFlagGovernance struct {
	s         *Service
	operation OperationContext
}

func (p currentThreadFlagGovernance) CheckThreadFlagGovernance(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.s == nil || p.s.audit == nil || p.s.enabled == nil || !p.s.enabled() {
		return communityport.ErrGovernanceCommandUnavailable
	}
	config := p.s.currentConfig()
	switch operation {
	case "pin", "unpin":
		if !config.AllowPin {
			return ErrActionDisabled
		}
	case "lock", "unlock":
		if !config.AllowLock {
			return ErrActionDisabled
		}
	default:
		return identityport.ErrResourcePolicyUnknownAction
	}
	return nil
}

func (p currentThreadFlagGovernance) RecordThreadFlagGovernance(ctx context.Context, principal identityport.PrincipalContext, operation string, before, after *communitydomain.Thread) error {
	if principal.Validate() != nil || principal.Actor.Kind != "user" || before == nil || after == nil || before.ID != after.ID || before.CategoryID != after.CategoryID || after.CurrentRevision != before.CurrentRevision+1 {
		return communityport.ErrGovernanceCommandUnavailable
	}
	return p.s.audit.Log(ctx, AuditRecord{TraceID: p.operation.TraceID, ActorID: principal.Actor.ID, Action: "thread." + operation, Resource: "thread", ResourceID: before.ID,
		Before: map[string]any{"is_pinned": before.IsPinned, "is_locked": before.IsLocked}, After: map[string]any{"is_pinned": after.IsPinned, "is_locked": after.IsLocked},
		Metadata: map[string]any{"category_id": before.CategoryID}, IPAddress: p.operation.IPAddress})
}

func (p currentThreadFlagGovernance) Snapshot() any {
	if snapshot, ok := p.s.audit.(transaction.Snapshotter); ok {
		return snapshot.Snapshot()
	}
	return nil
}
func (p currentThreadFlagGovernance) Restore(value any) {
	if snapshot, ok := p.s.audit.(transaction.Snapshotter); ok {
		snapshot.Restore(value)
	}
}

func (s *Service) SetThreadFlagForPrincipal(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver, metadata OperationContext) (*communityport.ThreadFlagResult, error) {
	if s == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	checked, ok := s.community.(communityport.CheckedThreadFlagCommands)
	if !ok || checked == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	participant := currentThreadFlagGovernance{s: s, operation: metadata}
	if err := participant.CheckThreadFlagGovernance(ctx, operation); err != nil {
		return nil, err
	}
	return checked.UserSetThreadFlag(ctx, id, operation, resolve, participant)
}
