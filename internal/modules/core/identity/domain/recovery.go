package domain

import (
	"strings"
	"time"
)

type RecoveryCaseStatus string

const (
	RecoveryCasePending   RecoveryCaseStatus = "pending"
	RecoveryCaseCompleted RecoveryCaseStatus = "completed"
	RecoveryCaseCancelled RecoveryCaseStatus = "cancelled"
	RecoveryCaseExpired   RecoveryCaseStatus = "expired"
)

// RecoveryCase is the internal record for an administrator-assisted recovery.
// The target address and proof reference remain inside Identity storage; HTTP
// consumers receive RecoveryCaseView, never the proof reference.
type RecoveryCase struct {
	ID                    string             `json:"id"`
	PublicID              string             `json:"public_id"`
	UserID                string             `json:"user_id"`
	AccountID             string             `json:"account_id"`
	TargetEmailNormalized string             `json:"-"`
	ChallengeID           string             `json:"-"`
	CreatedBy             string             `json:"created_by,omitempty"`
	ProofReference        string             `json:"-"`
	Status                RecoveryCaseStatus `json:"status"`
	ExpiresAt             time.Time          `json:"expires_at"`
	CompletedAt           *time.Time         `json:"completed_at,omitempty"`
	CancelledAt           *time.Time         `json:"cancelled_at,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

type RecoveryCaseView struct {
	ID                string             `json:"id"`
	UserID            string             `json:"user_id"`
	TargetEmailMasked string             `json:"target_email_masked"`
	Status            RecoveryCaseStatus `json:"status"`
	ExpiresAt         time.Time          `json:"expires_at"`
	CompletedAt       *time.Time         `json:"completed_at,omitempty"`
	CancelledAt       *time.Time         `json:"cancelled_at,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
}

func (c *RecoveryCase) View() RecoveryCaseView {
	if c == nil {
		return RecoveryCaseView{}
	}
	return RecoveryCaseView{
		ID: c.PublicID, UserID: c.UserID, TargetEmailMasked: MaskEmail(c.TargetEmailNormalized), Status: c.Status,
		ExpiresAt: c.ExpiresAt, CompletedAt: cloneRecoveryTime(c.CompletedAt), CancelledAt: cloneRecoveryTime(c.CancelledAt), CreatedAt: c.CreatedAt,
	}
}

func MaskEmail(email string) string {
	email = NormalizeEmail(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "hidden"
	}
	local := []rune(email[:at])
	if len(local) == 1 {
		return string(local) + "***" + email[at:]
	}
	return string(local[:1]) + "***" + string(local[len(local)-1:]) + email[at:]
}

type PasswordResetChallengeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type PasswordResetChallengeResponse struct {
	Accepted    bool       `json:"accepted"`
	ChallengeID string     `json:"challenge_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type PasswordResetVerificationRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Code        string `json:"code" binding:"required,len=6,numeric"`
}

type PasswordResetCompletionRequest struct {
	Email       string `json:"email" binding:"required,email"`
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Ticket      string `json:"ticket" binding:"required,min=16,max=256"`
	Password    string `json:"password" binding:"required,min=6,max=64"`
}

type EmailBindingChallengeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type EmailBindingVerificationRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Code        string `json:"code" binding:"required,len=6,numeric"`
}

type EmailBindingCompletionRequest struct {
	Email       string `json:"email" binding:"required,email"`
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Ticket      string `json:"ticket" binding:"required,min=16,max=256"`
}

type AdminRecoveryCaseCreateRequest struct {
	UserID         string `json:"user_id" binding:"required,numeric"`
	Email          string `json:"email" binding:"required,email"`
	ProofReference string `json:"proof_reference" binding:"required,min=3,max=160"`
}

type AdminRecoveryCaseCancelRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=160"`
}

type RecoveryCaseCompletionRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required,min=16,max=256"`
	Ticket      string `json:"ticket" binding:"required,min=16,max=256"`
	Password    string `json:"password" binding:"required,min=6,max=64"`
}

func cloneRecoveryTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
