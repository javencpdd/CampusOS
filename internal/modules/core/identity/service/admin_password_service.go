package service

import (
	"context"
	"errors"
	"reflect"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/auth"
)

var (
	ErrAdminPasswordInvalid = errors.New("new administrator password must contain 6 to 64 bytes and differ from the current password")
	ErrAdminPasswordCurrent = errors.New("current administrator password is incorrect")
	ErrAdminPasswordMFA     = errors.New("recent administrator MFA is required")
)

// ChangePassword changes only the current authenticated Admin. Account and
// credential locks serialize it with login/recovery; all Admin sessions and
// outstanding recovery tickets are revoked in the same required-audit command.
func (s *AdminSelfService) ChangePassword(ctx context.Context, resolve identityport.SessionPrincipalResolver, currentPassword, newPassword string) error {
	if len(newPassword) < 6 || len(newPassword) > 64 || currentPassword == newPassword {
		return ErrAdminPasswordInvalid
	}
	if s == nil || s.sessions == nil || s.sessions.reliable == nil || resolve == nil || s.admins == nil || s.security == nil {
		return ErrAdminSelfUnavailable
	}
	accountLocker, ok := s.admins.(repository.AdminAccountAuthorityLocker)
	credentialLocker, credentialOK := s.admins.(repository.AdminLocalRecoveryCredentialLocker)
	security, securityOK := s.security.(repository.AdminLocalRecoverySecurityStore)
	if !ok || !credentialOK || !securityOK {
		return ErrAdminSelfUnavailable
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil || p.Actor.Kind != "admin" || p.Audience != "admin" {
		return identityport.ErrAdminSessionPrincipalUnavailable
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return ErrAdminPasswordInvalid
	}
	event, err := reliability.NewEvent("authorization.changed", "admin_account", p.Actor.ID, map[string]string{"command": "identity.admin_self.password.change", "resource_type": "admin_account", "resource_id": p.Actor.ID})
	if err != nil {
		return err
	}
	return s.sessions.reliable.Execute(ctx, reliability.Command{Code: "identity.admin_self.password.change", ActorType: "admin", ActorID: p.Actor.ID, ResourceType: "admin_account", ResourceID: p.Actor.ID, OperationCode: "identity.admin_self.password.change", Event: &event}, func(tx context.Context) error {
		account, err := accountLocker.GetAccountByIDForUpdate(tx, p.Actor.ID)
		if err != nil || account.Status != "active" || account.RevokedAt != nil {
			return ErrAdminSessionInvalid
		}
		credential, err := credentialLocker.GetCredentialForUpdate(tx, p.Actor.ID)
		if err != nil {
			return ErrAdminSelfUnavailable
		}
		current, err := resolve(tx)
		if err != nil || !reflect.DeepEqual(current, p) {
			return ErrAdminSessionInvalid
		}
		if _, _, err := s.sessions.verifiedSession(tx, p.Actor.ID, p.CredentialID); err != nil {
			return err
		}
		if !auth.CheckPassword(currentPassword, credential.CredentialHash) {
			return ErrAdminPasswordCurrent
		}
		method, err := security.GetTOTPForUpdate(tx, p.Actor.ID)
		if err != nil && !errors.Is(err, repository.ErrAdminDomainTOTPNotFound) {
			return ErrAdminSelfUnavailable
		}
		if err == nil && method.Status == "active" && !p.AdminMFAOptional {
			authority, err := s.sessions.CurrentAdminSession(tx, p.Actor.ID, p.CredentialID)
			if err != nil || authority.AuthenticationStrength != "mfa" || authority.MFAAuthenticatedAt == nil || authority.MFAAuthenticatedAt.After(s.sessions.now()) || s.sessions.now().Sub(*authority.MFAAuthenticatedAt) > AdminGovernanceMFAWindow {
				return ErrAdminPasswordMFA
			}
		}
		at := s.sessions.now().UTC().Truncate(time.Microsecond)
		if _, changed, err := s.admins.SetCredentialCAS(tx, p.Actor.ID, hash, credential.CredentialVersion, at); err != nil {
			return err
		} else if !changed {
			return identityport.ErrPolicyFactsChanged
		}
		if err := security.InvalidateLocalRecoveryTickets(tx, p.Actor.ID, at); err != nil {
			return err
		}
		return s.sessions.sessions.RevokeSessionsForAccount(tx, p.Actor.ID, "password_changed", at)
	})
}
