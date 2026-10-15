package portadapt

import (
	"context"

	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/middleware"
)

// AdminDomainSessionVerifier adapts the identity AdminSessionService to the
// transport middleware contract. User sessions never resolve here.
type AdminDomainSessionVerifier struct {
	sessions *service.AdminSessionService
}

func NewAdminDomainSessionVerifier(sessions *service.AdminSessionService) *AdminDomainSessionVerifier {
	return &AdminDomainSessionVerifier{sessions: sessions}
}

func (v *AdminDomainSessionVerifier) VerifyAdminSession(ctx context.Context, sessionID string) (middleware.AdminSessionFacts, error) {
	if v == nil || v.sessions == nil {
		return middleware.AdminSessionFacts{}, service.ErrAdminSessionUnavailable
	}
	session, err := v.sessions.CurrentAdminSession(ctx, "", sessionID)
	if err != nil {
		return middleware.AdminSessionFacts{}, err
	}
	return middleware.AdminSessionFacts{
		AccountID:          session.AccountID,
		Strength:           session.AuthenticationStrength,
		MFAAuthenticatedAt: session.MFAAuthenticatedAt,
	}, nil
}
