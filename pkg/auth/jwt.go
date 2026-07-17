package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
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
}

// JWTConfig JWT 配置
type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Issuer     string
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
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
		return []byte(m.cfg.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
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

func (m *JWTManager) AccessTTL() time.Duration { return m.cfg.AccessTTL }

func (m *JWTManager) RefreshTTL() time.Duration { return m.cfg.RefreshTTL }
