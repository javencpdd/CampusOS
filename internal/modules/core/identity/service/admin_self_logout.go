package service

import (
	"context"
	"reflect"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

// RevokeCurrentSession accepts only the current independent Admin subject.
// Management revocation of another session remains a separate permissioned command.
func (s *AdminSessionService) RevokeCurrentSession(ctx context.Context, resolve identityport.SessionPrincipalResolver) error {
	if s == nil || s.reliable == nil || resolve == nil {
		return ErrAdminSessionUnavailable
	}
	p, err := resolve(ctx)
	if err != nil {
		return err
	}
	initial, account, err := s.verifiedSession(ctx, p.Actor.ID, p.CredentialID)
	if err != nil {
		return err
	}
	initial = canonicalAdminLogoutSession(initial)
	r := identityport.AdminSelfReadRequest{RequestID: "admin-own-logout", PolicyVersion: identityport.AdminSelfReadPolicyVersion, Principal: p, AccountID: account.ID, Status: account.Status, SessionID: initial.ID, AccountVersion: account.Version, SessionVersion: initial.Version, ExpiresAtMS: initial.ExpiresAt.UnixMilli(), EvaluatedAtMS: s.now().UnixMilli()}
	d, err := (policy.AdminSelfReadPolicy{}).DecideAdminSelfRead(r)
	if err != nil || identityport.CheckAdminSelfReadDecision(r, d, r) != nil {
		return ErrAdminSessionInvalid
	}
	event, err := reliability.NewEvent("authorization.changed", "admin_session", initial.ID, map[string]string{"command": "identity.admin_session.logout", "resource_type": "admin_session", "resource_id": initial.ID})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{Code: "identity.admin_session.logout", ActorType: "admin", ActorID: p.Actor.ID, ResourceType: "admin_session", ResourceID: initial.ID, OperationCode: "identity.admin_session.logout", Event: &event}, func(commandCtx context.Context) error {
		current, err := resolve(commandCtx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, p) {
			return identityport.ErrPolicyFactsChanged
		}
		session, active, err := s.verifiedSession(commandCtx, p.Actor.ID, p.CredentialID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(canonicalAdminLogoutSession(session), initial) || !reflect.DeepEqual(active, account) {
			return identityport.ErrPolicyFactsChanged
		}
		at := s.now().UTC().Truncate(time.Microsecond)
		expected := canonicalAdminLogoutSession(session)
		expected.RevokedAt = &at
		expected.RevokeReason = "logout"
		expected.UpdatedAt = at
		expected.Version++
		receipt, ok, err := s.sessions.RevokeSessionCAS(commandCtx, session.ID, session.Version, "logout", at)
		if err != nil {
			return err
		}
		if !ok || !reflect.DeepEqual(canonicalAdminLogoutSession(receipt), expected) {
			return identityport.ErrPolicyFactsChanged
		}
		actual, err := s.sessions.GetSessionByID(commandCtx, session.ID)
		if err != nil {
			return err
		}
		finalAccount, err := s.admins.GetAccountByID(commandCtx, p.Actor.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(canonicalAdminLogoutSession(actual), expected) || !reflect.DeepEqual(finalAccount, account) {
			return identityport.ErrPolicyFactsChanged
		}
		return commandCtx.Err()
	})
}

// PostgreSQL scans and Memory may use different time locations. Preserve every
// field and instant while comparing the same canonical representation.
func canonicalAdminLogoutSession(s repository.AdminSession) repository.AdminSession {
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	s.LastActiveAt = s.LastActiveAt.UTC()
	s.ExpiresAt = s.ExpiresAt.UTC()
	s.MFAAuthenticatedAt = adminSelfTime(s.MFAAuthenticatedAt)
	s.RevokedAt = adminSelfTime(s.RevokedAt)
	return s
}
