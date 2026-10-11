package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/hkdf"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	AuthVersion int64  `json:"auth_version,omitempty"`
	TokenType   string `json:"typ,omitempty"`
	jwt.RegisteredClaims
}

const AccessTokenType = "access"

// AccessTokenContext binds a short-lived access JWT to a persisted login
// session. The old two-argument GenerateAccessToken call remains available to
// isolated legacy tests, but production middleware only accepts this context.
type AccessTokenContext struct {
	SessionID   string
	AuthVersion int64
	// Nickname is an optional display-name snapshot for lightweight callers.
	// CampusOS production middleware refreshes it from the verified identity
	// record before exposing it to a request.
	Nickname string
}

// JWTConfig JWT 配置
type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Issuer     string
	// Audience pins the intended consumer. Empty keeps pre-v12 behavior; the
	// v1.2 identity domains set one audience per domain and verification then
	// rejects tokens aimed at the other domain.
	Audience string
	// KeyID identifies the signing key in the token header. Empty keeps
	// pre-v12 behavior; the identity domains use one key id per domain so a
	// token minted by the other domain never verifies.
	KeyID string
}

// JWTManager JWT 管理器
type JWTManager struct {
	cfg JWTConfig
}

// NewJWTManager 创建 JWT 管理器
func NewJWTManager(cfg JWTConfig) *JWTManager {
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "campusos"
	}
	return &JWTManager{cfg: cfg}
}

// GenerateAccessToken creates an access JWT. Callers serving HTTP requests
// must supply exactly one AccessTokenContext; the no-context form only exists
// to keep pre-v12 in-process unit tests source compatible.
func (m *JWTManager) GenerateAccessToken(userID, username string, contexts ...AccessTokenContext) (string, error) {
	var context AccessTokenContext
	if len(contexts) > 0 {
		context = contexts[0]
	}
	claims := JWTClaims{
		UserID:      userID,
		Username:    username,
		Nickname:    context.Nickname,
		SessionID:   context.SessionID,
		AuthVersion: context.AuthVersion,
		TokenType:   AccessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.cfg.Issuer,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.cfg.AccessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	if m.cfg.Audience != "" {
		claims.Audience = jwt.ClaimStrings{m.cfg.Audience}
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if m.cfg.KeyID != "" {
		token.Header["kid"] = m.cfg.KeyID
	}
	return token.SignedString([]byte(m.cfg.Secret))
}

// GenerateRefreshToken is retained for source compatibility only. v12 refresh
// credentials are opaque random values and have no JWT claims or signature.
// Identity SessionService remains responsible for binding the digest to a user.
func (m *JWTManager) GenerateRefreshToken(_ string) (string, error) {
	return NewOpaqueRefreshToken()
}

// NewOpaqueRefreshToken returns 256 bits of random URL-safe data. Refresh
// tokens are deliberately not JWTs and must only be persisted as a digest by
// the Identity Session service.
func NewOpaqueRefreshToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read refresh token randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// VerifyToken 验证 Token
func (m *JWTManager) VerifyToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if m.cfg.KeyID != "" {
			if kid, ok := t.Header["kid"].(string); !ok || kid != m.cfg.KeyID {
				return nil, errors.New("token key id does not match this domain")
			}
		}
		return []byte(m.cfg.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if m.cfg.Issuer != "" && claims.Issuer != m.cfg.Issuer {
		return nil, errors.New("token issuer does not match this domain")
	}
	if m.cfg.Audience != "" {
		found := false
		for _, value := range claims.Audience {
			if value == m.cfg.Audience {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("token audience does not match this domain")
		}
	}
	return claims, nil
}

// VerifyAccessToken enforces the v12 token-shape boundary. In particular, a
// legacy v11 JWT, refresh JWT, or token minted without a persisted session
// cannot be treated as an authenticated browser/API credential.
func (m *JWTManager) VerifyAccessToken(tokenString string) (*JWTClaims, error) {
	claims, err := m.VerifyToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != AccessTokenType || claims.SessionID == "" || claims.AuthVersion < 1 {
		return nil, errors.New("token is not a v12 access session token")
	}
	return claims, nil
}

// Secret exposes the signing secret for HKDF domain derivation inside the
// process only. It must never be logged, returned by an API, or copied into
// configuration snapshots.
func (m *JWTManager) Secret() string { return m.cfg.Secret }

func (m *JWTManager) AccessTTL() time.Duration { return m.cfg.AccessTTL }

func (m *JWTManager) RefreshTTL() time.Duration { return m.cfg.RefreshTTL }

// DomainJWTManagers pairs one token manager per identity domain (v1.2
// independent User/Admin identity domain contract). The caller must supply
// distinct issuer, audience and secret/key ids; verification then rejects any
// token minted for the other domain at every boundary.
type DomainJWTManagers struct {
	User  *JWTManager
	Admin *JWTManager
}

func NewDomainJWTManagers(user JWTConfig, admin JWTConfig) *DomainJWTManagers {
	if user.Secret == "" || admin.Secret == "" || user.Secret == admin.Secret {
		panic("identity domains require distinct signing secrets per domain")
	}
	if user.Issuer == admin.Issuer || user.Audience == admin.Audience {
		panic("identity domains require distinct issuers and audiences")
	}
	return &DomainJWTManagers{User: NewJWTManager(user), Admin: NewJWTManager(admin)}
}

// DeriveDomainSecret derives a per-domain signing secret from a base secret
// with HKDF-SHA-256 domain separation. Deployments that need independent key
// custody set the domain secret explicitly instead of deriving it.
func DeriveDomainSecret(base, domain string) string {
	key := hkdf.Extract(sha256.New, []byte(base), []byte(domain))
	return base64.RawURLEncoding.EncodeToString(key)
}
