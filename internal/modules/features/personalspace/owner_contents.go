package space

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

var ErrAuthenticatedAuthorQueryUnavailable = errors.New("authenticated community author query is unavailable")

// ListOwnContentsForPrincipal is the authenticated owner-list entry. The owner
// comes only from a fresh user-domain session proof; a missing authenticated
// Community port never falls back to a bare-ID query or the legacy projection.
func (s *Service) ListOwnContentsForPrincipal(ctx context.Context, resolve func(context.Context) (identityport.PrincipalContext, error), page, pageSize int) ([]*SpaceContent, int64, error) {
	principal, err := resolveOwnContentsPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, 0, err
	}
	query, ok := s.contentQuery.(communityport.AuthenticatedAuthorContentQuery)
	if !ok || nilOwnContentsAdapter(query) {
		return nil, 0, ErrAuthenticatedAuthorQueryUnavailable
	}
	space, err := s.GetOwnSpace(ctx, principal.Actor.ID)
	if err != nil {
		return nil, 0, err
	}
	if space == nil || space.Space == nil || space.Owner.ID != principal.Actor.ID || space.Space.UserID != principal.Actor.ID {
		return nil, 0, identityport.ErrPolicyDecisionMismatch
	}
	// Bind all Community reads to the same complete principal as the space
	// configuration read. Replacing the actor or session cannot switch owners.
	boundResolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := resolveOwnContentsPrincipal(ctx, resolve)
		if err != nil {
			return identityport.PrincipalContext{}, err
		}
		if !reflect.DeepEqual(current, principal) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	contents := make([]*SpaceContent, 0)
	var total int64
	if space.Space.SyncEnabled {
		categoryIDs := normalizedFilterValues(space.Space.SyncCategories)
		if len(categoryIDs) == 0 {
			categoryIDs = nil // No configured restriction; an empty set matches no rows.
		}
		threads, count, err := query.ListAuthorThreadsForPrincipal(ctx, boundResolve, communitydomain.ThreadListFilter{
			CategoryIDs: categoryIDs,
			AnyTags:     normalizedFilterValues(space.Space.SyncTags),
			Page:        normalizePage(page), PageSize: normalizePageSize(pageSize),
		})
		if err != nil {
			return nil, 0, fmt.Errorf("query authenticated owner profile content: %w", err)
		}
		if count < 0 {
			return nil, 0, identityport.ErrPolicyDecisionMismatch
		}
		contents = make([]*SpaceContent, 0, len(threads))
		for _, thread := range threads {
			if thread == nil || thread.AuthorID != principal.Actor.ID {
				return nil, 0, identityport.ErrPolicyDecisionMismatch
			}
			contents = append(contents, contentFromThread(thread))
		}
		total = count
	}
	// Even a disabled or empty list must not consume a revoked session. This
	// check also covers projection work after Community's final snapshot.
	if _, err := boundResolve(ctx); err != nil {
		return nil, 0, err
	}
	return contents, total, nil
}

func nilOwnContentsAdapter(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func resolveOwnContentsPrincipal(ctx context.Context, resolve func(context.Context) (identityport.PrincipalContext, error)) (identityport.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	if resolve == nil {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	principal, err := resolve(ctx)
	if err != nil {
		return identityport.PrincipalContext{}, err
	}
	if principal.Validate() != nil || principal.Actor.Kind != "user" || principal.Audience != "user" {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	return principal, nil
}
