package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrAdminSessionUnavailable = errors.New("admin session service is unavailable")
	ErrAdminLoginDenied        = errors.New("admin login denied")
	ErrAdminRefreshInvalid     = errors.New("admin refresh token is invalid")
	ErrAdminRefreshReuse       = errors.New("admin refresh token reuse detected")
	ErrAdminSessionInvalid     = errors.New("admin session is invalid")
)

// AdminSessionMetadata carries non-secret request facts into session records.
type AdminSessionMetadata struct {
	DeviceName string
	UserAgent  string
	ClientIP   string
}

// AdminSessionTokens is the login/refresh result: a domain-scoped access JWT
// plus the opaque refresh credential bound to the session store digest.
type AdminSessionTokens struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

// AdminSessionService runs admin-domain login, refresh rotation and revocation
// against the independent admin session store. The JWT manager must be the
// admin domain manager; user-domain tokens never verify here.
type AdminSessionService struct {
	admins      repository.AdminDomainRepository
	sessions    repository.AdminSessionRepository
	jwt         *auth.JWTManager
	reliable    *reliability.Service
	now         func() time.Time
	ipHashKey   string
	mfaOptional bool
}

func NewAdminSessionService(admins repository.AdminDomainRepository, sessions repository.AdminSessionRepository, jwt *auth.JWTManager) *AdminSessionService {
	return &AdminSessionService{admins: admins, sessions: sessions, jwt: jwt, now: time.Now}
}

func (s *AdminSessionService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.sessions.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// SetClock replaces the wall clock in tests; production keeps time.Now.
func (s *AdminSessionService) SetClock(now func() time.Time) { s.now = now }

func (s *AdminSessionService) SetIPHashKey(key string) { s.ipHashKey = key }

// SetMFAOptional is deployment composition only; never call it from HTTP input.
// It does not change stored authentication strength or enrollments.
func (s *AdminSessionService) SetMFAOptional(optional bool) { s.mfaOptional = optional }

func adminRefreshDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func (s *AdminSessionService) ipHash(value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.ipHashKey))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *AdminSessionService) execute(ctx context.Context, code, resourceID string, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	event, err := reliability.NewEvent("authorization.changed", "admin_session", resourceID, map[string]string{
		"command": code, "resource_type": "admin_session", "resource_id": resourceID,
	})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{
		Code: code, ActorType: "anonymous", ResourceType: "admin_session", ResourceID: resourceID,
		OperationCode: code, Event: &event,
	}, action)
}

func (s *AdminSessionService) issueSession(ctx context.Context, accountID, familyID, rotatedFrom, strength string, metadata AdminSessionMetadata) (*AdminSessionTokens, error) {
	now := s.now()
	rawRefresh, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}
	sessionID := fmt.Sprintf("%d", idgen.New())
	expiresAt := now.Add(s.jwt.RefreshTTL())
	if familyID == "" {
		familyID = sessionID
	}
	if err := s.sessions.CreateSession(ctx, repository.AdminSession{
		ID: sessionID, AccountID: accountID, RefreshTokenDigest: adminRefreshDigest(rawRefresh),
		TokenFamilyID: familyID, RotatedFromID: rotatedFrom, DeviceName: metadata.DeviceName,
		UserAgent: metadata.UserAgent, IPHash: s.ipHash(metadata.ClientIP), AuthenticationStrength: strength,
		LastActiveAt: now, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	access, err := s.jwt.GenerateAccessToken(accountID, accountID, auth.AccessTokenContext{
		SessionID: sessionID, AuthVersion: 1,
	})
	if err != nil {
		return nil, err
	}
	return &AdminSessionTokens{AccessToken: access, RefreshToken: rawRefresh, ExpiresAt: now.Add(s.jwt.AccessTTL()), RefreshExpiresAt: expiresAt}, nil
}

// Login verifies the admin credential against the independent admin store and
// issues a domain-scoped access JWT plus an opaque refresh credential. Only
// active admin accounts authenticate.
func (s *AdminSessionService) Login(ctx context.Context, username, password string, metadata AdminSessionMetadata) (*AdminSessionTokens, error) {
	if s.admins == nil || s.sessions == nil || s.jwt == nil {
		return nil, ErrAdminSessionUnavailable
	}
	account, err := s.admins.GetAccountByUsername(ctx, username)
	if err != nil {
		// Admin logins may present either the account username or its contact
		// email; both resolve only inside the admin domain.
		account, err = s.admins.GetAccountByEmail(ctx, username)
	}
	if err != nil {
		return nil, ErrAdminLoginDenied
	}
	credential, err := s.admins.GetCredential(ctx, account.ID)
	if err != nil || !auth.CheckPassword(password, credential.CredentialHash) {
		return nil, ErrAdminLoginDenied
	}
	if account.Status != "active" {
		return nil, ErrAdminLoginDenied
	}
	tokens := &AdminSessionTokens{}
	err = s.execute(ctx, "identity.admin_session.login", account.ID, func(commandCtx context.Context) error {
		// Re-read inside the command transaction: suspended or revoked
		// accounts fail closed before a session commits.
		get := s.admins.GetAccountByID
		if transaction.Active(commandCtx) {
			locker, ok := s.admins.(repository.AdminAccountAuthorityLocker)
			if !ok {
				return ErrAdminSessionUnavailable
			}
			get = locker.GetAccountByIDForUpdate
		}
		current, lookupErr := get(commandCtx, account.ID)
		if lookupErr != nil || current.ID != account.ID || current.Status != "active" || current.RevokedAt != nil || current.Version < 1 {
			return ErrAdminLoginDenied
		}
		currentCredential, lookupErr := s.admins.GetCredential(commandCtx, account.ID)
		if lookupErr != nil || currentCredential.AccountID != account.ID || currentCredential.CredentialVersion < 1 || currentCredential.CredentialVersion != credential.CredentialVersion || currentCredential.CredentialHash != credential.CredentialHash || !currentCredential.PasswordChangedAt.Equal(credential.PasswordChangedAt) {
			return ErrAdminLoginDenied
		}
		issued, err := s.issueSession(commandCtx, account.ID, "", "", "password", metadata)
		if err != nil {
			return err
		}
		*tokens = *issued
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// Refresh rotates one refresh credential inside its family. Reusing a rotated
// credential revokes the whole family as a theft signal; expired sessions and
// deactivated accounts fail closed with the session revoked.
func (s *AdminSessionService) Refresh(ctx context.Context, rawRefresh string, metadata AdminSessionMetadata) (*AdminSessionTokens, error) {
	if s.sessions == nil || s.jwt == nil {
		return nil, ErrAdminSessionUnavailable
	}
	if rawRefresh == "" {
		return nil, ErrAdminRefreshInvalid
	}
	now := s.now()
	newRawRefresh, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}
	hint, err := s.sessions.GetSessionByRefreshDigestForUpdate(ctx, adminRefreshDigest(rawRefresh))
	if err != nil || hint.ID == "" || hint.AccountID == "" {
		return nil, ErrAdminRefreshInvalid
	}
	var issued *AdminSessionTokens
	var resultErr error
	err = s.execute(ctx, "identity.admin_session.refresh", adminRefreshDigest(rawRefresh), func(commandCtx context.Context) error {
		// All account-local commands lock account then session, matching lifecycle
		// and MFA commands. The digest hint is data only and re-bound under the lock.
		getAccount := s.admins.GetAccountByID
		if transaction.Active(commandCtx) {
			locker, ok := s.admins.(repository.AdminAccountAuthorityLocker)
			if !ok {
				return ErrAdminSessionUnavailable
			}
			getAccount = locker.GetAccountByIDForUpdate
		}
		account, accountErr := getAccount(commandCtx, hint.AccountID)
		current, err := s.sessions.GetSessionByRefreshDigestForUpdate(commandCtx, adminRefreshDigest(rawRefresh))
		if err != nil || current.ID != hint.ID || current.AccountID != hint.AccountID {
			return ErrAdminRefreshInvalid
		}
		if current.RevokedAt != nil {
			if current.RevokeReason == "rotated" {
				if err := s.sessions.RevokeSessionFamily(commandCtx, current.TokenFamilyID, "refresh_reuse_detected", now); err != nil {
					return err
				}
				resultErr = ErrAdminRefreshReuse
				return nil
			}
			return ErrAdminRefreshInvalid
		}
		if !now.Before(current.ExpiresAt) {
			if _, ok, err := s.sessions.RevokeSessionCAS(commandCtx, current.ID, current.Version, "expired", now); err != nil {
				return err
			} else if !ok {
				return ErrAdminRefreshInvalid
			}
			resultErr = ErrAdminRefreshInvalid
			return nil
		}
		if accountErr != nil || account.ID != current.AccountID || account.Status != "active" || account.RevokedAt != nil || account.Version < 1 {
			if _, ok, err := s.sessions.RevokeSessionCAS(commandCtx, current.ID, current.Version, "account_not_active", now); err != nil {
				return err
			} else if !ok {
				return ErrAdminRefreshInvalid
			}
			resultErr = ErrAdminRefreshInvalid
			return nil
		}
		raw := newRawRefresh
		tokens, err := s.rotateLocked(commandCtx, current, raw, metadata)
		if err != nil {
			return err
		}
		issued = tokens
		return nil
	})
	if err != nil {
		return nil, err
	}
	if resultErr != nil {
		return nil, resultErr
	}
	return issued, nil
}

func (s *AdminSessionService) rotateLocked(ctx context.Context, current repository.AdminSession, raw string, metadata AdminSessionMetadata) (*AdminSessionTokens, error) {
	now := s.now()
	if _, ok, err := s.sessions.RevokeSessionCAS(ctx, current.ID, current.Version, "rotated", now); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrAdminRefreshInvalid
	}
	strength := current.AuthenticationStrength
	mfaAt := current.MFAAuthenticatedAt
	if strength != "mfa" {
		strength = "password"
		mfaAt = nil
	}
	rawRefresh := raw
	digest := adminRefreshDigest(rawRefresh)
	sessionID := fmt.Sprintf("%d", idgen.New())
	expiresAt := now.Add(s.jwt.RefreshTTL())
	deviceName := metadata.DeviceName
	if deviceName == "" {
		deviceName = current.DeviceName
	}
	userAgent := metadata.UserAgent
	if userAgent == "" {
		userAgent = current.UserAgent
	}
	ipHash := s.ipHash(metadata.ClientIP)
	if ipHash == "" {
		ipHash = current.IPHash
	}
	if err := s.sessions.CreateSession(ctx, repository.AdminSession{
		ID: sessionID, AccountID: current.AccountID, RefreshTokenDigest: digest,
		TokenFamilyID: current.TokenFamilyID, RotatedFromID: current.ID, DeviceName: deviceName,
		UserAgent: userAgent, IPHash: ipHash, AuthenticationStrength: strength, MFAAuthenticatedAt: mfaAt,
		LastActiveAt: now, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	access, err := s.jwt.GenerateAccessToken(current.AccountID, current.AccountID, auth.AccessTokenContext{
		SessionID: sessionID, AuthVersion: 1,
	})
	if err != nil {
		return nil, err
	}
	return &AdminSessionTokens{AccessToken: access, RefreshToken: rawRefresh, ExpiresAt: now.Add(s.jwt.AccessTTL()), RefreshExpiresAt: expiresAt}, nil
}

// VerifyAccess resolves one admin access session to its current facts. The
// admin HTTP middleware consumes this Port; user sessions never resolve here.
func (s *AdminSessionService) VerifyAccess(ctx context.Context, sessionID string) (repository.AdminSession, error) {
	session, _, err := s.verifiedSession(ctx, "", sessionID)
	return session, err
}

// RevokeSession logs out one session; RevokeAllForAccount cascades after
// suspension or revocation of the owning admin account.
func (s *AdminSessionService) RevokeSession(ctx context.Context, sessionID, reason string) error {
	if s.sessions == nil {
		return ErrAdminSessionUnavailable
	}
	return s.execute(ctx, "identity.admin_session.revoke", sessionID, func(commandCtx context.Context) error {
		session, err := s.sessions.GetSessionByID(commandCtx, sessionID)
		if err != nil {
			return err
		}
		_, _, err = s.sessions.RevokeSessionCAS(commandCtx, sessionID, session.Version, reason, s.now())
		return err
	})
}

func (s *AdminSessionService) RevokeAllForAccount(ctx context.Context, accountID, reason string) error {
	if s.sessions == nil {
		return ErrAdminSessionUnavailable
	}
	return s.execute(ctx, "identity.admin_session.revoke_account", accountID, func(commandCtx context.Context) error {
		return s.sessions.RevokeSessionsForAccount(commandCtx, accountID, reason, s.now())
	})
}
