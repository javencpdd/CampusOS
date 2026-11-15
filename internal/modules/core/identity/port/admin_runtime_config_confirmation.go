package port

import (
	"context"
	"errors"
)

var ErrAdminConfirmationPassword = errors.New("administrator password confirmation failed")
var ErrAdminConfirmationThrottled = errors.New("administrator password confirmation throttled")

// AdminRuntimeConfigPasswordReader is a deliberately narrow alternative to
// recent MFA for the current runtime file only. The plaintext lives in one
// request, never becomes a session strength or a reusable authorization token.
type AdminRuntimeConfigPasswordReader interface {
	LoadRuntimeConfigPasswordAuthority(context.Context, SessionPrincipalResolver, string, bool) (AdminExecutionAuthority, error)
}
