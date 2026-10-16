package service

import (
	"context"
	"errors"
	"reflect"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

// Private, data-only scope for authenticated account-local factor commands.
// Current strength is derived from the actual session, never Actor's bool.
type adminMFASelfFacts struct {
	Account   repository.AdminDomainAccount
	Session   repository.AdminSession
	Authority identityport.AdminSessionAuthority
	Method    *repository.AdminTOTPMethod
}

func (s *AdminMFAService) loadSelfFacts(ctx context.Context, actor AdminActor) (adminMFASelfFacts, error) {
	if s == nil || nilGovernanceAdapter(s.admins) || nilGovernanceAdapter(s.sessions) || nilGovernanceAdapter(s.security) {
		return adminMFASelfFacts{}, ErrAdminMFAAvailable
	}
	if actor.ID == "" || actor.CredentialID == "" {
		return adminMFASelfFacts{}, ErrAdminMFAActor
	}
	reader := NewAdminSessionService(s.admins, s.sessions, nil)
	reader.SetClock(s.now)
	session, account, err := reader.verifiedSession(ctx, actor.ID, actor.CredentialID)
	if err != nil {
		return adminMFASelfFacts{}, errors.Join(ErrAdminMFAActor, err)
	}
	authority, err := reader.CurrentAdminSession(ctx, actor.ID, actor.CredentialID)
	if err != nil || authority.AccountVersion != account.Version || authority.SessionVersion != session.Version {
		return adminMFASelfFacts{}, errors.Join(ErrAdminMFAActor, err)
	}
	method, err := s.security.GetTOTP(ctx, actor.ID)
	var value *repository.AdminTOTPMethod
	if !errors.Is(err, repository.ErrAdminDomainTOTPNotFound) {
		if err != nil {
			return adminMFASelfFacts{}, err
		}
		if method.AccountID != actor.ID || method.Version < 1 || method.KeyID == "" || len(method.Nonce) < 8 || len(method.Ciphertext) < 16 || method.LastAcceptedStep < 0 || (method.Status != "pending" && method.Status != "active" && method.Status != "disabled") || method.Status == "active" && method.ConfirmedAt == nil {
			return adminMFASelfFacts{}, ErrAdminMFAAvailable
		}
		method.EnrollmentExpiresAt = method.EnrollmentExpiresAt.UTC()
		method.ConfirmedAt = adminSelfTime(method.ConfirmedAt)
		method.DisabledAt = adminSelfTime(method.DisabledAt)
		value = &method
	}
	account.CreatedAt = account.CreatedAt.UTC()
	account.UpdatedAt = account.UpdatedAt.UTC()
	account.RevokedAt = adminSelfTime(account.RevokedAt)
	session.CreatedAt = session.CreatedAt.UTC()
	session.UpdatedAt = session.UpdatedAt.UTC()
	session.LastActiveAt = session.LastActiveAt.UTC()
	session.ExpiresAt = session.ExpiresAt.UTC()
	session.MFAAuthenticatedAt = adminSelfTime(session.MFAAuthenticatedAt)
	session.RevokedAt = adminSelfTime(session.RevokedAt)
	return adminMFASelfFacts{account, session, authority, value}, nil
}
func cloneAdminMFASelfFacts(f adminMFASelfFacts) adminMFASelfFacts {
	f.Account.RevokedAt = adminSelfTime(f.Account.RevokedAt)
	f.Session.MFAAuthenticatedAt = adminSelfTime(f.Session.MFAAuthenticatedAt)
	f.Session.RevokedAt = adminSelfTime(f.Session.RevokedAt)
	f.Authority.MFAAuthenticatedAt = adminSelfTime(f.Authority.MFAAuthenticatedAt)
	if f.Method != nil {
		m := *f.Method
		m.ConfirmedAt = adminSelfTime(m.ConfirmedAt)
		m.DisabledAt = adminSelfTime(m.DisabledAt)
		f.Method = &m
	}
	return f
}
func (s *AdminMFAService) selfMutation(ctx context.Context, actor AdminActor, code, resource string, initial adminMFASelfFacts, expected adminMFASelfFacts, write func(context.Context) error) error {
	return s.execute(ctx, code, resource, actor.ID, func(tx context.Context) error {
		current, err := s.loadSelfFacts(tx, actor)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := write(tx); err != nil {
			return err
		}
		current, err = s.loadSelfFacts(tx, actor)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(expected, current) {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
}
