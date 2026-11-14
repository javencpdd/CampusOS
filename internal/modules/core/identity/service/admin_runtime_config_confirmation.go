package service

import (
	"context"
	"crypto/sha256"
	"reflect"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

type adminConfirmationAttempts struct {
	Started time.Time
	Count   int
}
type runtimePasswordBinding struct {
	Authority         any
	CredentialVersion int64
	CredentialDigest  [32]byte
	ChangedAt         time.Time
}

// The limit is per current Admin account, shared by its sessions, bounded and
// local to this API generation. It never stores a password or a submitted hash.
func (s *BoardGovernanceAuthorityService) reservePasswordConfirmation(id string) error {
	s.confirmationMu.Lock()
	defer s.confirmationMu.Unlock()
	if s.confirmationAttempts == nil {
		s.confirmationAttempts = map[string]adminConfirmationAttempts{}
	}
	now := s.now()
	for key, attempts := range s.confirmationAttempts {
		if now.Sub(attempts.Started) >= time.Minute {
			delete(s.confirmationAttempts, key)
		}
	}
	a, exists := s.confirmationAttempts[id]
	if a.Count >= 5 || !exists && len(s.confirmationAttempts) >= 1024 {
		return identityport.ErrAdminConfirmationThrottled
	}
	if !exists {
		a.Started = now
	}
	a.Count++
	s.confirmationAttempts[id] = a
	return nil
}
func (s *BoardGovernanceAuthorityService) clearPasswordConfirmation(id string) {
	s.confirmationMu.Lock()
	delete(s.confirmationAttempts, id)
	s.confirmationMu.Unlock()
}

func (s *BoardGovernanceAuthorityService) LoadRuntimeConfigPasswordAuthority(ctx context.Context, resolve identityport.SessionPrincipalResolver, password string, lock bool) (identityport.AdminExecutionAuthority, error) {
	var zero identityport.AdminExecutionAuthority
	if s == nil || nilGovernanceAdapter(s.admins) || s.now == nil || lock && !transaction.Active(ctx) {
		return zero, identityport.ErrGovernanceAuthorityUnavailable
	}
	if len(password) == 0 || len(password) > 72 {
		return zero, identityport.ErrAdminConfirmationPassword
	}
	p, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return zero, err
	}
	if p.Actor.Kind != "admin" || p.Audience != "admin" {
		return zero, identityport.ErrAdminSessionPrincipalUnavailable
	}
	// Account -> credential -> role facts follows the existing credential-change
	// lock order. A reset/revocation cannot race the final file-write check.
	getAccount := s.admins.GetAccountByID
	getCredential := s.admins.GetCredential
	if lock {
		accounts, ok := s.admins.(repository.AdminAccountAuthorityLocker)
		credentials, credentialOK := s.admins.(repository.AdminLocalRecoveryCredentialLocker)
		if !ok || !credentialOK {
			return zero, identityport.ErrGovernanceAuthorityUnavailable
		}
		getAccount = accounts.GetAccountByIDForUpdate
		getCredential = credentials.GetCredentialForUpdate
	}
	account, err := getAccount(ctx, p.Actor.ID)
	if err != nil || account.Status != "active" || account.RevokedAt != nil {
		return zero, identityport.ErrAdminSessionPrincipalUnavailable
	}
	credential, err := getCredential(ctx, p.Actor.ID)
	if err != nil || credential.AccountID != p.Actor.ID || credential.CredentialVersion < 1 {
		return zero, identityport.ErrGovernanceAuthorityUnavailable
	}
	if err = s.reservePasswordConfirmation(p.Actor.ID); err != nil {
		return zero, err
	}
	if !auth.CheckPassword(password, credential.CredentialHash) {
		return zero, identityport.ErrAdminConfirmationPassword
	}
	s.clearPasswordConfirmation(p.Actor.ID)
	q := identityport.AdminExecutionQuery{PermissionCode: "platform.runtime_config.manage", Scope: identityport.PolicyResourceRef{Kind: "platform.runtime_config", ID: "current"}}
	a, err := s.loadAdminExecutionAuthority(ctx, q, resolve, lock, true)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(a.Principal, p) {
		return zero, identityport.ErrPolicyFactsChanged
	}
	current, err := getCredential(ctx, p.Actor.ID)
	if err != nil || !reflect.DeepEqual(current, credential) {
		return zero, identityport.ErrPolicyFactsChanged
	}
	a.Binding = runtimePasswordBinding{Authority: a.Binding, CredentialVersion: credential.CredentialVersion, CredentialDigest: sha256.Sum256([]byte(credential.CredentialHash)), ChangedAt: credential.PasswordChangedAt}
	return a, nil
}
