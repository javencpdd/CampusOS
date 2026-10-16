package port

import (
	"context"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type ThreadFlagResult struct {
	ID     string `json:"id"`
	Pinned bool   `json:"is_pinned"`
	Locked bool   `json:"is_locked"`
}

// Core owns resource policy and mutation. Moderation owns its current config
// and required audit. Callback arguments are detached from persisted records.
type ThreadFlagGovernanceParticipant interface {
	CheckThreadFlagGovernance(context.Context, string) error
	RecordThreadFlagGovernance(context.Context, identityport.PrincipalContext, string, *domain.Thread, *domain.Thread) error
}

type CheckedThreadFlagCommands interface {
	UserSetThreadFlag(context.Context, string, string, identityport.SessionPrincipalResolver, ThreadFlagGovernanceParticipant) (*ThreadFlagResult, error)
}
