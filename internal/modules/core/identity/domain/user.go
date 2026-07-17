package domain

import (
	"strings"
	"time"
)

// UserStatus 用户状态
type UserStatus string

const (
	UserStatusActive      UserStatus = "active"
	UserStatusSuspended   UserStatus = "suspended"
	UserStatusDeactivated UserStatus = "deactivated"
)

type VerificationState string

const (
	VerificationStateUnverified     VerificationState = "unverified"
	VerificationStateLegacyAccepted VerificationState = "legacy_accepted"
	VerificationStateVerified       VerificationState = "verified"
	VerificationStateSystemManaged  VerificationState = "system_managed"

	LegacySharedPlaceholderEmail = "1904650862@qq.com"
)

// NormalizeEmail defines the v12 identifier rule. Provider-specific rewrites
// (for example dots in Gmail local parts) are deliberately out of scope: they
// can merge distinct valid accounts.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func IsReservedEmail(email string) bool {
	return NormalizeEmail(email) == LegacySharedPlaceholderEmail
}

// User 用户领域实体
type User struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Nickname           string     `json:"nickname"`
	Email              string     `json:"email,omitempty"`
	Avatar             string     `json:"avatar,omitempty"`
	Bio                string     `json:"bio,omitempty"`
	Status             UserStatus `json:"status"`
	AuthVersion        int64      `json:"auth_version,omitempty"`
	MustChangePassword bool       `json:"must_change_password,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// EmailAccount is the credential-free identity projection used by Core modules
// that need verification state. Password credentials and token material never
// leave the Identity repository boundary.
type EmailAccount struct {
	ID                   string            `json:"id"`
	UserID               string            `json:"user_id"`
	IdentifierNormalized string            `json:"identifier_normalized"`
	VerificationState    VerificationState `json:"verification_state"`
	VerifiedAt           *time.Time        `json:"verified_at,omitempty"`
	VerificationSource   string            `json:"verification_source,omitempty"`
	CredentialVersion    int64             `json:"credential_version"`
	PasswordChangedAt    *time.Time        `json:"password_changed_at,omitempty"`
}

// PublicUser is the public profile projection. Account email and other future
// identity fields must not be exposed by public directory endpoints.
type PublicUser struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Nickname  string     `json:"nickname"`
	Avatar    string     `json:"avatar,omitempty"`
	Bio       string     `json:"bio,omitempty"`
	Status    UserStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (u *User) Public() PublicUser {
	return PublicUser{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar,
		Bio: u.Bio, Status: u.Status, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Nickname string `json:"nickname" binding:"required,min=1,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=64"`
}

// RegistrationRequest is the public registration contract. ChallengeID and
// Ticket intentionally use handler-level validation so a legacy one-step
// request can receive the stable verification-required error instead of a
// generic decoder error.
type RegistrationRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=32"`
	Nickname    string `json:"nickname" binding:"required,min=1,max=64"`
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=6,max=64"`
	ChallengeID string `json:"challenge_id"`
	Ticket      string `json:"ticket"`
}

func (r RegistrationRequest) UserRequest() CreateUserRequest {
	return CreateUserRequest{
		Username: r.Username,
		Nickname: r.Nickname,
		Email:    r.Email,
		Password: r.Password,
	}
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Nickname *string `json:"nickname,omitempty" binding:"omitempty,max=64"`
	Bio      *string `json:"bio,omitempty" binding:"omitempty,max=500"`
	Avatar   *string `json:"avatar,omitempty" binding:"omitempty,max=512"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// RoleInfo 角色信息（用于登录响应）
type RoleInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	User         *User      `json:"user"`
	Roles        []RoleInfo `json:"roles"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	TokenType    string     `json:"token_type"`
	ExpiresIn    int        `json:"expires_in"`
}
