package port

import "context"

// User is Identity's public profile projection for other modules. It excludes
// credentials and account internals while retaining the fields that profile
// features are allowed to render.
type User struct {
	ID       string
	Username string
	Nickname string
	Email    string
	Avatar   string
	Bio      string
	Status   string
}
type UserReader interface {
	GetUser(context.Context, string) (User, error)
	GetUserByUsername(context.Context, string) (User, error)
}
type CurrentUser interface {
	ResolveCurrentUser(context.Context) (User, error)
}
type Authorization interface {
	Check(context.Context, string, string, string) (bool, error)
	CheckCode(context.Context, string, string) (bool, error)
	CheckCodeScoped(context.Context, string, string, string, int64) (bool, error)
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
	ReplaceCategoryRoleScopesByActor(context.Context, string, string, string, []int64) (bool, error)
}
