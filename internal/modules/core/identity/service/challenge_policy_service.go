package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrChallengePolicyInvalid = errors.New("challenge policy is invalid")

type ChallengePolicyService struct {
	store    repository.ChallengePolicyRepository
	reliable *reliability.Service
	clock    func() time.Time
}

func NewChallengePolicyService(store repository.ChallengePolicyRepository) (*ChallengePolicyService, error) {
	if store == nil {
		return nil, errors.New("challenge policy repository is required")
	}
	return &ChallengePolicyService{store: store, clock: time.Now}, nil
}

func (s *ChallengePolicyService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable != nil {
		if snapshotter, ok := s.store.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *ChallengePolicyService) GetChallengePolicy(ctx context.Context) (*domain.ChallengePolicy, error) {
	policy, err := s.store.GetChallengePolicy(ctx)
	if err != nil {
		return nil, err
	}
	if err := ValidateChallengePolicy(policy); err != nil {
		return nil, err
	}
	copy := *policy
	return &copy, nil
}

func (s *ChallengePolicyService) UpdateChallengePolicy(ctx context.Context, actorID string, request domain.UpdateChallengePolicyRequest) (*domain.ChallengePolicy, error) {
	actorID = strings.TrimSpace(actorID)
	policy := &domain.ChallengePolicy{
		ID:                 domain.ChallengePolicyID,
		EmailWindowMinutes: request.EmailWindowMinutes,
		EmailMaxRequests:   request.EmailMaxRequests,
		IPWindowMinutes:    request.IPWindowMinutes,
		IPMaxRequests:      request.IPMaxRequests,
		Version:            request.ExpectedVersion,
		UpdatedBy:          actorID,
		UpdatedAt:          s.clock().UTC(),
	}
	if actorID == "" || request.ExpectedVersion < 1 {
		return nil, ErrChallengePolicyInvalid
	}
	if err := ValidateChallengePolicy(policy); err != nil {
		return nil, err
	}
	event, err := reliability.NewEvent("identity.challenge.policy.updated.v1", "identity_challenge_policy", policy.ID, struct {
		PolicyID string `json:"policy_id"`
		Version  int64  `json:"version"`
	}{PolicyID: policy.ID, Version: request.ExpectedVersion + 1})
	if err != nil {
		return nil, err
	}
	action := func(commandCtx context.Context) error {
		return s.store.UpdateChallengePolicy(commandCtx, policy, request.ExpectedVersion)
	}
	if s.reliable != nil {
		err = s.reliable.Execute(ctx, reliability.Command{
			Code: "identity.challenge_policy.update", ActorID: actorID, ActorType: "user",
			ResourceType: "identity_challenge_policy", ResourceID: policy.ID,
			OperationCode: "http.identity.challenge_policy.update", PermissionCode: "identity.challenge_policy.update",
			Event: &event,
		}, action)
	} else {
		err = action(ctx)
	}
	if err != nil {
		return nil, err
	}
	copy := *policy
	return &copy, nil
}

func ValidateChallengePolicy(policy *domain.ChallengePolicy) error {
	if policy == nil || policy.ID != domain.ChallengePolicyID || policy.Version < 1 {
		return ErrChallengePolicyInvalid
	}
	if policy.EmailWindowMinutes < domain.ChallengePolicyMinWindowMinutes || policy.EmailWindowMinutes > domain.ChallengePolicyMaxWindowMinutes ||
		policy.IPWindowMinutes < domain.ChallengePolicyMinWindowMinutes || policy.IPWindowMinutes > domain.ChallengePolicyMaxWindowMinutes ||
		policy.EmailMaxRequests < domain.ChallengePolicyMinEmailRequests || policy.EmailMaxRequests > domain.ChallengePolicyMaxEmailRequests ||
		policy.IPMaxRequests < domain.ChallengePolicyMinIPRequests || policy.IPMaxRequests > domain.ChallengePolicyMaxIPRequests {
		return fmt.Errorf("%w: values are outside the supported bounds", ErrChallengePolicyInvalid)
	}
	return nil
}

var _ ChallengePolicyReader = (*ChallengePolicyService)(nil)
