package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
)

type ObjectService struct {
	storage    Port
	quota      Quota
	repository ObjectRepository
	maxBytes   int64
	meter      observability.Meter
}

func (s *ObjectService) SetMeter(meter observability.Meter) { s.meter = meter }

func NewObjectService(storage Port, quota Quota, repository ObjectRepository, maxBytes int64) (*ObjectService, error) {
	if storage == nil || quota == nil || repository == nil {
		return nil, errors.New("storage object dependencies are required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultQuotaBytes
	}
	return &ObjectService{storage: storage, quota: quota, repository: repository, maxBytes: maxBytes}, nil
}
func (s *ObjectService) Put(ctx context.Context, owner string, req PutRequest) (Object, error) {
	if !SafeSegment(owner) || req.Reader == nil || req.SizeHint < 0 || req.SizeHint > s.maxBytes {
		return Object{}, ErrUnsafePath
	}
	namespace, purpose, name, mime := normalizeObjectText(req.Namespace), normalizeObjectText(req.Purpose), filepath.Base(strings.TrimSpace(req.OriginalName)), normalizeObjectText(req.MimeType)
	if namespace == "" || purpose == "" || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || len([]rune(name)) > 255 {
		return Object{}, ErrUnsafePath
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if _, err := s.ensureAccount(ctx, owner); err != nil {
		return Object{}, err
	}
	id := strconv.FormatInt(idgen.New(), 10)
	item, res, err := s.repository.Reserve(ctx, storedObject{Object: Object{ID: id, OwnerID: owner, Namespace: namespace, Purpose: purpose, OriginalName: name, MimeType: mime}, storageKey: id + ".bin"}, req.SizeHint, s.quota.QuotaBytes(owner), 0)
	if err != nil {
		return Object{}, err
	}
	var path string
	committed := false
	renamed := false
	defer func() {
		if !committed {
			if renamed {
				// A database commit can fail after the provider rename. Keep the
				// object pending/aborted state and attempt immediate local cleanup;
				// if cleanup itself fails, reconcile will report the orphan.
				_ = os.Remove(path)
			}
			_ = s.repository.Abort(context.Background(), item.ID, res)
		}
	}()
	path, err = s.objectPath(owner, item.storageKey)
	if err != nil {
		return Object{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Object{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+id+"-*.tmp")
	if err != nil {
		return Object{}, err
	}
	tempPath := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(tempPath) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(req.Reader, req.SizeHint+1))
	if err != nil {
		return Object{}, err
	}
	if written > req.SizeHint {
		return Object{}, ErrObjectQuota
	}
	if err = temp.Sync(); err != nil {
		return Object{}, err
	}
	if err = temp.Close(); err != nil {
		return Object{}, err
	}
	if err = os.Rename(tempPath, path); err != nil {
		return Object{}, err
	}
	renamed = true
	item, err = s.repository.Commit(ctx, item.ID, res, written, hex.EncodeToString(hash.Sum(nil)), item.storageKey)
	if err != nil {
		return Object{}, err
	}
	committed = true
	_ = s.RefreshMetrics(ctx)
	return item.Object, nil
}
func (s *ObjectService) Open(ctx context.Context, actor, id string) (ObjectReader, error) {
	item, err := s.repository.GetOwned(ctx, actor, strings.TrimSpace(id))
	if err != nil {
		return ObjectReader{}, err
	}
	path, err := s.objectPath(actor, item.storageKey)
	if err != nil {
		return ObjectReader{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return ObjectReader{}, ErrObjectNotFound
	}
	if err != nil {
		return ObjectReader{}, err
	}
	return ObjectReader{Reader: file, Object: item.Object}, nil
}
func (s *ObjectService) Stat(ctx context.Context, actor, id string) (Object, error) {
	item, err := s.repository.GetOwned(ctx, actor, strings.TrimSpace(id))
	return item.Object, err
}
func (s *ObjectService) Delete(ctx context.Context, actor, id string, version int64) error {
	item, err := s.repository.PrepareDelete(ctx, actor, strings.TrimSpace(id), version)
	if err != nil {
		return err
	}
	path, pathErr := s.objectPath(actor, item.storageKey)
	if pathErr != nil {
		_ = s.repository.RestoreReady(context.Background(), item.ID)
		return pathErr
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = s.repository.RestoreReady(context.Background(), item.ID)
		return err
	}
	if err := s.repository.FinalizeDelete(ctx, item.ID); err != nil {
		return err
	}
	_ = s.RefreshMetrics(ctx)
	return nil
}
func (s *ObjectService) List(ctx context.Context, owner string, filter ObjectFilter, page PageRequest) (ObjectPage, error) {
	if !SafeSegment(owner) {
		return ObjectPage{}, ErrUnsafePath
	}
	return s.repository.ListOwned(ctx, owner, filter, page)
}
func (s *ObjectService) Usage(ctx context.Context, owner string) (ObjectUsage, error) {
	if !SafeSegment(owner) {
		return ObjectUsage{}, ErrUnsafePath
	}
	account, err := s.ensureAccount(ctx, owner)
	if err != nil {
		return ObjectUsage{}, err
	}
	quota := s.quota.QuotaBytes(owner)
	remaining := quota - account.UsedBytes - account.ReservedBytes
	if remaining < 0 {
		remaining = 0
	}
	return ObjectUsage{UsedBytes: account.UsedBytes, QuotaBytes: quota, RemainingBytes: remaining}, nil
}

// ReplaceCompatibility accounts for a built-in feature's explicitly retained
// legacy copy. It is deliberately not part of ObjectPort: callers cannot use
// it to obtain paths or store arbitrary new business files.
func (s *ObjectService) ReplaceCompatibility(ctx context.Context, owner string, previousBytes, nextBytes int64) error {
	if !SafeSegment(owner) || previousBytes < 0 || nextBytes < 0 {
		return ErrUnsafePath
	}
	if _, err := s.ensureAccount(ctx, owner); err != nil {
		return err
	}
	return s.repository.AdjustUsage(ctx, owner, nextBytes-previousBytes, s.quota.QuotaBytes(owner), 0)
}

// ensureAccount bridges legacy directory usage into the durable ledger once.
// Normal ObjectPort calls must not recursively walk a user's files.
func (s *ObjectService) ensureAccount(ctx context.Context, owner string) (objectAccount, error) {
	if !SafeSegment(owner) {
		return objectAccount{}, ErrUnsafePath
	}
	account, exists, err := s.repository.Account(ctx, owner)
	if err != nil || exists {
		return account, err
	}
	observed, err := s.storage.Usage(owner)
	if err != nil {
		return objectAccount{}, err
	}
	return s.repository.EnsureAccount(ctx, owner, observed)
}

// RefreshMetrics updates only aggregate lifecycle gauges. It is safe to call
// after a successful mutation or at module startup; metrics failure never
// changes an object write result.
func (s *ObjectService) RefreshMetrics(ctx context.Context) error {
	if s == nil || s.meter == nil || s.repository == nil {
		return nil
	}
	summary, err := s.repository.Summary(ctx)
	if err != nil {
		return err
	}
	for _, status := range []string{ObjectStatusPending, ObjectStatusReady, ObjectStatusDeleting, ObjectStatusDeleted, ObjectStatusQuarantined, ObjectStatusMissing} {
		_ = s.meter.SetGauge("campusos_storage_objects", observability.Labels{"status": status, "provider": "local"}, float64(summary.ObjectStatuses[status]))
	}
	for _, status := range []string{ObjectStatusPending, "committed", "released"} {
		_ = s.meter.SetGauge("campusos_storage_reservations", observability.Labels{"status": status}, float64(summary.ReservationStatuses[status]))
	}
	return nil
}
func (s *ObjectService) objectPath(owner, key string) (string, error) {
	if !SafeSegment(key) {
		return "", ErrUnsafePath
	}
	return s.storage.Path(owner, FileDir, "objects", key)
}
func (s *ObjectService) String() string {
	return fmt.Sprintf("local-object-service(max=%d)", s.maxBytes)
}
