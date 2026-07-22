package reliability

import "github.com/campusos/CampusOS/pkg/apperror"

var replayErrorTranslator = apperror.MustTranslator("platform.reliability.replay", apperror.ReliabilityReplayUnavailable,
	apperror.Rule{Target: ErrEventNotFound, Descriptor: apperror.ReliabilityEventNotFound},
	apperror.Rule{Target: ErrEventNotReplayable, Descriptor: apperror.ReliabilityEventNotReplayable},
	apperror.Rule{Target: ErrReplayIdempotencyKeyRequired, Descriptor: apperror.ReliabilityReplayKeyRequired},
	apperror.Rule{Target: ErrReplayAlreadyRequested, Descriptor: apperror.ReliabilityReplayAlreadyRequested},
	apperror.Rule{Target: ErrReplayActorRequired, Descriptor: apperror.ReliabilityReplayActorRequired},
)
