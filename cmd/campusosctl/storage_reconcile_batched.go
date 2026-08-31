package main

// This file implements the capacity-safe execution path for
// `campusosctl storage reconcile`.  The storage package keeps a small
// all-in-memory ReconcileLocal helper for focused unit tests; the operator CLI
// must not use that helper for a real provider tree because a deployed object
// catalog can be much larger than process memory.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const storageReconcileCheckpointSchema = "campusos.storage-reconcile-checkpoint/v1"

type storageReconcileBatchConfig struct {
	Root           string
	BatchSize      int
	CheckpointPath string
	Resume         bool
	MaxDifferences int
	Now            time.Time
}

// storageReconcileCheckpoint retains only cursors, aggregate counts, and a
// deliberately bounded sample of differences.  It contains no provider
// absolute path, content, hash, or user file name.
type storageReconcileCheckpoint struct {
	Schema               string                  `json:"schema"`
	RootDigest           string                  `json:"root_digest"`
	Phase                string                  `json:"phase"`
	ObjectCursor         string                  `json:"object_cursor,omitempty"`
	ReservationCursor    string                  `json:"reservation_cursor,omitempty"`
	PhysicalOwnerCursor  string                  `json:"physical_owner_cursor,omitempty"`
	AccountCursor        string                  `json:"account_cursor,omitempty"`
	Report               storage.ReconcileReport `json:"report"`
	DifferencesTruncated bool                    `json:"differences_truncated,omitempty"`
}

type storageReconcileAccumulator struct {
	report    storage.ReconcileReport
	maximum   int
	truncated bool
}

func newStorageReconcileAccumulator(root string, now time.Time, maximum int) *storageReconcileAccumulator {
	return &storageReconcileAccumulator{
		report: storage.ReconcileReport{
			Root:        filepath.Base(root),
			GeneratedAt: now.UTC(),
			Counts:      map[string]int{},
		},
		maximum: maximum,
	}
}

func (a *storageReconcileAccumulator) add(item storage.ReconcileDifference) {
	if a.report.Counts == nil {
		a.report.Counts = map[string]int{}
	}
	a.report.Counts[item.Kind]++
	if a.maximum > 0 && len(a.report.Differences) >= a.maximum {
		a.truncated = true
		return
	}
	a.report.Differences = append(a.report.Differences, item)
}

func (a *storageReconcileAccumulator) checkpoint(state *storageReconcileCheckpoint) {
	state.Report = a.report
	state.DifferencesTruncated = a.truncated
}

func accumulatorFromCheckpoint(state storageReconcileCheckpoint, maximum int) *storageReconcileAccumulator {
	if state.Report.Counts == nil {
		state.Report.Counts = map[string]int{}
	}
	return &storageReconcileAccumulator{report: state.Report, maximum: maximum, truncated: state.DifferencesTruncated}
}

// reconcileStorageBatched avoids loading all object, reservation, account or
// physical-file metadata at once.  Database tables use stable keyset cursors;
// provider scanning advances one owner directory at a time.  An optional
// checkpoint is atomically persisted after every bounded unit of work, so an
// operator can resume a long dry-run without restarting from the beginning.
func reconcileStorageBatched(ctx context.Context, pool *pgxpool.Pool, config storageReconcileBatchConfig) (storage.ReconcileReport, bool, error) {
	if pool == nil {
		return storage.ReconcileReport{}, false, errors.New("storage database pool is required")
	}
	root, err := filepath.Abs(filepath.Clean(config.Root))
	if err != nil {
		return storage.ReconcileReport{}, false, err
	}
	if config.BatchSize < 10 || config.BatchSize > 1000 {
		return storage.ReconcileReport{}, false, errors.New("reconcile batch size must be between 10 and 1000")
	}
	if config.MaxDifferences < 0 {
		return storage.ReconcileReport{}, false, errors.New("reconcile maximum differences must not be negative")
	}
	if config.Now.IsZero() {
		config.Now = time.Now().UTC()
	}

	digest := sha256.Sum256([]byte(root))
	state := storageReconcileCheckpoint{
		Schema:     storageReconcileCheckpointSchema,
		RootDigest: hex.EncodeToString(digest[:]),
		Phase:      "objects",
	}
	accumulator := newStorageReconcileAccumulator(root, config.Now, config.MaxDifferences)
	if config.Resume {
		state, err = readStorageReconcileCheckpoint(config.CheckpointPath)
		if err != nil {
			return storage.ReconcileReport{}, false, err
		}
		if state.Schema != storageReconcileCheckpointSchema || state.RootDigest != hex.EncodeToString(digest[:]) {
			return storage.ReconcileReport{}, false, errors.New("checkpoint does not belong to this provider root or schema")
		}
		accumulator = accumulatorFromCheckpoint(state, config.MaxDifferences)
	}

	save := func() error {
		if strings.TrimSpace(config.CheckpointPath) == "" {
			return nil
		}
		accumulator.checkpoint(&state)
		return writeStorageReconcileCheckpoint(config.CheckpointPath, state)
	}

	if state.Phase == "completed" {
		return accumulator.report, accumulator.truncated, nil
	}
	if state.Phase == "objects" {
		if err := reconcileObjectBatches(ctx, pool, root, &state, accumulator, config.BatchSize, config.Now, save); err != nil {
			return storage.ReconcileReport{}, false, err
		}
		state.Phase = "reservations"
		if err := save(); err != nil {
			return storage.ReconcileReport{}, false, err
		}
	}
	if state.Phase == "reservations" {
		if err := reconcileReservationBatches(ctx, pool, &state, accumulator, config.BatchSize, config.Now, save); err != nil {
			return storage.ReconcileReport{}, false, err
		}
		state.Phase = "physical"
		if err := save(); err != nil {
			return storage.ReconcileReport{}, false, err
		}
	}
	if state.Phase == "physical" {
		if err := reconcilePhysicalOwners(ctx, pool, root, &state, accumulator, config.BatchSize, save); err != nil {
			return storage.ReconcileReport{}, false, err
		}
		state.Phase = "accounts"
		if err := save(); err != nil {
			return storage.ReconcileReport{}, false, err
		}
	}
	if state.Phase == "accounts" {
		if err := reconcileAccountBatches(ctx, pool, root, &state, accumulator, config.BatchSize, save); err != nil {
			return storage.ReconcileReport{}, false, err
		}
		state.Phase = "completed"
		if err := save(); err != nil {
			return storage.ReconcileReport{}, false, err
		}
	}
	if state.Phase != "completed" {
		return storage.ReconcileReport{}, false, errors.New("checkpoint has an unknown reconciliation phase")
	}
	return accumulator.report, accumulator.truncated, nil
}

func reconcileObjectBatches(ctx context.Context, pool *pgxpool.Pool, root string, state *storageReconcileCheckpoint, accumulator *storageReconcileAccumulator, batchSize int, now time.Time, save func() error) error {
	cursor := state.ObjectCursor
	if cursor == "" {
		cursor = "0"
	}
	for {
		rows, err := pool.Query(ctx, `SELECT id::text,owner_user_id::text,storage_key,size_bytes,sha256,status,updated_at
			FROM storage_objects WHERE id>$1::bigint ORDER BY id ASC LIMIT $2`, cursor, batchSize)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			count++
			var item storage.ReconcileObject
			if err := rows.Scan(&item.ID, &item.OwnerID, &item.StorageKey, &item.SizeBytes, &item.SHA256, &item.Status, &item.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			state.ObjectCursor = item.ID
			reconcileObjectFile(root, item, now, accumulator)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count == 0 {
			return nil
		}
		cursor = state.ObjectCursor
		if err := save(); err != nil {
			return err
		}
	}
}

func reconcileObjectFile(root string, item storage.ReconcileObject, now time.Time, accumulator *storageReconcileAccumulator) {
	if item.Status == storage.ObjectStatusDeleted {
		return
	}
	if item.Status == storage.ObjectStatusPending && !item.UpdatedAt.IsZero() && !item.UpdatedAt.After(now.Add(-15*time.Minute)) {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcilePendingObjectExpired, OwnerID: item.OwnerID, ObjectID: item.ID})
	}
	if item.Status != storage.ObjectStatusReady {
		return
	}
	path, relative, ok := reconcileObjectPath(root, item.OwnerID, item.StorageKey)
	if !ok {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: item.OwnerID, ObjectID: item.ID})
		return
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileMetadataMissingFile, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: relative})
		return
	}
	if err != nil {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: relative})
		return
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: relative})
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileMetadataMissingFile, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: relative})
		return
	}
	hash, err := reconcileFileSHA256(path)
	if err != nil || item.SizeBytes != info.Size() || !strings.EqualFold(item.SHA256, hash) {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcilePayloadMismatch, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: relative})
	}
}

func reconcileReservationBatches(ctx context.Context, pool *pgxpool.Pool, state *storageReconcileCheckpoint, accumulator *storageReconcileAccumulator, batchSize int, now time.Time, save func() error) error {
	cursor := state.ReservationCursor
	if cursor == "" {
		cursor = "0"
	}
	for {
		rows, err := pool.Query(ctx, `SELECT id::text,object_id::text,user_id::text,status,expires_at
			FROM user_storage_reservations WHERE id>$1::bigint ORDER BY id ASC LIMIT $2`, cursor, batchSize)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			count++
			var item storage.ReconcileReservation
			if err := rows.Scan(&item.ID, &item.ObjectID, &item.OwnerID, &item.Status, &item.ExpiresAt); err != nil {
				rows.Close()
				return err
			}
			state.ReservationCursor = item.ID
			if item.Status == storage.ObjectStatusPending && !item.ExpiresAt.IsZero() && !item.ExpiresAt.After(now) {
				accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileReservationExpired, OwnerID: item.OwnerID, ObjectID: item.ObjectID, ReservationID: item.ID})
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count == 0 {
			return nil
		}
		cursor = state.ReservationCursor
		if err := save(); err != nil {
			return err
		}
	}
}

func reconcilePhysicalOwners(ctx context.Context, pool *pgxpool.Pool, root string, state *storageReconcileCheckpoint, accumulator *storageReconcileAccumulator, batchSize int, save func() error) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath})
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if state.PhysicalOwnerCursor != "" && name <= state.PhysicalOwnerCursor {
			continue
		}
		relative := filepath.ToSlash(name)
		if entry.Type()&os.ModeSymlink != 0 {
			accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, RelativePath: relative})
		} else if !entry.IsDir() || !validReconcileOwnerID(name) {
			accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileInvalidOwnerDirectory, RelativePath: relative})
		} else if err := reconcilePhysicalOwner(ctx, pool, root, name, accumulator, batchSize); err != nil {
			return err
		}
		state.PhysicalOwnerCursor = name
		if err := save(); err != nil {
			return err
		}
	}
	return nil
}

func reconcilePhysicalOwner(ctx context.Context, pool *pgxpool.Pool, root, owner string, accumulator *storageReconcileAccumulator, batchSize int) error {
	ownerRoot := filepath.Join(root, owner)
	var physicalBytes int64
	keys := make([]string, 0, batchSize)
	flushKeys := func() error {
		if len(keys) == 0 {
			return nil
		}
		rows, err := pool.Query(ctx, `SELECT storage_key FROM storage_objects
			WHERE owner_user_id=$1::bigint AND provider='local' AND storage_key=ANY($2::text[]) AND status<>'deleted'`, owner, keys)
		if err != nil {
			return err
		}
		known := make(map[string]struct{}, len(keys))
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			known[key] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, key := range keys {
			if _, exists := known[key]; !exists {
				accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcilePhysicalOrphan, OwnerID: owner, RelativePath: filepath.ToSlash(filepath.Join(owner, storage.FileDir, "objects", key))})
			}
		}
		keys = keys[:0]
		return nil
	}

	err := filepath.WalkDir(ownerRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == ownerRoot {
			return nil
		}
		relative, relErr := filepath.Rel(ownerRoot, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: owner, RelativePath: filepath.ToSlash(filepath.Join(owner, relative))})
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		physicalBytes += info.Size()
		parts := strings.Split(relative, "/")
		if len(parts) == 3 && parts[0] == storage.FileDir && parts[1] == "objects" {
			if !storage.SafeSegment(parts[2]) {
				accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: owner, RelativePath: filepath.ToSlash(filepath.Join(owner, relative))})
				return nil
			}
			keys = append(keys, parts[2])
			if len(keys) >= batchSize {
				return flushKeys()
			}
			return nil
		}
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileLegacyUnclassified, OwnerID: owner, RelativePath: filepath.ToSlash(filepath.Join(owner, relative))})
		return nil
	})
	if err != nil {
		return err
	}
	if err := flushKeys(); err != nil {
		return err
	}
	var used int64
	err = pool.QueryRow(ctx, `SELECT used_bytes FROM user_storage_accounts WHERE user_id=$1::bigint`, owner).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if used != physicalBytes {
		accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileLedgerMismatch, OwnerID: owner})
	}
	return nil
}

// Accounts belonging to a missing user directory are not visited during the
// physical pass.  This final keyset scan detects their non-zero ledgers
// without retaining the complete account list in memory.
func reconcileAccountBatches(ctx context.Context, pool *pgxpool.Pool, root string, state *storageReconcileCheckpoint, accumulator *storageReconcileAccumulator, batchSize int, save func() error) error {
	cursor := state.AccountCursor
	if cursor == "" {
		cursor = "0"
	}
	for {
		rows, err := pool.Query(ctx, `SELECT user_id::text,used_bytes FROM user_storage_accounts WHERE user_id>$1::bigint ORDER BY user_id ASC LIMIT $2`, cursor, batchSize)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			count++
			var owner string
			var used int64
			if err := rows.Scan(&owner, &used); err != nil {
				rows.Close()
				return err
			}
			state.AccountCursor = owner
			path := filepath.Join(root, owner)
			info, statErr := os.Lstat(path)
			if errors.Is(statErr, os.ErrNotExist) {
				if used != 0 {
					accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileLedgerMismatch, OwnerID: owner})
				}
				continue
			}
			if statErr != nil {
				return statErr
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileUnsafePath, OwnerID: owner})
				if used != 0 {
					accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcileLedgerMismatch, OwnerID: owner})
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count == 0 {
			return nil
		}
		cursor = state.AccountCursor
		if err := save(); err != nil {
			return err
		}
	}
}

func reconcileObjectPath(root, owner, key string) (string, string, bool) {
	if !storage.SafeSegment(owner) || !storage.SafeSegment(key) {
		return "", "", false
	}
	base := filepath.Join(root, owner, storage.FileDir, "objects")
	path := filepath.Join(base, key)
	cleanBase, baseErr := filepath.Abs(filepath.Clean(base))
	cleanPath, pathErr := filepath.Abs(filepath.Clean(path))
	if baseErr != nil || pathErr != nil || !strings.HasPrefix(cleanPath, cleanBase+string(os.PathSeparator)) {
		return "", "", false
	}
	return cleanPath, filepath.ToSlash(filepath.Join(owner, storage.FileDir, "objects", key)), true
}

func reconcileFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validReconcileOwnerID(value string) bool {
	if !storage.SafeSegment(value) {
		return false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0
}

func readStorageReconcileCheckpoint(path string) (storageReconcileCheckpoint, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return storageReconcileCheckpoint{}, err
	}
	var state storageReconcileCheckpoint
	if err := json.Unmarshal(payload, &state); err != nil {
		return storageReconcileCheckpoint{}, err
	}
	if state.Schema != storageReconcileCheckpointSchema {
		return storageReconcileCheckpoint{}, errors.New("unsupported storage reconciliation checkpoint schema")
	}
	return state, nil
}

func writeStorageReconcileCheckpoint(path string, state storageReconcileCheckpoint) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		if err != nil {
			return err
		}
		return errors.New("storage reconciliation checkpoint parent is not a directory")
	}
	temporary, err := os.CreateTemp(directory, ".storage-reconcile-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
