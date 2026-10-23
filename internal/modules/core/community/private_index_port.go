package community

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type privateIndexReader struct {
	module *Module
	facts  interface {
		PrivateIndexFacts(context.Context, string) (communityport.PrivateIndexFacts, error)
	}
}

func (r privateIndexReader) PrivateIndexFacts(ctx context.Context, id string) (communityport.PrivateIndexFacts, error) {
	return r.facts.PrivateIndexFacts(ctx, id)
}
func (r privateIndexReader) ReadOwnPrivateIndexThread(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if r.module.threadService == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	row, e := r.module.threadService.GetFeatureThreadForPrincipal(ctx, id, resolve)
	if errors.Is(e, repository.ErrThreadNotFound) {
		return nil, communityport.ErrThreadNotFound
	}
	return row, e
}

// This first-party checked reader is not a plugin AppContext capability.
func (m *Module) PrivateIndexReader() communityport.PrivateIndexSourceReader {
	facts, ok := m.threads.(interface {
		PrivateIndexFacts(context.Context, string) (communityport.PrivateIndexFacts, error)
	})
	if !ok {
		return nil
	}
	return privateIndexReader{module: m, facts: facts}
}
