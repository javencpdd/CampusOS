package storage

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
)

// Built-in lifecycle commands can inspect their owned terminal metadata and
// retry a pending delete. This grants no authentication, byte read or path.
type OwnedObjectLifecycleCommands interface {
	GetOwnedObjectLifecycle(context.Context, string, string) (ObjectLifecycleReceipt, error)
	DeleteOwnedObjectForCommand(context.Context, ObjectLifecycleReceipt) error
}
type ObjectLifecycleReceipt struct {
	Object  Object `json:"-"`
	binding *storedObject
}
type objectDeleteExpectedKey struct{}

func cloneLifecycleObject(o Object) Object {
	o.CreatedAt, o.UpdatedAt = o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	if o.DeletedAt != nil {
		at := o.DeletedAt.UTC()
		o.DeletedAt = &at
	}
	return o
}
func cloneStoredLifecycle(x storedObject) storedObject {
	x.Object = cloneLifecycleObject(x.Object)
	return x
}
func checkStoredLifecycle(current, expected storedObject) error {
	if !reflect.DeepEqual(cloneStoredLifecycle(current), cloneStoredLifecycle(expected)) {
		return ErrObjectVersion
	}
	return nil
}
func checkExpectedDelete(ctx context.Context, x storedObject) error {
	if expected, ok := ctx.Value(objectDeleteExpectedKey{}).(storedObject); ok {
		return checkStoredLifecycle(x, expected)
	}
	return nil
}
func (s *ObjectService) GetOwnedObjectLifecycle(ctx context.Context, owner, id string) (ObjectLifecycleReceipt, error) {
	if s == nil || s.repository == nil || ctx.Err() != nil {
		return ObjectLifecycleReceipt{}, errors.Join(ErrObjectUnavailable, ctx.Err())
	}
	x, err := s.repository.GetOwnedLifecycle(ctx, owner, strings.TrimSpace(id))
	if err != nil {
		return ObjectLifecycleReceipt{}, err
	}
	x = cloneStoredLifecycle(x)
	return ObjectLifecycleReceipt{Object: cloneLifecycleObject(x.Object), binding: &x}, nil
}
func (s *ObjectService) DeleteOwnedObjectForCommand(ctx context.Context, receipt ObjectLifecycleReceipt) error {
	if receipt.binding == nil || !reflect.DeepEqual(cloneLifecycleObject(receipt.Object), cloneLifecycleObject(receipt.binding.Object)) {
		return ErrObjectVersion
	}
	expected := cloneStoredLifecycle(*receipt.binding)
	return s.Delete(context.WithValue(ctx, objectDeleteExpectedKey{}, expected), expected.OwnerID, expected.ID, expected.Version)
}
func (r *MemoryObjectRepository) GetOwnedLifecycle(ctx context.Context, owner, id string) (storedObject, error) {
	if err := ctx.Err(); err != nil {
		return storedObject{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	x, ok := r.objects[id]
	if !ok || x.OwnerID != owner {
		return storedObject{}, ErrObjectNotFound
	}
	return cloneStoredLifecycle(x), nil
}
func (r *PgObjectRepository) GetOwnedLifecycle(ctx context.Context, owner, id string) (storedObject, error) {
	query := `SELECT id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at FROM storage_objects WHERE id=$1 AND owner_user_id=$2`
	if _, ok := transaction.FromContext(ctx); ok {
		query += " FOR UPDATE"
	}
	x, err := queryStoredObject(ctx, r.db(ctx), query, id, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedObject{}, ErrObjectNotFound
	}
	return cloneStoredLifecycle(x), err
}
func (r *PgObjectRepository) lockDeleteLifecycle(ctx context.Context, owner, id string) (storedObject, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return storedObject{}, ErrObjectUnavailable
	}
	x, err := queryStoredObject(ctx, r.db(ctx), `SELECT id::text,owner_user_id::text,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at,deleted_at FROM storage_objects WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, id, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedObject{}, ErrObjectNotFound
	}
	return cloneStoredLifecycle(x), err
}
