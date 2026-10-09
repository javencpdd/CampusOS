package portadapt

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
)

type RepositoryUserReader struct{ repository repository.UserRepository }

type AccountLookup interface {
	GetEmailAccount(context.Context, string) (*domain.EmailAccount, error)
}

type ServiceAccountReader struct{ lookup AccountLookup }

type ServiceSessionVerifier struct{ lookup SessionLookup }

type SessionLookup interface {
	VerifyAccess(context.Context, *auth.JWTClaims) error
}

func NewServiceAccountReader(lookup AccountLookup) *ServiceAccountReader {
	return &ServiceAccountReader{lookup: lookup}
}

func NewServiceSessionVerifier(lookup SessionLookup) *ServiceSessionVerifier {
	return &ServiceSessionVerifier{lookup: lookup}
}

func (r *ServiceSessionVerifier) VerifyAccess(ctx context.Context, claims *auth.JWTClaims) error {
	if r == nil || r.lookup == nil {
		return errors.New("identity session verifier is unavailable")
	}
	return r.lookup.VerifyAccess(ctx, claims)
}

func (r *ServiceAccountReader) GetEmailAccount(ctx context.Context, userID string) (identityport.EmailAccount, error) {
	value, err := r.lookup.GetEmailAccount(ctx, userID)
	if err != nil {
		return identityport.EmailAccount{}, err
	}
	return identityport.EmailAccount{
		UserID:               value.UserID,
		IdentifierNormalized: value.IdentifierNormalized,
		VerificationState:    string(value.VerificationState),
		VerifiedAt:           value.VerifiedAt,
		CredentialVersion:    value.CredentialVersion,
	}, nil
}

type ChallengeDispatchLookup interface {
	Dispatch(context.Context, string) (*domain.ChallengeDispatch, error)
}

type ServiceChallengeDispatchReader struct{ lookup ChallengeDispatchLookup }

func NewServiceChallengeDispatchReader(lookup ChallengeDispatchLookup) *ServiceChallengeDispatchReader {
	return &ServiceChallengeDispatchReader{lookup: lookup}
}

func (r *ServiceChallengeDispatchReader) Dispatch(ctx context.Context, challengeID string) (identityport.ChallengeDispatch, error) {
	value, err := r.lookup.Dispatch(ctx, challengeID)
	if err != nil {
		if errors.Is(err, service.ErrChallengeInvalid) || errors.Is(err, repository.ErrChallengeNotFound) {
			return identityport.ChallengeDispatch{}, identityport.ErrChallengeNotDeliverable
		}
		return identityport.ChallengeDispatch{}, err
	}
	return identityport.ChallengeDispatch{
		ChallengeID: value.ChallengeID,
		PublicID:    value.PublicID,
		Purpose:     string(value.Purpose),
		Email:       value.Email,
		Code:        value.Code,
		ExpiresAt:   value.ExpiresAt,
	}, nil
}

func NewRepositoryUserReader(repository repository.UserRepository) *RepositoryUserReader {
	return &RepositoryUserReader{repository: repository}
}
func (r *RepositoryUserReader) GetUser(ctx context.Context, id string) (identityport.User, error) {
	value, err := r.repository.GetByID(ctx, id)
	if err != nil {
		return identityport.User{}, err
	}
	return userProjection(value), nil
}

func (r *RepositoryUserReader) GetUserByUsername(ctx context.Context, username string) (identityport.User, error) {
	value, err := r.repository.GetByUsername(ctx, username)
	if err != nil {
		return identityport.User{}, err
	}
	return userProjection(value), nil
}

func userProjection(value *domain.User) identityport.User {
	if value == nil {
		return identityport.User{}
	}
	return identityport.User{
		ID:       value.ID,
		Username: value.Username,
		Nickname: value.Nickname,
		Email:    value.Email,
		Avatar:   value.Avatar,
		Bio:      value.Bio,
		Status:   string(value.Status),
	}
}
