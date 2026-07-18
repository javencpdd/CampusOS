package domain

import "time"

const ChallengePolicyID = "email_verification"

const (
	ChallengePolicyMinWindowMinutes = 1
	ChallengePolicyMaxWindowMinutes = 1440
	ChallengePolicyMinEmailRequests = 1
	ChallengePolicyMaxEmailRequests = 100
	ChallengePolicyMinIPRequests    = 1
	ChallengePolicyMaxIPRequests    = 1000
)

// ChallengePolicy is an always-on Identity security policy. Administrators can
// tune its bounded values, but cannot disable rate limiting.
type ChallengePolicy struct {
	ID                 string    `json:"id"`
	EmailWindowMinutes int       `json:"email_window_minutes"`
	EmailMaxRequests   int       `json:"email_max_requests"`
	IPWindowMinutes    int       `json:"ip_window_minutes"`
	IPMaxRequests      int       `json:"ip_max_requests"`
	Version            int64     `json:"version"`
	UpdatedBy          string    `json:"updated_by,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func DefaultChallengePolicy() ChallengePolicy {
	return ChallengePolicy{
		ID:                 ChallengePolicyID,
		EmailWindowMinutes: 10,
		EmailMaxRequests:   5,
		IPWindowMinutes:    60,
		IPMaxRequests:      10,
		Version:            1,
	}
}

type UpdateChallengePolicyRequest struct {
	EmailWindowMinutes int   `json:"email_window_minutes" binding:"required,gte=1,lte=1440"`
	EmailMaxRequests   int   `json:"email_max_requests" binding:"required,gte=1,lte=100"`
	IPWindowMinutes    int   `json:"ip_window_minutes" binding:"required,gte=1,lte=1440"`
	IPMaxRequests      int   `json:"ip_max_requests" binding:"required,gte=1,lte=1000"`
	ExpectedVersion    int64 `json:"expected_version" binding:"required,gte=1"`
}
