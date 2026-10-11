package auth

import (
	"testing"
	"time"
)

func domainManagerConfig(kind, secret string) JWTConfig {
	return JWTConfig{
		Secret:    secret,
		Issuer:    "campusos-" + kind,
		Audience:  kind + "-api",
		KeyID:     kind + "-key",
		AccessTTL: time.Hour,
	}
}

func domainAccessToken(t *testing.T, manager *JWTManager, userID string) string {
	t.Helper()
	token, err := manager.GenerateAccessToken(userID, "tester", AccessTokenContext{
		SessionID: "session-1", AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestDomainJWTManagersNeverVerifyAcrossDomains(t *testing.T) {
	managers := NewDomainJWTManagers(
		domainManagerConfig("user", "user-secret-material"),
		domainManagerConfig("admin", "admin-secret-material"),
	)
	userToken := domainAccessToken(t, managers.User, "42")
	adminToken := domainAccessToken(t, managers.Admin, "42")

	// Same numeric subject in both domains; verification succeeds only in the
	// issuing domain and rejects in both directions.
	if _, err := managers.User.VerifyAccessToken(userToken); err != nil {
		t.Fatal("user token rejected by user domain:", err)
	}
	if _, err := managers.Admin.VerifyAccessToken(adminToken); err != nil {
		t.Fatal("admin token rejected by admin domain:", err)
	}
	if _, err := managers.User.VerifyAccessToken(adminToken); err == nil {
		t.Fatal("admin token accepted by user domain")
	}
	if _, err := managers.Admin.VerifyAccessToken(userToken); err == nil {
		t.Fatal("user token accepted by admin domain")
	}
}

func TestDomainJWTManagerRejectsKidAudienceMismatch(t *testing.T) {
	manager := NewJWTManager(domainManagerConfig("admin", "admin-secret-material"))
	token := domainAccessToken(t, manager, "42")

	// A sibling manager with the same secret but another audience or key id
	// must reject the token.
	for _, cfg := range []JWTConfig{
		{Secret: "admin-secret-material", Issuer: "campusos-admin", Audience: "user-api", KeyID: "admin-key"},
		{Secret: "admin-secret-material", Issuer: "campusos-admin", Audience: "admin-api", KeyID: "other-key"},
		{Secret: "admin-secret-material", Issuer: "campusos-user", Audience: "admin-api", KeyID: "admin-key"},
	} {
		sibling := NewJWTManager(cfg)
		if _, err := sibling.VerifyAccessToken(token); err == nil {
			t.Fatalf("token accepted with mismatched config: %+v", cfg)
		}
	}
}

func TestDomainJWTManagersRefuseSharedIdentity(t *testing.T) {
	base := domainManagerConfig("user", "user-secret-material")
	panics := func(user, admin JWTConfig) (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		NewDomainJWTManagers(user, admin)
		return false
	}
	if !panics(base, domainManagerConfig("admin", "user-secret-material")) {
		t.Fatal("shared secret accepted")
	}
	if !panics(base, JWTConfig{Secret: "admin-secret", Issuer: "campusos-user", Audience: "admin-api"}) {
		t.Fatal("shared issuer accepted")
	}
	if !panics(base, JWTConfig{Secret: "admin-secret", Issuer: "campusos-admin", Audience: "user-api"}) {
		t.Fatal("shared audience accepted")
	}
}

func TestJWTManagerKeepsLegacyNoDomainConfig(t *testing.T) {
	legacy := NewJWTManager(JWTConfig{Secret: "legacy-secret"})
	token := domainAccessToken(t, legacy, "42")
	if _, err := legacy.VerifyAccessToken(token); err != nil {
		t.Fatal("legacy configuration rejected its own token:", err)
	}
}
