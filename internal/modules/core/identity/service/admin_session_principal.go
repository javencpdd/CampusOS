package service

import (
	"context"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

// AdminGovernanceMFAWindow is the lifetime of current step-up strength. An
// expired step-up retains a valid password session and requires a new MFA
// challenge for governance; it never silently upgrades a password session.
const AdminGovernanceMFAWindow = 15 * time.Minute

func (s *AdminSessionService) verifiedSession(ctx context.Context, subject, id string) (repository.AdminSession, repository.AdminDomainAccount, error) {
	if s == nil || s.sessions == nil || s.admins == nil {
		return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionUnavailable
	}
	if err := ctx.Err(); err != nil {
		return repository.AdminSession{}, repository.AdminDomainAccount{}, err
	}
	var initial repository.AdminSession
	var err error
	if subject == "" {
		initial, err = s.sessions.GetSessionByID(ctx, id)
		if err != nil {
			return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionInvalid
		}
		subject = initial.AccountID
	}
	getAccount := s.admins.GetAccountByID
	getSession := s.sessions.GetSessionByID
	if transaction.Active(ctx) {
		accounts, ok := s.admins.(repository.AdminAccountAuthorityLocker)
		if !ok {
			return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionUnavailable
		}
		sessions, ok := s.sessions.(repository.AdminSessionAuthorityLocker)
		if !ok {
			return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionUnavailable
		}
		getAccount, getSession = accounts.GetAccountByIDForUpdate, sessions.GetSessionByIDForUpdate
	}
	// Account then session follows the same order as lifecycle commands. No
	// stale inferred subject can substitute a newly changed session owner.
	account, err := getAccount(ctx, subject)
	if err != nil || account.ID != subject || account.Status != "active" || account.RevokedAt != nil || account.Version < 1 {
		return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionInvalid
	}
	session, err := getSession(ctx, id)
	if err != nil || session.ID != id || session.AccountID != subject || session.RevokedAt != nil || session.Version < 1 || !s.now().Before(session.ExpiresAt) {
		return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionInvalid
	}
	if initial.ID != "" && session.Version != initial.Version {
		return repository.AdminSession{}, repository.AdminDomainAccount{}, ErrAdminSessionInvalid
	}
	return session, account, nil
}

func (s *AdminSessionService) CurrentAdminSession(ctx context.Context, subject, id string) (identityport.AdminSessionAuthority, error) {
	session, account, err := s.verifiedSession(ctx, subject, id)
	if err != nil {
		return identityport.AdminSessionAuthority{}, err
	}
	now := s.now()
	if !now.Before(session.ExpiresAt) {
		return identityport.AdminSessionAuthority{}, ErrAdminSessionInvalid
	}
	strength := session.AuthenticationStrength
	switch strength {
	case "password":
	case "mfa":
		if session.MFAAuthenticatedAt == nil || session.MFAAuthenticatedAt.IsZero() || session.MFAAuthenticatedAt.After(now) {
			return identityport.AdminSessionAuthority{}, ErrAdminSessionInvalid
		}
		if !now.Before(session.MFAAuthenticatedAt.Add(AdminGovernanceMFAWindow)) {
			strength = "password"
		}
	default:
		return identityport.AdminSessionAuthority{}, ErrAdminSessionInvalid
	}
	var mfaAt *time.Time
	if session.MFAAuthenticatedAt != nil {
		at := session.MFAAuthenticatedAt.UTC()
		mfaAt = &at
	}
	return identityport.AdminSessionAuthority{AccountID: account.ID, SessionID: session.ID, AuthenticationStrength: strength, AccountVersion: account.Version, SessionVersion: session.Version, ExpiresAt: session.ExpiresAt.UTC(), MFAAuthenticatedAt: mfaAt}, nil
}
