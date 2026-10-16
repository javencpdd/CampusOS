package port

import "context"

// UserCommandReader retains the current target account during an owning
// command's transaction. It returns the same credential-free projection as
// UserReader and supplies no authentication or mutation capability.
type UserCommandReader interface {
	GetUserForUpdate(context.Context, string) (User, error)
}
