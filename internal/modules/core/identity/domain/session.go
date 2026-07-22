package domain

import "time"

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Session is the server-side authority for an interactive login. Refresh
// material is never exposed by this type's JSON projection; the repository
// persists only its SHA-256 digest.
type Session struct {
	ID                     string                    `json:"id"`
	UserID                 string                    `json:"user_id"`
	RefreshTokenDigest     string                    `json:"-"`
	TokenFamilyID          string                    `json:"-"`
	RotatedFromID          string                    `json:"-"`
	RotatedToID            string                    `json:"-"`
	DeviceID               string                    `json:"device_id,omitempty"`
	DeviceName             string                    `json:"device_name,omitempty"`
	DeviceType             string                    `json:"device_type,omitempty"`
	IPHash                 string                    `json:"-"`
	UserAgent              string                    `json:"user_agent,omitempty"`
	AuthenticationStrength MFAAuthenticationStrength `json:"authentication_strength,omitempty"`
	MFAAuthenticatedAt     *time.Time                `json:"mfa_authenticated_at,omitempty"`
	LastActiveAt           time.Time                 `json:"last_active_at"`
	ExpiresAt              time.Time                 `json:"expires_at"`
	RevokedAt              *time.Time                `json:"revoked_at,omitempty"`
	RevokeReason           string                    `json:"revoke_reason,omitempty"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
}

// SessionView is safe for the account settings UI. It intentionally omits
// refresh digests, token families, IP hashes and browser user agent strings.
type SessionView struct {
	ID                     string                    `json:"id"`
	Current                bool                      `json:"current"`
	DeviceName             string                    `json:"device_name,omitempty"`
	DeviceType             string                    `json:"device_type,omitempty"`
	AuthenticationStrength MFAAuthenticationStrength `json:"authentication_strength,omitempty"`
	MFAAuthenticatedAt     *time.Time                `json:"mfa_authenticated_at,omitempty"`
	LastActiveAt           time.Time                 `json:"last_active_at"`
	ExpiresAt              time.Time                 `json:"expires_at"`
	RevokedAt              *time.Time                `json:"revoked_at,omitempty"`
	CreatedAt              time.Time                 `json:"created_at"`
}

func (s *Session) View(current bool) SessionView {
	if s == nil {
		return SessionView{}
	}
	return SessionView{
		ID: s.ID, Current: current, DeviceName: s.DeviceName, DeviceType: s.DeviceType,
		AuthenticationStrength: s.AuthenticationStrength, MFAAuthenticatedAt: cloneTime(s.MFAAuthenticatedAt),
		LastActiveAt: s.LastActiveAt, ExpiresAt: s.ExpiresAt, RevokedAt: cloneTime(s.RevokedAt), CreatedAt: s.CreatedAt,
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
