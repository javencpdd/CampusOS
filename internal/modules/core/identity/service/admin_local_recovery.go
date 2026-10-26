package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

var (
	ErrAdminLocalRecoveryInvalid     = errors.New("invalid local Admin recovery request")
	ErrAdminLocalRecoveryDenied      = errors.New("local Admin recovery target is unavailable")
	ErrAdminLocalRecoveryConflict    = errors.New("local Admin recovery facts changed")
	ErrAdminLocalRecoveryUnavailable = errors.New("local Admin recovery service is unavailable")
)

// AdminLocalRecoveryService is a trusted local maintenance Port. Only the
// interactive operator CLI constructs it after verifying its local proof.
// It is not registered with HTTP, Plugin Host, or the User recovery service.
type AdminLocalRecoveryService struct {
	admins   repository.AdminDomainRepository
	security repository.AdminSecurityRepository
	sessions repository.AdminSessionRepository
	reliable *reliability.Service
	now      func() time.Time
}

func NewAdminLocalRecoveryService(a repository.AdminDomainRepository, m repository.AdminSecurityRepository, s repository.AdminSessionRepository) *AdminLocalRecoveryService {
	return &AdminLocalRecoveryService{admins: a, security: m, sessions: s, now: time.Now}
}

func (s *AdminLocalRecoveryService) SetReliability(r *reliability.Service) {
	s.reliable = r
	if r == nil {
		return
	}
	for _, store := range []any{s.admins, s.security, s.sessions} {
		if !nilGovernanceAdapter(store) {
			if snapshot, ok := store.(transaction.Snapshotter); ok {
				r.RegisterMemorySnapshotters(snapshot)
			}
		}
	}
}

func (s *AdminLocalRecoveryService) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *AdminLocalRecoveryService) available() bool {
	if s == nil || s.reliable == nil || nilGovernanceAdapter(s.reliable.Store()) || nilGovernanceAdapter(s.admins) || nilGovernanceAdapter(s.security) || nilGovernanceAdapter(s.sessions) {
		return false
	}
	_, account := s.admins.(repository.AdminAccountAuthorityLocker)
	_, credential := s.admins.(repository.AdminLocalRecoveryCredentialLocker)
	_, security := s.security.(repository.AdminLocalRecoverySecurityStore)
	return account && credential && security
}

func validLocalAdminID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for i, ch := range []byte(id) {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || i > 0 && strings.ContainsRune("._:-", rune(ch)) {
			continue
		}
		return false
	}
	return true
}

func validLocalAdminReason(reason string) bool {
	return reason != "" && strings.TrimSpace(reason) == reason && utf8.ValidString(reason) && utf8.RuneCountInString(reason) <= 500 && !strings.ContainsAny(reason, "\r\n\x00")
}

func (s *AdminLocalRecoveryService) ResetPassword(ctx context.Context, email, password, reason string) error {
	email = domain.NormalizeEmail(email)
	if email == "" || len(email) > 254 || !utf8.ValidString(email) || len(password) < 6 || len(password) > 64 || !utf8.ValidString(password) || !validLocalAdminReason(reason) {
		return ErrAdminLocalRecoveryInvalid
	}
	if !s.available() {
		return ErrAdminLocalRecoveryUnavailable
	}
	account, err := s.admins.GetAccountByEmail(ctx, email)
	if err != nil {
		return ErrAdminLocalRecoveryDenied
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return ErrAdminLocalRecoveryInvalid
	}
	return s.recover(ctx, account.ID, email, "password_reset", hash, reason)
}

func (s *AdminLocalRecoveryService) ResetMFA(ctx context.Context, id, reason string) error {
	return s.recover(ctx, id, "", "totp_recovery", "", reason)
}

func (s *AdminLocalRecoveryService) RestoreAccount(ctx context.Context, id, reason string) error {
	return s.recover(ctx, id, "", "account_restore", "", reason)
}

type adminLocalRecoveryFacts struct {
	account    repository.AdminDomainAccount
	credential repository.AdminDomainCredential
	method     *repository.AdminTOTPMethod
}

func canonicalAdminLocalFacts(f adminLocalRecoveryFacts) adminLocalRecoveryFacts {
	f.account.CreatedAt = f.account.CreatedAt.UTC()
	f.account.UpdatedAt = f.account.UpdatedAt.UTC()
	f.account.RevokedAt = adminSelfTime(f.account.RevokedAt)
	f.credential.PasswordChangedAt = f.credential.PasswordChangedAt.UTC()
	if f.method != nil {
		copy := *f.method
		copy.EnrollmentExpiresAt = copy.EnrollmentExpiresAt.UTC()
		copy.ConfirmedAt, copy.DisabledAt = adminSelfTime(copy.ConfirmedAt), adminSelfTime(copy.DisabledAt)
		f.method = &copy
	}
	return f
}

func (s *AdminLocalRecoveryService) lockedFacts(ctx context.Context, id string) (adminLocalRecoveryFacts, error) {
	var f adminLocalRecoveryFacts
	var err error
	f.account, err = s.admins.(repository.AdminAccountAuthorityLocker).GetAccountByIDForUpdate(ctx, id)
	if err != nil || f.account.ID != id || f.account.Version < 1 || f.account.RevokedAt != nil || f.account.Status == "revoked" {
		return f, ErrAdminLocalRecoveryDenied
	}
	f.credential, err = s.admins.(repository.AdminLocalRecoveryCredentialLocker).GetCredentialForUpdate(ctx, id)
	if err != nil || f.credential.AccountID != id || f.credential.CredentialVersion < 1 || f.credential.CredentialHash == "" {
		return f, ErrAdminLocalRecoveryDenied
	}
	method, err := s.security.(repository.AdminLocalRecoverySecurityStore).GetTOTPForUpdate(ctx, id)
	if errors.Is(err, repository.ErrAdminDomainTOTPNotFound) {
		return canonicalAdminLocalFacts(f), nil
	}
	if err != nil || method.AccountID != id || method.Version < 1 || method.KeyID == "" || len(method.Nonce) < 8 || len(method.Ciphertext) < 16 || method.LastAcceptedStep < 0 || method.Status != "pending" && method.Status != "active" && method.Status != "disabled" || method.Status == "active" && method.ConfirmedAt == nil {
		return f, ErrAdminLocalRecoveryUnavailable
	}
	f.method = &method
	return canonicalAdminLocalFacts(f), nil
}

func (s *AdminLocalRecoveryService) recover(ctx context.Context, id, email, action, hash, reason string) error {
	if !validLocalAdminID(id) || !validLocalAdminReason(reason) {
		return ErrAdminLocalRecoveryInvalid
	}
	if !s.available() {
		return ErrAdminLocalRecoveryUnavailable
	}
	code := "identity.admin_local." + action
	event, err := reliability.NewEvent("authorization.changed", "admin_account", id, map[string]string{
		"command": code, "resource_type": "admin_account", "resource_id": id, "reason": reason,
	})
	if err != nil {
		return ErrAdminLocalRecoveryUnavailable
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorType: "system", ActorID: "local-cli", ResourceType: "admin_account", ResourceID: id, OperationCode: code, Event: &event}, func(tx context.Context) error {
		initial, err := s.lockedFacts(tx, id)
		if err != nil {
			return err
		}
		if email != "" && domain.NormalizeEmail(initial.account.Email) != email {
			return ErrAdminLocalRecoveryDenied
		}
		if action == "account_restore" && initial.account.Status != "suspended" || action != "account_restore" && initial.account.Status != "active" {
			return ErrAdminLocalRecoveryDenied
		}
		expected := initial
		now := s.now().UTC().Truncate(time.Microsecond)
		switch action {
		case "password_reset":
			credential, changed, err := s.admins.SetCredentialCAS(tx, id, hash, initial.credential.CredentialVersion, now)
			if err != nil || !changed {
				return ErrAdminLocalRecoveryConflict
			}
			expected.credential.CredentialHash, expected.credential.CredentialVersion, expected.credential.PasswordChangedAt = hash, initial.credential.CredentialVersion+1, now
			credential.PasswordChangedAt = credential.PasswordChangedAt.UTC()
			if !reflect.DeepEqual(credential, expected.credential) {
				return ErrAdminLocalRecoveryConflict
			}
		case "totp_recovery":
			if initial.method == nil || initial.method.Status == "disabled" {
				return ErrAdminLocalRecoveryDenied
			}
			method, changed, err := s.security.DisableTOTPCAS(tx, id, initial.method.Version, now)
			if err != nil || !changed {
				return ErrAdminLocalRecoveryConflict
			}
			copy := *initial.method
			copy.Status, copy.Version, copy.DisabledAt = "disabled", copy.Version+1, &now
			expected.method = &copy
			canonical := canonicalAdminLocalFacts(adminLocalRecoveryFacts{method: &method})
			if !reflect.DeepEqual(*canonical.method, copy) {
				return ErrAdminLocalRecoveryConflict
			}
		case "account_restore":
			account, changed, err := s.admins.SetAccountStatusCAS(tx, id, initial.account.Version, "active", reason, "local-cli", now)
			if err != nil || !changed {
				return ErrAdminLocalRecoveryConflict
			}
			expected.account.Status, expected.account.StatusReason, expected.account.StatusChangedBy = "active", reason, "local-cli"
			expected.account.Version, expected.account.UpdatedAt = initial.account.Version+1, now
			account = canonicalAdminLocalFacts(adminLocalRecoveryFacts{account: account}).account
			if !reflect.DeepEqual(account, expected.account) {
				return ErrAdminLocalRecoveryConflict
			}
		default:
			return ErrAdminLocalRecoveryInvalid
		}
		if err := s.security.(repository.AdminLocalRecoverySecurityStore).InvalidateLocalRecoveryTickets(tx, id, now); err != nil {
			return ErrAdminLocalRecoveryUnavailable
		}
		if err := s.sessions.RevokeSessionsForAccount(tx, id, "local_"+action, now); err != nil {
			return ErrAdminLocalRecoveryUnavailable
		}
		final, err := s.lockedFacts(tx, id)
		if err != nil || !reflect.DeepEqual(final, expected) {
			return ErrAdminLocalRecoveryConflict
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrAdminLocalRecoveryDenied) || errors.Is(err, ErrAdminLocalRecoveryConflict) || errors.Is(err, ErrAdminLocalRecoveryInvalid) {
			return err
		}
		return ErrAdminLocalRecoveryUnavailable
	}
	return nil
}
