package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
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
		cfg.AccessTTL = 2 * time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "campusos"
	}
	return &JWTManager{cfg: cfg}
}

// GenerateAccessToken 生成 Access Token
func (m *JWTManager) GenerateAccessToken(userID, username string) (string, error) {
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
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

// GenerateRefreshToken 生成 Refresh Token
func (m *JWTManager) GenerateRefreshToken(userID string) (string, error) {
	claims := JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.cfg.Issuer,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(m.cfg.RefreshTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.cfg.Secret))
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
