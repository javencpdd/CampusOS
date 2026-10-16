package portadapt

import (
	"context"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
)

func (r *RepositoryUserReader) GetUserForUpdate(ctx context.Context, id string) (identityport.User, error) {
	if r == nil || !transaction.Active(ctx) {
		return identityport.User{}, errors.New("identity target account transaction unavailable")
	}
	reader, ok := r.repository.(repository.UserAuthorityLocker)
	if !ok || reader == nil || reflect.ValueOf(reader).Kind() == reflect.Pointer && reflect.ValueOf(reader).IsNil() {
		return identityport.User{}, errors.New("identity target account lock unavailable")
	}
	value, err := reader.GetByIDForUpdate(ctx, id)
	if err != nil {
		return identityport.User{}, err
	}
	return userProjection(value), nil
}
