package port

import (
	"context"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// Facts are metadata only for cleanup of existing selections, never a read grant.
type PrivateIndexFacts = domain.PrivateIndexFacts

type PrivateIndexSourceReader interface {
	PrivateIndexFacts(context.Context, string) (PrivateIndexFacts, error)
	ReadOwnPrivateIndexThread(context.Context, string, identityport.SessionPrincipalResolver) (*domain.Thread, error)
}
