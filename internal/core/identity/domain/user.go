package domain

import (
	"time"
)

// UserStatus 用户状态
type UserStatus string

const (
	UserStatusActive      UserStatus = "active"
	UserStatusSuspended   UserStatus = "suspended"
	UserStatusDeactivated UserStatus = "deactivated"
)

// User 用户领域实体
type User struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Nickname  string     `json:"nickname"`
	Email     string     `json:"email,omitempty"`
	Avatar    string     `json:"avatar,omitempty"`
	Bio       string     `json:"bio,omitempty"`
	Status    UserStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Nickname string `json:"nickname" binding:"required,min=1,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=64"`
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Nickname *string `json:"nickname,omitempty"`
	Bio      *string `json:"bio,omitempty"`
	Avatar   *string `json:"avatar,omitempty"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}
