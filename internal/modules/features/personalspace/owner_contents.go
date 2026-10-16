package space

import (
	"context"
	"errors"
	"reflect"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

var ErrAuthenticatedAuthorQueryUnavailable = errors.New("authenticated community author query is unavailable")

// ListOwnContentsForPrincipal is the authenticated owner-list entry. The owner
// comes only from a fresh user-domain session proof; a missing authenticated
// Community port never falls back to a bare-ID query or the legacy projection.
func (s *Service) ListOwnContentsForPrincipal(ctx context.Context, resolve func(context.Context) (identityport.PrincipalContext, error), page, pageSize int) ([]*SpaceContent, int64, error) {
	initial, err := resolveOwnContentsPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, 0, err
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := resolveOwnContentsPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(initial, current) {
			return current, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	return s.currentSpaceContents(ctx, "", false, false, bound, page, pageSize)
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
