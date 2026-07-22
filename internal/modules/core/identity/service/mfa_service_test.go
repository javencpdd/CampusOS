package service

import (
	"context"
	"encoding/base32"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/observability"
)

type allowMFAPermissionChecker struct{ allowed bool }

func (p allowMFAPermissionChecker) CheckCode(context.Context, string, string) (bool, error) {
	return p.allowed, nil
}

func newMFAServiceForTest(t *testing.T) (*MFAService, *repository.MemoryMFARepository, *repository.MemoryUserRepository, *SessionService, *repository.MemoryRoleRepository, *time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	users := repository.NewMemoryUserRepository()
	user := &domain.User{
		ID: "73001", Username: "mfa_user", Nickname: "MFA User", Email: "mfa@example.test",
		Status: domain.UserStatusActive, AuthVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	mfaRepo := repository.NewMemoryMFARepository()
	roles := repository.NewMemoryRoleRepository()
	reliable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	sessions, err := NewSessionService(
		repository.NewMemorySessionRepository(), users,
		auth.NewJWTManager(auth.JWTConfig{Secret: "mfa-test-jwt-secret", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, Issuer: "test"}),
		SessionConfig{IPHashSecret: "mfa-test-session-ip-hash-secret", Clock: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetReliability(reliable)
	service, err := NewMFAService(mfaRepo, users, allowMFAPermissionChecker{allowed: true}, roles, MFAConfig{
		ActiveKeyID: "current", EncryptionKeys: map[string]string{
			"current":  "mfa-test-current-encryption-key-material",
			"previous": "mfa-test-previous-encryption-key-material",
		},
		Issuer: "CampusOS Test", LocalRecoveryAvailable: true, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service.SetReliability(reliable)
	service.SetSessionRevoker(sessions)
	return service, mfaRepo, users, sessions, roles, &now
}

func enrollMFAForTest(t *testing.T, service *MFAService, now *time.Time) (*domain.MFAEnrollmentConfirmation, []byte) {
	t.Helper()
	enrollment, err := service.StartEnrollment(context.Background(), "73001")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.ManualKey)
	if err != nil {
		t.Fatalf("decode manual key: %v", err)
	}
	confirmation, err := service.ConfirmEnrollment(context.Background(), "73001", totpCode(secret, now.Unix()/mfaTOTPStepSeconds))
	if err != nil {
		t.Fatalf("confirm enrollment: %v", err)
	}
	if len(confirmation.RecoveryCodes) != mfaRecoveryCodeCount {
		t.Fatalf("recovery code count=%d, want %d", len(confirmation.RecoveryCodes), mfaRecoveryCodeCount)
	}
	return confirmation, secret
}

func TestMFAEnrollmentTicketReplayRecoveryAndLocalReset(t *testing.T) {
	ctx := context.Background()
	service, _, users, sessions, roles, now := newMFAServiceForTest(t)
	confirmation, secret := enrollMFAForTest(t, service, now)

	status, err := service.Status(ctx, "73001")
	if err != nil || !status.Enabled || status.RecoveryCodesRemaining != mfaRecoveryCodeCount || !status.MFAAvailable {
		t.Fatalf("unexpected MFA status: %#v err=%v", status, err)
	}
	*now = now.Add(30 * time.Second)
	requirement, err := service.BeginLogin(ctx, "73001", domain.MFAAudienceAdmin)
	if err != nil || requirement == nil || !requirement.Required || requirement.Ticket == "" {
		t.Fatalf("begin MFA login: requirement=%#v err=%v", requirement, err)
	}

	var successes int
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, completeErr := service.CompleteLogin(ctx, requirement.Ticket, totpCode(secret, now.Unix()/mfaTOTPStepSeconds))
			if completeErr == nil {
				mutex.Lock()
				successes++
				mutex.Unlock()
			}
		}()
	}
	wait.Wait()
	if successes != 1 {
		t.Fatalf("one MFA ticket must be consumed once, successes=%d", successes)
	}
	if err := service.VerifyCurrentFactor(ctx, "73001", totpCode(secret, now.Unix()/mfaTOTPStepSeconds)); !errors.Is(err, ErrMFAReplay) {
		t.Fatalf("same TOTP step error=%v, want replay", err)
	}

	rotated, err := service.RotateRecoveryCodes(ctx, "73001", "", confirmation.RecoveryCodes[0])
	if err != nil || len(rotated.RecoveryCodes) != mfaRecoveryCodeCount {
		t.Fatalf("rotate recovery codes: result=%#v err=%v", rotated, err)
	}
	if _, err := service.RotateRecoveryCodes(ctx, "73001", "", confirmation.RecoveryCodes[0]); !errors.Is(err, ErrMFARecoveryCodeInvalid) {
		t.Fatalf("used recovery code error=%v", err)
	}

	user, err := users.GetByID(ctx, "73001")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.IssueWithAuthentication(ctx, user, SessionMetadata{ClientIP: "203.0.113.73"}, domain.MFAAuthenticationTOTP, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisableFromLocalRecovery(ctx, "73001", "lost authenticator during isolated recovery drill"); err != nil {
		t.Fatalf("local MFA recovery: %v", err)
	}
	claims, err := sessions.jwt.VerifyAccessToken(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.VerifyAccess(ctx, claims); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("local recovery did not revoke active session: %v", err)
	}
	status, err = service.Status(ctx, "73001")
	if err != nil || status.Enabled || status.RecoveryCodesRemaining != 0 {
		t.Fatalf("local recovery did not clear MFA state: %#v err=%v", status, err)
	}
	audits, err := roles.ListAuthorizationAudits(ctx, 20)
	if err != nil || len(audits) == 0 || audits[0].PermissionCode != "identity.mfa.local_recovery" {
		t.Fatalf("local recovery audit missing: %#v err=%v", audits, err)
	}
}

func TestMFASecretProtectorSupportsReadRotationAndFailsClosed(t *testing.T) {
	previous, err := NewMFASecretProtector("previous", map[string]string{
		"previous": "mfa-test-previous-encryption-key-material",
	})
	if err != nil {
		t.Fatal(err)
	}
	keyID, nonce, ciphertext, err := previous.Seal([]byte("totp-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewMFASecretProtector("current", map[string]string{
		"previous": "mfa-test-previous-encryption-key-material",
		"current":  "mfa-test-current-encryption-key-material",
	})
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := rotated.Open(keyID, nonce, ciphertext)
	if err != nil || string(plaintext) != "totp-test-secret" {
		t.Fatalf("rotated reader failed: plaintext=%q err=%v", plaintext, err)
	}
	missing, err := NewMFASecretProtector("current", map[string]string{
		"current": "mfa-test-current-encryption-key-material",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Open(keyID, nonce, ciphertext); !errors.Is(err, ErrMFASecretUnavailable) {
		t.Fatalf("missing historical key error=%v", err)
	}
}

func TestMFAMetricsUseRegisteredBoundedLabelsAndDoNotExposeSecrets(t *testing.T) {
	service, _, _, _, _, now := newMFAServiceForTest(t)
	collector := observability.NewCollector()
	service.SetMeter(collector)
	_, secret := enrollMFAForTest(t, service, now)
	metrics := collector.PrometheusText()
	for _, expected := range []string{
		`campusos_identity_mfa_total{operation="enrollment",result="started"} 1`,
		`campusos_identity_mfa_total{operation="enrollment",result="confirmed"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("MFA metrics missing %q:\n%s", expected, metrics)
		}
	}
	if strings.Contains(metrics, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)) {
		t.Fatalf("MFA metrics leaked enrollment secret:\n%s", metrics)
	}
}

func TestMFARequiredPolicyRejectsUnsafeCoverageAndExpiredGrace(t *testing.T) {
	ctx := context.Background()
	service, repo, _, _, _, now := newMFAServiceForTest(t)
	repo.SetAdminCoverage(domain.MFAAdminCoverage{ActiveAdministrators: 2, MFAEnrolledAdministrators: 1})
	policy, err := repo.GetPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateAdminPolicy(ctx, "73001", MFAAdminPolicyUpdate{Mode: domain.MFAPolicyRequired, ExpectedVersion: policy.Version}); !errors.Is(err, ErrMFAPolicySafety) {
		t.Fatalf("unsafe required policy error=%v", err)
	}

	expired := now.Add(-time.Minute)
	updated, err := repo.UpdatePolicy(ctx, domain.MFAPolicy{Mode: domain.MFAPolicyEnrollmentGrace, GraceEndsAt: &expired, UpdatedAt: *now}, policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Mode != domain.MFAPolicyEnrollmentGrace {
		t.Fatalf("unexpected policy: %#v", updated)
	}
	if _, err := service.BeginLogin(ctx, "73001", domain.MFAAudienceAdmin); !errors.Is(err, ErrMFAEnrollmentRequired) {
		t.Fatalf("expired enrollment grace error=%v", err)
	}
	web, err := service.BeginLogin(ctx, "73001", domain.MFAAudienceWeb)
	if err != nil || web.Required {
		t.Fatalf("administrator policy must not silently force the ordinary web audience: requirement=%#v err=%v", web, err)
	}
}

func TestMFARequiredPolicyGuardsAdminSessionRegardlessOfLoginSurface(t *testing.T) {
	ctx := context.Background()
	service, repo, users, sessions, _, now := newMFAServiceForTest(t)
	service.SetSessionStrengthReader(sessions)
	_, _ = enrollMFAForTest(t, service, now)

	policy, err := repo.GetPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdatePolicy(ctx, domain.MFAPolicy{
		ID: "admin", Mode: domain.MFAPolicyRequired, UpdatedAt: *now,
	}, policy.Version); err != nil {
		t.Fatalf("set required policy: %v", err)
	}
	user, err := users.GetByID(ctx, "73001")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(ctx, user, SessionMetadata{ClientIP: "203.0.113.73"})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := service.CheckAdminMFA(ctx, user.ID, issued.Session.ID)
	if err != nil || allowed {
		t.Fatalf("password-only Session bypassed required MFA: allowed=%v err=%v", allowed, err)
	}
	if _, err := sessions.MarkMFA(ctx, user.ID, issued.Session.ID); err != nil {
		t.Fatalf("mark MFA session: %v", err)
	}
	allowed, err = service.CheckAdminMFA(ctx, user.ID, issued.Session.ID)
	if err != nil || !allowed {
		t.Fatalf("fresh MFA Session was rejected: allowed=%v err=%v", allowed, err)
	}
}
