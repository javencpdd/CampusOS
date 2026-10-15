package storage

import (
	"context"
	"errors"
	"strings"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// OwnedObjectCommandLocker binds a built-in command to current metadata. It
// exposes no provider path and is not an external-plugin capability.
type OwnedObjectCommandLocker interface {
	LockOwnedObjectForCommand(context.Context, string, string) (Object, error)
}

type ownedObjectRepositoryLocker interface {
	lockOwnedForCommand(context.Context, string, string) (storedObject, error)
}

func (s *ObjectService) LockOwnedObjectForCommand(ctx context.Context, owner, id string) (Object, error) {
	if !transaction.Active(ctx) || !SafeSegment(owner) {
		return Object{}, ErrObjectUnavailable
	}
	r, ok := s.repository.(ownedObjectRepositoryLocker)
	if !ok {
		return Object{}, ErrObjectUnavailable
	}
	x, err := r.lockOwnedForCommand(ctx, owner, strings.TrimSpace(id))
	return x.Object, err
}

func (r *MemoryObjectRepository) lockOwnedForCommand(ctx context.Context, owner, id string) (storedObject, error) {
	if !transaction.Active(ctx) {
		return storedObject{}, ErrObjectUnavailable
	}
	return r.GetOwned(ctx, owner, id)
}

func (r *PgObjectRepository) lockOwnedForCommand(ctx context.Context, owner, id string) (storedObject, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return storedObject{}, ErrObjectUnavailable
	}
	x, err := queryStoredObject(ctx, r.db(ctx), `SELECT id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at FROM storage_objects WHERE id=$1 AND owner_user_id=$2 AND status='ready' FOR UPDATE`, id, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedObject{}, ErrObjectNotFound
	}
	return x, err
}
