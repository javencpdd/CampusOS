package handler

import (
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/apperror"
)

var challengeErrorTranslator = apperror.MustTranslator("core.identity.challenge", apperror.IdentityChallengeUnavailable,
	apperror.Rule{Target: service.ErrChallengeInvalid, Descriptor: apperror.IdentityChallengeInvalid},
	apperror.Rule{Target: service.ErrChallengeTicket, Descriptor: apperror.IdentityChallengeInvalid},
	apperror.Rule{Target: service.ErrChallengeRateLimited, Descriptor: apperror.IdentityChallengeRateLimited},
)

var registrationErrorTranslator = apperror.MustTranslator("core.identity.registration", apperror.InternalError,
	apperror.Rule{Target: service.ErrRegistrationVerificationRequired, Descriptor: apperror.IdentityRegistrationVerificationRequired},
	apperror.Rule{Target: service.ErrRegistrationTicketInvalid, Descriptor: apperror.IdentityChallengeInvalid},
	apperror.Rule{Target: service.ErrRegistrationConflict, Descriptor: apperror.IdentityRegistrationConflict},
)

var sessionErrorTranslator = apperror.MustTranslator("core.identity.session", apperror.IdentitySessionUnavailable,
	apperror.Rule{Target: service.ErrSessionInvalid, Descriptor: apperror.IdentitySessionInactive},
	apperror.Rule{Target: service.ErrRefreshTokenInvalid, Descriptor: apperror.IdentityRefreshInvalid},
	apperror.Rule{Target: service.ErrRefreshTokenReuse, Descriptor: apperror.IdentityRefreshInvalid},
	apperror.Rule{Target: service.ErrSessionNotOwned, Descriptor: apperror.IdentitySessionNotFound},
	apperror.Rule{Target: service.ErrSessionConfiguration, Descriptor: apperror.IdentitySessionUnavailable},
)

var adminAdmissionErrorTranslator = apperror.MustTranslator("core.identity.admin_admission", apperror.IdentityAdminAdmissionUnavailable,
	apperror.Rule{Target: service.ErrAdminAdmissionInvalid, Descriptor: apperror.IdentityAdminAdmissionInvalid},
	apperror.Rule{Target: service.ErrAdminAdmissionPermission, Descriptor: apperror.PermissionDenied},
	apperror.Rule{Target: service.ErrAdminAdmissionUnavailable, Descriptor: apperror.IdentityAdminAdmissionUnavailable},
	apperror.Rule{Target: repository.ErrAdminAccountNotFound, Descriptor: apperror.IdentityAdminAdmissionNotFound},
	apperror.Rule{Target: repository.ErrAdminAccountVersionConflict, Descriptor: apperror.IdentityAdminAdmissionVersionConflict},
	apperror.Rule{Target: repository.ErrAdminAccountInvalidTransition, Descriptor: apperror.IdentityAdminAdmissionTransitionConflict},
	apperror.Rule{Target: repository.ErrLastActiveAdministrator, Descriptor: apperror.IdentityAdminAdmissionLastActive},
)

var mfaErrorTranslator = apperror.MustTranslator("core.identity.mfa", apperror.IdentityMFAUnavailable,
	apperror.Rule{Target: service.ErrMFAInvalid, Descriptor: apperror.IdentityMFAInvalid},
	apperror.Rule{Target: service.ErrMFATicketInvalid, Descriptor: apperror.IdentityMFATicketInvalid},
	apperror.Rule{Target: service.ErrMFACodeInvalid, Descriptor: apperror.IdentityMFAFactorInvalid},
	apperror.Rule{Target: service.ErrMFARecoveryCodeInvalid, Descriptor: apperror.IdentityMFAFactorInvalid},
	apperror.Rule{Target: service.ErrMFAReplay, Descriptor: apperror.IdentityMFAReplay},
	apperror.Rule{Target: service.ErrMFAEnrollmentRequired, Descriptor: apperror.IdentityMFAEnrollmentRequired},
	apperror.Rule{Target: service.ErrMFANotEnabled, Descriptor: apperror.IdentityMFANotEnabled},
	apperror.Rule{Target: service.ErrMFAPolicyInvalid, Descriptor: apperror.IdentityMFAPolicyInvalid},
	apperror.Rule{Target: service.ErrMFAPolicySafety, Descriptor: apperror.IdentityMFAPolicySafety},
	apperror.Rule{Target: service.ErrMFAStepUpRequired, Descriptor: apperror.IdentityMFAStepUpRequired},
	apperror.Rule{Target: service.ErrMFAPermission, Descriptor: apperror.PermissionDenied},
	apperror.Rule{Target: repository.ErrMFAPolicyConflict, Descriptor: apperror.IdentityAdminAdmissionVersionConflict},
)

func unavailableError(descriptor apperror.Descriptor) *apperror.AppError {
	return apperror.New(descriptor, nil).WithHTTPStatus(503)
}

func unavailableIfInternal(value *apperror.AppError) *apperror.AppError {
	if value != nil && value.Descriptor() == apperror.InternalError {
		value.WithHTTPStatus(503)
	}
	return value
}
