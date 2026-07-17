package port

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
)

// ErrChallengeNotDeliverable tells the compiled email-delivery Core module
// that replaying an event is safe to acknowledge. It deliberately does not
// reveal whether the Challenge expired, was consumed, or never existed.
var ErrChallengeNotDeliverable = errors.New("email challenge is no longer deliverable")

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

// EmailAccount is a credential-free projection. Consumers can branch on a
// verification state but never receive a password, challenge, ticket, refresh
// token, or raw email migration metadata.
type EmailAccount struct {
	UserID               string
	IdentifierNormalized string
	VerificationState    string
	VerifiedAt           *time.Time
	CredentialVersion    int64
}

type AccountReader interface {
	GetEmailAccount(context.Context, string) (EmailAccount, error)
}

// SessionVerifier is the narrow authentication contract consumed by HTTP
// middleware. It accepts claims, never raw token material or a database port.
type SessionVerifier interface {
	VerifyAccess(context.Context, *auth.JWTClaims) error
}

// ChallengeDispatchReader is reserved for the compiled Core email-delivery
// module. Its argument is the internal opaque challenge ID carried by the
// durable event; it returns an ephemeral code after re-checking challenge state.
// plugins, HTTP handlers, and browser clients never receive this Port.
type ChallengeDispatchReader interface {
	Dispatch(context.Context, string) (ChallengeDispatch, error)
}

type ChallengeDispatch struct {
	ChallengeID string
	PublicID    string
	Purpose     string
	Email       string
	Code        string
	ExpiresAt   time.Time
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
