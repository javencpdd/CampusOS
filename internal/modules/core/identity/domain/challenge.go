package domain

import "time"

// ChallengePurpose prevents a registration code or ticket from being reused
// for password reset or email binding.
type ChallengePurpose string

const (
	ChallengePurposeRegistration  ChallengePurpose = "registration"
	ChallengePurposeEmailBinding  ChallengePurpose = "email_binding"
	ChallengePurposePasswordReset ChallengePurpose = "password_reset"
)

func (p ChallengePurpose) Valid() bool {
	switch p {
	case ChallengePurposeRegistration, ChallengePurposeEmailBinding, ChallengePurposePasswordReset:
		return true
	default:
		return false
	}
}

// EmailChallenge is persistence metadata only. It intentionally has no code
// or raw ticket field: the code is HMAC-derived and the ticket is hashed.
type EmailChallenge struct {
	ID              string           `json:"id"`
	PublicID        string           `json:"public_id"`
	Purpose         ChallengePurpose `json:"purpose"`
	EmailNormalized string           `json:"email_normalized"`
	AccountID       string           `json:"account_id,omitempty"`
	KeyID           string           `json:"key_id"`
	Nonce           string           `json:"-"`
	ExpiresAt       time.Time        `json:"expires_at"`
	AttemptCount    int              `json:"attempt_count"`
	MaxAttempts     int              `json:"max_attempts"`
	VerifiedAt      *time.Time       `json:"verified_at,omitempty"`
	TicketDigest    string           `json:"-"`
	TicketExpiresAt *time.Time       `json:"ticket_expires_at,omitempty"`
	ConsumedAt      *time.Time       `json:"consumed_at,omitempty"`
	InvalidatedAt   *time.Time       `json:"invalidated_at,omitempty"`
	RequestedIPHash string           `json:"-"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type ChallengeRequest struct {
	Purpose   ChallengePurpose
	Email     string
	AccountID string
	ClientIP  string
}

type ChallengeReceipt struct {
	PublicID  string           `json:"challenge_id"`
	Purpose   ChallengePurpose `json:"purpose"`
	ExpiresAt time.Time        `json:"expires_at"`
}

// RegistrationChallengeRequest is intentionally purpose-free at the HTTP
// boundary. Public callers cannot request a code for a different identity
// operation through the registration endpoint.
type RegistrationChallengeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type RegistrationChallengeVerificationRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Code        string `json:"code" binding:"required,len=6,numeric"`
}

type ChallengeVerificationRequest struct {
	PublicID string
	Purpose  ChallengePurpose
	Code     string
}

type ChallengeTicket struct {
	PublicID  string           `json:"challenge_id"`
	Purpose   ChallengePurpose `json:"purpose"`
	Ticket    string           `json:"ticket"`
	ExpiresAt time.Time        `json:"expires_at"`
}

type ChallengeTicketConsumption struct {
	PublicID string
	Purpose  ChallengePurpose
	Ticket   string
	Email    string
}

// ChallengeDispatch is the narrow mail-delivery projection. It exists for
// the Core email module only; consumers must not log or persist Code.
type ChallengeDispatch struct {
	ChallengeID string
	PublicID    string
	Purpose     ChallengePurpose
	Email       string
	Code        string
	ExpiresAt   time.Time
}

type ChallengeRateWindow struct {
	Scope         string
	SubjectDigest string
	WindowStart   time.Time
	Limit         int
}
