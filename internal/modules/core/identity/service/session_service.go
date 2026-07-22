package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
)

var (
	ErrSessionInvalid       = errors.New("identity session is invalid")
	ErrRefreshTokenInvalid  = errors.New("identity refresh token is invalid")
	ErrRefreshTokenReuse    = errors.New("identity refresh token reuse detected")
	ErrSessionNotOwned      = errors.New("identity session does not belong to the current user")
	ErrSessionConfiguration = errors.New("identity session service is unavailable")
)

type SessionConfig struct {
	IPHashSecret string
	Clock        func() time.Time
}

// SessionMetadata is intentionally transport-neutral; the HTTP handler fills
// it from a request but the service only stores a keyed IP digest.
type SessionMetadata struct {
	DeviceID   string
	DeviceName string
	DeviceType string
	ClientIP   string
	UserAgent  string
}

type SessionTokens struct {
	AccessToken  string
	RefreshToken string
	Session      *domain.Session
	ExpiresIn    int
}

// SessionService is the sole issuer and verifier for v12 browser/API sessions.
// JWT signing is not enough: every access claim is checked against session and
// user state before a route is granted.
type SessionService struct {
	sessions     repository.SessionRepository
	users        repository.UserRepository
	authVersions repository.AuthVersionWriter
	jwt          *auth.JWTManager
	reliable     *reliability.Service
	ipHashKey    []byte
	clock        func() time.Time
	meter        observability.Meter
}

func (s *SessionService) SetMeter(meter observability.Meter) {
	if s != nil {
		s.meter = meter
	}
}

func NewSessionService(sessions repository.SessionRepository, users repository.UserRepository, jwtManager *auth.JWTManager, config SessionConfig) (*SessionService, error) {
	if sessions == nil || users == nil || jwtManager == nil {
		return nil, ErrSessionConfiguration
	}
	authVersions, ok := users.(repository.AuthVersionWriter)
	if !ok {
		return nil, errors.New("identity user repository does not implement auth-version invalidation")
	}
	if strings.TrimSpace(config.IPHashSecret) == "" {
		return nil, errors.New("identity session IP hash secret is required")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &SessionService{
		sessions: sessions, users: users, authVersions: authVersions, jwt: jwtManager,
		ipHashKey: []byte(config.IPHashSecret), clock: config.Clock,
	}, nil
}

func (s *SessionService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.sessions.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

func (s *SessionService) Issue(ctx context.Context, user *domain.User, metadata SessionMetadata) (*SessionTokens, error) {
	return s.IssueWithAuthentication(ctx, user, metadata, domain.MFAAuthenticationPassword, nil)
}

// IssueWithAuthentication persists the verified authentication strength with
// the server-side Session. MFA is asserted only after MFAService has consumed
// a valid one-time ticket; callers cannot synthesize it from a JWT claim.
func (s *SessionService) IssueWithAuthentication(ctx context.Context, user *domain.User, metadata SessionMetadata, strength domain.MFAAuthenticationStrength, mfaVerifiedAt *time.Time) (*SessionTokens, error) {
	if user == nil || user.ID == "" || user.Status != domain.UserStatusActive || user.AuthVersion < 1 {
		s.observeSession("issue", "invalid")
		return nil, ErrSessionInvalid
	}
	now := s.now()
	if strength != domain.MFAAuthenticationTOTP {
		strength = domain.MFAAuthenticationPassword
		mfaVerifiedAt = nil
	} else if mfaVerifiedAt == nil {
		stamp := now
		mfaVerifiedAt = &stamp
	}
	rawRefresh, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		s.observeSession("issue", "error")
		return nil, err
	}
	sessionID := fmt.Sprintf("%d", idgen.New())
	session := &domain.Session{
		ID: sessionID, UserID: user.ID, RefreshTokenDigest: refreshDigest(rawRefresh), TokenFamilyID: fmt.Sprintf("%d", idgen.New()),
		DeviceID: clip(metadata.DeviceID, 128), DeviceName: defaultDeviceName(metadata.DeviceName), DeviceType: defaultDeviceType(metadata.DeviceType),
		IPHash: s.ipHash(metadata.ClientIP), UserAgent: clip(metadata.UserAgent, 512),
		AuthenticationStrength: strength, MFAAuthenticatedAt: cloneSessionServiceTime(mfaVerifiedAt),
		LastActiveAt: now, ExpiresAt: now.Add(s.jwt.RefreshTTL()), CreatedAt: now, UpdatedAt: now,
	}
	accessToken, err := s.jwt.GenerateAccessToken(user.ID, user.Username, auth.AccessTokenContext{SessionID: session.ID, AuthVersion: user.AuthVersion})
	if err != nil {
		s.observeSession("issue", "error")
		return nil, fmt.Errorf("generate session access token: %w", err)
	}
	if err := s.execute(ctx, reliability.Command{
		Code: "identity.session.issue", ActorID: user.ID, ResourceType: "identity_session", ResourceID: session.ID,
		OperationCode: "identity.session.issue", IdempotencyKey: "identity.session.issue:" + session.ID,
	}, func(commandCtx context.Context) error {
		return s.sessions.Create(commandCtx, session)
	}); err != nil {
		s.observeSession("issue", "error")
		return nil, err
	}
	s.observeSession("issue", "success")
	return &SessionTokens{AccessToken: accessToken, RefreshToken: rawRefresh, Session: session, ExpiresIn: durationSeconds(s.jwt.AccessTTL())}, nil
}

// Refresh rotates a one-time opaque token. A previously rotated token is a
// theft signal, so that branch commits a whole-family revocation before it
// returns ErrRefreshTokenReuse to the caller.
func (s *SessionService) Refresh(ctx context.Context, rawRefresh string, metadata SessionMetadata) (*SessionTokens, error) {
	if strings.TrimSpace(rawRefresh) == "" {
		s.observeSession("refresh", "invalid")
		return nil, ErrRefreshTokenInvalid
	}
	newRawRefresh, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		s.observeSession("refresh", "error")
		return nil, err
	}
	now := s.now()
	var issued *domain.Session
	var user *domain.User
	var resultErr error
	err = s.execute(ctx, reliability.Command{
		Code: "identity.session.refresh", ActorType: "anonymous", ResourceType: "identity_session",
		OperationCode: "identity.session.refresh",
	}, func(commandCtx context.Context) error {
		current, err := s.sessions.GetByRefreshDigestForUpdate(commandCtx, refreshDigest(rawRefresh))
		if errors.Is(err, repository.ErrSessionNotFound) {
			resultErr = ErrRefreshTokenInvalid
			return nil
		}
		if err != nil {
			return err
		}
		if current.RevokedAt != nil {
			if current.RevokeReason == "rotated" {
				if _, err := s.sessions.RevokeFamily(commandCtx, current.TokenFamilyID, "refresh_reuse_detected", now); err != nil {
					return err
				}
				resultErr = ErrRefreshTokenReuse
				return nil
			}
			resultErr = ErrRefreshTokenInvalid
			return nil
		}
		if !now.Before(current.ExpiresAt) {
			stamp := now
			current.RevokedAt = &stamp
			current.RevokeReason = "expired"
			current.UpdatedAt = now
			if err := s.sessions.Update(commandCtx, current); err != nil {
				return err
			}
			resultErr = ErrRefreshTokenInvalid
			return nil
		}
		user, err = s.users.GetByID(commandCtx, current.UserID)
		if err != nil {
			resultErr = ErrRefreshTokenInvalid
			return nil
		}
		if user.Status != domain.UserStatusActive || user.AuthVersion < 1 {
			stamp := now
			current.RevokedAt = &stamp
			current.RevokeReason = "user_not_active"
			current.UpdatedAt = now
			if err := s.sessions.Update(commandCtx, current); err != nil {
				return err
			}
			resultErr = ErrRefreshTokenInvalid
			return nil
		}
		issued = &domain.Session{
			ID: fmt.Sprintf("%d", idgen.New()), UserID: current.UserID, RefreshTokenDigest: refreshDigest(newRawRefresh),
			TokenFamilyID: current.TokenFamilyID, RotatedFromID: current.ID,
			DeviceID: clip(metadata.DeviceID, 128), DeviceName: defaultDeviceName(metadata.DeviceName), DeviceType: defaultDeviceType(metadata.DeviceType),
			IPHash: s.ipHash(metadata.ClientIP), UserAgent: clip(metadata.UserAgent, 512),
			AuthenticationStrength: current.AuthenticationStrength, MFAAuthenticatedAt: cloneSessionServiceTime(current.MFAAuthenticatedAt),
			LastActiveAt: now, ExpiresAt: now.Add(s.jwt.RefreshTTL()), CreatedAt: now, UpdatedAt: now,
		}
		if issued.DeviceID == "" {
			issued.DeviceID = current.DeviceID
		}
		if metadata.DeviceName == "" {
			issued.DeviceName = current.DeviceName
		}
		if metadata.DeviceType == "" {
			issued.DeviceType = current.DeviceType
		}
		if issued.AuthenticationStrength != domain.MFAAuthenticationTOTP {
			issued.AuthenticationStrength = domain.MFAAuthenticationPassword
			issued.MFAAuthenticatedAt = nil
		}
		stamp := now
		current.RevokedAt = &stamp
		current.RevokeReason = "rotated"
		current.RotatedToID = issued.ID
		current.LastActiveAt = now
		current.UpdatedAt = now
		if err := s.sessions.Update(commandCtx, current); err != nil {
			return err
		}
		return s.sessions.Create(commandCtx, issued)
	})
	if err != nil {
		s.observeSession("refresh", "error")
		return nil, err
	}
	if resultErr != nil {
		if errors.Is(resultErr, ErrRefreshTokenReuse) {
			s.observeSession("refresh", "reuse")
		} else {
			s.observeSession("refresh", "invalid")
		}
		return nil, resultErr
	}
	if issued == nil || user == nil {
		s.observeSession("refresh", "invalid")
		return nil, ErrRefreshTokenInvalid
	}
	accessToken, err := s.jwt.GenerateAccessToken(user.ID, user.Username, auth.AccessTokenContext{SessionID: issued.ID, AuthVersion: user.AuthVersion})
	if err != nil {
		s.observeSession("refresh", "error")
		return nil, fmt.Errorf("generate refreshed access token: %w", err)
	}
	s.observeSession("refresh", "success")
	return &SessionTokens{AccessToken: accessToken, RefreshToken: newRawRefresh, Session: issued, ExpiresIn: durationSeconds(s.jwt.AccessTTL())}, nil
}

// VerifyAccess implements middleware.AccessSessionVerifier without making the
// middleware depend on the Identity module.
func (s *SessionService) VerifyAccess(ctx context.Context, claims *auth.JWTClaims) error {
	if claims == nil || claims.TokenType != auth.AccessTokenType || claims.SessionID == "" || claims.AuthVersion < 1 || claims.UserID == "" {
		s.observeSession("verify", "invalid")
		return ErrSessionInvalid
	}
	session, err := s.sessions.GetByID(ctx, claims.SessionID)
	if err != nil || session.UserID != claims.UserID || session.RevokedAt != nil || !s.now().Before(session.ExpiresAt) {
		s.observeSession("verify", "invalid")
		return ErrSessionInvalid
	}
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil || user.Status != domain.UserStatusActive || user.AuthVersion != claims.AuthVersion {
		s.observeSession("verify", "invalid")
		return ErrSessionInvalid
	}
	s.observeSession("verify", "success")
	return nil
}

// MarkMFA records a successful current-session step-up and returns a fresh
// Access Token bound to the same server-side session. The Refresh credential
// remains HttpOnly and is not returned or rotated by this operation.
func (s *SessionService) MarkMFA(ctx context.Context, userID, sessionID string) (*SessionTokens, error) {
	if s == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, ErrSessionInvalid
	}
	var updated *domain.Session
	var user *domain.User
	err := s.execute(ctx, reliability.Command{
		Code: "identity.session.mfa_step_up", ActorID: userID, ResourceType: "identity_session", ResourceID: sessionID,
		OperationCode: "identity.session.mfa_step_up", IdempotencyKey: "identity.session.mfa_step_up:" + userID + ":" + sessionID + ":" + fmt.Sprintf("%d", s.now().UnixNano()),
	}, func(commandCtx context.Context) error {
		current, lookupErr := s.sessions.GetByIDForUpdate(commandCtx, sessionID)
		if lookupErr != nil || current.UserID != userID || current.RevokedAt != nil || !s.now().Before(current.ExpiresAt) {
			return ErrSessionInvalid
		}
		user, lookupErr = s.users.GetByID(commandCtx, userID)
		if lookupErr != nil || user.Status != domain.UserStatusActive || user.AuthVersion < 1 {
			return ErrSessionInvalid
		}
		now := s.now()
		current.AuthenticationStrength = domain.MFAAuthenticationTOTP
		current.MFAAuthenticatedAt = &now
		current.LastActiveAt = now
		current.UpdatedAt = now
		if updateErr := s.sessions.Update(commandCtx, current); updateErr != nil {
			return updateErr
		}
		updated = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	if updated == nil || user == nil {
		return nil, ErrSessionInvalid
	}
	accessToken, err := s.jwt.GenerateAccessToken(user.ID, user.Username, auth.AccessTokenContext{SessionID: updated.ID, AuthVersion: user.AuthVersion})
	if err != nil {
		return nil, fmt.Errorf("generate MFA step-up access token: %w", err)
	}
	s.observeSession("mfa_step_up", "success")
	return &SessionTokens{AccessToken: accessToken, Session: updated, ExpiresIn: durationSeconds(s.jwt.AccessTTL())}, nil
}

// HasRecentMFA is a narrow Port for future high-risk command guards. It reads
// authoritative session state rather than trusting client-side timestamps.
func (s *SessionService) HasRecentMFA(ctx context.Context, userID, sessionID string, maximumAge time.Duration) (bool, error) {
	if s == nil || maximumAge <= 0 {
		return false, ErrSessionInvalid
	}
	session, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if session.UserID != userID || session.RevokedAt != nil || session.AuthenticationStrength != domain.MFAAuthenticationTOTP || session.MFAAuthenticatedAt == nil {
		return false, nil
	}
	return s.now().Sub(session.MFAAuthenticatedAt.UTC()) <= maximumAge, nil
}

func (s *SessionService) List(ctx context.Context, userID, currentSessionID string) ([]domain.SessionView, error) {
	items, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]domain.SessionView, 0, len(items))
	for _, session := range items {
		views = append(views, session.View(session.ID == currentSessionID))
	}
	return views, nil
}

func (s *SessionService) RevokeCurrent(ctx context.Context, userID, sessionID string) error {
	return s.revokeOwned(ctx, userID, sessionID, "logout", "identity.session.logout")
}

func (s *SessionService) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.revokeOwned(ctx, userID, sessionID, "user_revoked", "identity.session.revoke")
}

func (s *SessionService) revokeOwned(ctx context.Context, userID, sessionID, reason, code string) error {
	if userID == "" || sessionID == "" {
		return ErrSessionInvalid
	}
	now := s.now()
	var resultErr error
	err := s.execute(ctx, reliability.Command{
		Code: code, ActorID: userID, ResourceType: "identity_session", ResourceID: sessionID, OperationCode: code,
		IdempotencyKey: code + ":" + userID + ":" + sessionID,
	}, func(commandCtx context.Context) error {
		session, err := s.sessions.GetByIDForUpdate(commandCtx, sessionID)
		if errors.Is(err, repository.ErrSessionNotFound) {
			resultErr = ErrSessionInvalid
			return nil
		}
		if err != nil {
			return err
		}
		if session.UserID != userID {
			resultErr = ErrSessionNotOwned
			return nil
		}
		if session.RevokedAt == nil {
			stamp := now
			session.RevokedAt = &stamp
			session.RevokeReason = reason
			session.UpdatedAt = now
			if err := s.sessions.Update(commandCtx, session); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return resultErr
}

// RevokeAll increments auth_version before revoking sessions. Access JWTs are
// therefore invalid immediately even if a later process reads a stale session
// cache (the current implementation has no cache, but the invariant remains).
func (s *SessionService) RevokeAll(ctx context.Context, userID, reason, commandCode string) (*domain.User, error) {
	if userID == "" {
		return nil, ErrSessionInvalid
	}
	var updated *domain.User
	err := s.execute(ctx, reliability.Command{
		Code: commandCode, ActorID: userID, ResourceType: "user", ResourceID: userID, OperationCode: commandCode,
		IdempotencyKey: commandCode + ":" + userID + ":" + fmt.Sprintf("%d", s.now().UnixNano()),
	}, func(commandCtx context.Context) error {
		var revokeErr error
		updated, revokeErr = s.revokeAll(commandCtx, userID, reason)
		return revokeErr
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RevokeAllForCommand joins a caller-owned reliable command. It is intentionally
// narrow: only Identity application services use it to preserve one atomic
// state transition, command audit, and outbox event. Calling it outside an
// active command would make its transaction boundary ambiguous, so it fails.
func (s *SessionService) RevokeAllForCommand(ctx context.Context, userID, reason string) (*domain.User, error) {
	if s == nil || userID == "" || !transaction.Active(ctx) {
		return nil, ErrSessionConfiguration
	}
	return s.revokeAll(ctx, userID, reason)
}

func (s *SessionService) revokeAll(ctx context.Context, userID, reason string) (*domain.User, error) {
	user, err := s.authVersions.BumpAuthVersion(ctx, userID)
	if err != nil {
		return nil, err
	}
	if _, err := s.sessions.RevokeByUser(ctx, userID, reason, s.now()); err != nil {
		return nil, err
	}
	return user, nil
}

// RecordRefreshBodyCompatibility leaves deprecation evidence without logging
// the request body or raw refresh token. It is intentionally best-effort: a
// telemetry write must never turn a valid browser session into a failed login.
func (s *SessionService) RecordRefreshBodyCompatibility(ctx context.Context) {
	if s.reliable != nil {
		_ = s.reliable.RecordCompatibility(ctx, "identity.refresh.body", "deprecation", map[string]string{
			"replacement": "HttpOnly refresh cookie",
		})
	}
}

func (s *SessionService) now() time.Time { return s.clock().UTC() }

func (s *SessionService) observeSession(operation, result string) {
	if s == nil || s.meter == nil {
		return
	}
	_ = s.meter.AddCounter("campusos_identity_sessions_total", observability.Labels{
		"operation": operation,
		"result":    result,
	}, 1)
}

func (s *SessionService) execute(ctx context.Context, command reliability.Command, action func(context.Context) error) error {
	if s.reliable != nil {
		return s.reliable.Execute(ctx, command, action)
	}
	return action(ctx)
}

func (s *SessionService) ipHash(value string) string {
	mac := hmac.New(sha256.New, s.ipHashKey)
	_, _ = mac.Write([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(mac.Sum(nil))
}

func refreshDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func durationSeconds(value time.Duration) int {
	seconds := int(value.Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func clip(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func defaultDeviceName(value string) string {
	if value = clip(value, 128); value != "" {
		return value
	}
	return "Web browser"
}

func defaultDeviceType(value string) string {
	if value = strings.ToLower(clip(value, 20)); value != "" {
		return value
	}
	return "web"
}

func cloneSessionServiceTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
