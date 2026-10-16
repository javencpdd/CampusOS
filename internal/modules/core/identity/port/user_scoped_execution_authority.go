package port

import (
	"context"
	"reflect"
	"strconv"
)

// Only the business owner supplies this query, after loading its actual board.
// This is distinct from the frozen two-action board delegation contract.
type UserScopedExecutionQuery struct {
	PermissionCode string
	Scope          PolicyResourceRef
}

func (q UserScopedExecutionQuery) Validate() error {
	if q.PermissionCode != "community.thread.pin" && q.PermissionCode != "community.thread.lock" {
		return ErrResourcePolicyUnknownAction
	}
	id, err := strconv.ParseInt(q.Scope.ID, 10, 64)
	if q.Scope.Kind != "community.board" || err != nil || id <= 0 || strconv.FormatInt(id, 10) != q.Scope.ID {
		return ErrResourcePolicyInvalid
	}
	return nil
}

type UserScopedExecutionAuthority struct {
	Principal   PrincipalContext         `json:"-"`
	Query       UserScopedExecutionQuery `json:"-"`
	Allowed     bool                     `json:"-"`
	ExpiresAtMS int64                    `json:"-"`
	Binding     any                      `json:"-"`
}

type UserScopedExecutionAuthorityReader interface {
	LoadUserScopedExecutionAuthority(context.Context, UserScopedExecutionQuery, SessionPrincipalResolver, bool) (UserScopedExecutionAuthority, error)
}

func CheckUserScopedExecutionAuthority(expected, current UserScopedExecutionAuthority) error {
	if expected.Query.Validate() != nil || expected.Principal.Validate() != nil || expected.Principal.Actor.Kind != "user" || expected.ExpiresAtMS < 1 || nilUserSessionAdapter(expected.Binding) || nilUserSessionAdapter(current.Binding) || !reflect.DeepEqual(expected, current) {
		return ErrPolicyFactsChanged
	}
	return nil
}
