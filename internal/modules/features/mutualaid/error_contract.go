package mutualaid

import "github.com/campusos/CampusOS/pkg/apperror"

var errorTranslator = apperror.MustTranslator("feature.mutual-aid", apperror.InternalError,
	apperror.Rule{Target: ErrFeatureDisabled, Descriptor: apperror.MutualAidFeatureDisabled},
	apperror.Rule{Target: ErrNotFound, Descriptor: apperror.MutualAidNotFound},
	apperror.Rule{Target: ErrForbidden, Descriptor: apperror.MutualAidForbidden},
	apperror.Rule{Target: ErrInvalidInput, Descriptor: apperror.MutualAidInvalidInput},
	apperror.Rule{Target: ErrInvalidTransition, Descriptor: apperror.MutualAidInvalidTransition},
	apperror.Rule{Target: ErrVersionConflict, Descriptor: apperror.MutualAidVersionConflict},
	apperror.Rule{Target: ErrThreadNotEditable, Descriptor: apperror.MutualAidNotEditable},
)
