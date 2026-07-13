package port

import "context"

type User struct{ ID, Username, Status string }
type UserReader interface {
	GetUser(context.Context, string) (User, error)
}
type CurrentUser interface {
	ResolveCurrentUser(context.Context) (User, error)
}
type Authorization interface {
	Check(context.Context, string, string, string) (bool, error)
}
type RoleAdministration interface {
	Assign(context.Context, string, string, string) error
	Revoke(context.Context, string, string, string) error
}

type RoleAssignment struct {
	UserID    string
	ScopeType string
	ScopeID   *int64
}

// ModerationPolicy is the public identity contract for category-scoped
// governance. It intentionally exposes no repository or role model.
type ModerationPolicy interface {
	CheckScoped(context.Context, string, string, string, string, int64) (bool, error)
	ListRoleAssignments(context.Context, string, string) ([]RoleAssignment, error)
	ReplaceCategoryRoleScopes(context.Context, string, string, []int64) (bool, error)
}
