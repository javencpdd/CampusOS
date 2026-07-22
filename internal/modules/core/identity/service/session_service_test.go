package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/observability"
)

func newSessionServiceForTest(t *testing.T) (*SessionService, *repository.MemoryUserRepository, *auth.JWTManager) {
	t.Helper()
	users := repository.NewMemoryUserRepository()
	user := &domain.User{
		ID: "22001", Username: "session_user", Nickname: "Session User", Email: "session@example.test",
		Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	jwtManager := auth.NewJWTManager(auth.JWTConfig{Secret: "session-test-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"})
	sessions := repository.NewMemorySessionRepository()
	service, err := NewSessionService(sessions, users, jwtManager, SessionConfig{IPHashSecret: "session-ip-hash-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	service.SetReliability(reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore()))
	return service, users, jwtManager
}

func TestSessionRefreshRotationRejectsReuseAndRevokesFamily(t *testing.T) {
	service, _, jwtManager := newSessionServiceForTest(t)
	collector := observability.NewCollector()
	service.SetMeter(collector)
	ctx := context.Background()
	user, err := service.users.GetByID(ctx, "22001")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Issue(ctx, user, SessionMetadata{DeviceName: "Test Browser", ClientIP: "203.0.113.11"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := jwtManager.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatalf("verify issued access JWT: %v", err)
	}
	if err := service.VerifyAccess(ctx, claims); err != nil {
		t.Fatalf("verify issued session: %v", err)
	}

	rotated, err := service.Refresh(ctx, issued.RefreshToken, SessionMetadata{DeviceName: "Test Browser", ClientIP: "203.0.113.11"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.Session.ID == issued.Session.ID || rotated.RefreshToken == issued.RefreshToken {
		t.Fatalf("refresh did not rotate session/token: %#v", rotated.Session)
	}
	if err := service.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("rotated access token remained valid: %v", err)
	}
	if _, err := service.Refresh(ctx, issued.RefreshToken, SessionMetadata{}); !errors.Is(err, ErrRefreshTokenReuse) {
		t.Fatalf("old refresh reuse error = %v, want ErrRefreshTokenReuse", err)
	}
	if _, err := service.Refresh(ctx, rotated.RefreshToken, SessionMetadata{}); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("family token remained usable after reuse detection: %v", err)
	}
	metrics := collector.PrometheusText()
	for _, expected := range []string{
		`campusos_identity_sessions_total{operation="issue",result="success"} 1`,
		`campusos_identity_sessions_total{operation="refresh",result="success"} 1`,
		`campusos_identity_sessions_total{operation="refresh",result="reuse"} 1`,
		`campusos_identity_sessions_total{operation="refresh",result="invalid"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("session metrics missing %q:\n%s", expected, metrics)
		}
	}
	for _, forbidden := range []string{issued.RefreshToken, rotated.RefreshToken, "session@example.test"} {
		if strings.Contains(metrics, forbidden) {
			t.Fatalf("session metrics leaked transient value")
		}
	}
}

func TestSessionLogoutAllBumpsAuthVersionAndRevokesAllSessions(t *testing.T) {
	service, users, jwtManager := newSessionServiceForTest(t)
	ctx := context.Background()
	user, _ := users.GetByID(ctx, "22001")
	first, err := service.Issue(ctx, user, SessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Issue(ctx, user, SessionMetadata{DeviceID: "other-device"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.RevokeAll(ctx, user.ID, "logout_all", "identity.session.logout_all")
	if err != nil {
		t.Fatalf("logout all: %v", err)
	}
	if updated.AuthVersion != user.AuthVersion+1 {
		t.Fatalf("auth version = %d, want %d", updated.AuthVersion, user.AuthVersion+1)
	}
	for _, token := range []string{first.AccessToken, second.AccessToken} {
		claims, err := jwtManager.VerifyAccessToken(token)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("logout-all token remained valid: %v", err)
		}
	}
}

func TestSessionVerifierRejectsLegacyAndRefreshJWTShapes(t *testing.T) {
	service, _, jwtManager := newSessionServiceForTest(t)
	legacy, err := jwtManager.GenerateAccessToken("22001", "session_user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtManager.VerifyAccessToken(legacy); err == nil {
		t.Fatalf("legacy JWT unexpectedly passed v12 access shape validation")
	}
	refreshJWT, err := jwtManager.GenerateRefreshToken("22001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtManager.VerifyAccessToken(refreshJWT); err == nil {
		t.Fatal("refresh JWT unexpectedly passed v12 access shape validation")
	}
	if err := service.VerifyAccess(context.Background(), &auth.JWTClaims{UserID: "22001", SessionID: "missing", AuthVersion: 1, TokenType: auth.AccessTokenType}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("missing session verifier error=%v", err)
	}
}
