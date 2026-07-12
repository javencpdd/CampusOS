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
