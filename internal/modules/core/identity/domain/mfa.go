package domain

import "time"

// MFAAudience binds a short-lived login ticket to the surface that completed
// the password step. A web ticket can never be exchanged for an Admin session.
type MFAAudience string

const (
	MFAAudienceWeb   MFAAudience = "web"
	MFAAudienceAdmin MFAAudience = "admin"
)

type MFATicketPurpose string

const (
	MFATicketPurposeLogin  MFATicketPurpose = "login"
	MFATicketPurposeStepUp MFATicketPurpose = "step_up"
)

type MFAAuthenticationStrength string

const (
	MFAAuthenticationPassword MFAAuthenticationStrength = "password"
	MFAAuthenticationTOTP     MFAAuthenticationStrength = "mfa"
)

type MFAPolicyMode string

const (
	MFAPolicyOff             MFAPolicyMode = "off"
	MFAPolicyEnrollmentGrace MFAPolicyMode = "enrollment_grace"
	MFAPolicyRequired        MFAPolicyMode = "required"
)

// MFAStatus is safe to return to its owner. It deliberately contains no
// secret, recovery code, ticket digest, device information, or key ID.
type MFAStatus struct {
	Enabled                bool          `json:"enabled"`
	PendingEnrollment      bool          `json:"pending_enrollment"`
	RecoveryCodesRemaining int           `json:"recovery_codes_remaining"`
	MFAAvailable           bool          `json:"mfa_available"`
	PolicyMode             MFAPolicyMode `json:"policy_mode"`
	GraceEndsAt            *time.Time    `json:"grace_ends_at,omitempty"`
	StepUpRequiredAfter    int           `json:"step_up_required_after_seconds"`
}

// MFAEnrollment is returned exactly once after password reauthentication. The
// manual key and otpauth URI are never persisted in browser storage by the
// CampusOS clients and must not be logged by callers.
type MFAEnrollment struct {
	ManualKey string    `json:"manual_key"`
	OTAuthURI string    `json:"otpauth_uri"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MFAEnrollmentConfirmation returns recovery codes only once, after a valid
// TOTP confirmation activates the pending method.
type MFAEnrollmentConfirmation struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

type MFAPolicy struct {
	ID          string        `json:"id"`
	Mode        MFAPolicyMode `json:"mode"`
	GraceEndsAt *time.Time    `json:"grace_ends_at,omitempty"`
	Version     int64         `json:"version"`
	UpdatedBy   string        `json:"updated_by,omitempty"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type MFAAdminCoverage struct {
	ActiveAdministrators      int  `json:"active_administrators"`
	MFAEnrolledAdministrators int  `json:"mfa_enrolled_administrators"`
	LocalRecoveryAvailable    bool `json:"local_recovery_available"`
}

// MFAAdminPolicyStatus is an Admin-safe policy projection. It intentionally
// returns aggregate counts rather than any user's factor details.
type MFAAdminPolicyStatus struct {
	Policy    MFAPolicy        `json:"policy"`
	Coverage  MFAAdminCoverage `json:"coverage"`
	Available bool             `json:"available"`
}
