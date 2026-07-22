package secondhand

import "github.com/campusos/CampusOS/pkg/apperror"

var errorTranslator = apperror.MustTranslator("feature.secondhand", apperror.InternalError,
	apperror.Rule{Target: ErrFeatureDisabled, Descriptor: apperror.SecondhandFeatureDisabled},
	apperror.Rule{Target: ErrNotFound, Descriptor: apperror.SecondhandNotFound},
	apperror.Rule{Target: ErrForbidden, Descriptor: apperror.SecondhandForbidden},
	apperror.Rule{Target: ErrInvalidInput, Descriptor: apperror.SecondhandInvalidInput},
	apperror.Rule{Target: ErrInvalidTransition, Descriptor: apperror.SecondhandInvalidTransition},
	apperror.Rule{Target: ErrVersionConflict, Descriptor: apperror.SecondhandVersionConflict},
	apperror.Rule{Target: ErrThreadNotEditable, Descriptor: apperror.SecondhandNotEditable},
)
