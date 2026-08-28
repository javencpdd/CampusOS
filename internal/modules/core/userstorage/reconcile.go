package storage

// This file contains the read-only part of the local-object reconciliation
// contract.  It intentionally knows only relative, provider-owned locations:
// callers decide whether an explicit, audited repair is appropriate.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ReconcilePendingObjectExpired = "pending_object_expired"
	ReconcileReservationExpired   = "reservation_expired"
	ReconcileMetadataMissingFile  = "metadata_missing_physical"
	ReconcilePhysicalOrphan       = "physical_without_metadata"
	ReconcilePayloadMismatch      = "payload_hash_or_size_mismatch"
	ReconcileUnsafePath           = "unsafe_path"
	ReconcileLedgerMismatch       = "ledger_mismatch"
	ReconcileLegacyUnclassified   = "legacy_unclassified"
)

// ReconcileObject is the minimum metadata needed by a local-provider audit.
// It deliberately excludes an absolute provider path.
type ReconcileObject struct {
	ID         string
	OwnerID    string
	StorageKey string
	SizeBytes  int64
	SHA256     string
	Status     string
	UpdatedAt  time.Time
}

type ReconcileReservation struct {
	ID        string
	ObjectID  string
	OwnerID   string
	Status    string
	ExpiresAt time.Time
}

type ReconcileAccount struct {
	OwnerID       string
	UsedBytes     int64
	ReservedBytes int64
}

type ReconcileSnapshot struct {
	Objects      []ReconcileObject
	Reservations []ReconcileReservation
	Accounts     []ReconcileAccount
}

// ReconcileDifference never includes an absolute host path. RelativePath is
// available only to a local operator running the explicit maintenance command.
type ReconcileDifference struct {
	Kind          string `json:"kind"`
	OwnerID       string `json:"owner_id,omitempty"`
	ObjectID      string `json:"object_id,omitempty"`
	ReservationID string `json:"reservation_id,omitempty"`
	RelativePath  string `json:"relative_path,omitempty"`
}

type ReconcileReport struct {
	Root        string                `json:"root"`
	GeneratedAt time.Time             `json:"generated_at"`
	Differences []ReconcileDifference `json:"differences"`
	Counts      map[string]int        `json:"counts"`
}

// ReconcileLocal scans a provider root without following symbolic links. It
// changes nothing; --apply policy belongs in the operational command.
func ReconcileLocal(root string, snapshot ReconcileSnapshot, now time.Time) (ReconcileReport, error) {
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return ReconcileReport{}, err
	}
	report := ReconcileReport{Root: cleanRoot, GeneratedAt: now.UTC(), Counts: map[string]int{}}
	add := func(item ReconcileDifference) {
		report.Differences = append(report.Differences, item)
		report.Counts[item.Kind]++
	}

	expected := make(map[string]ReconcileObject, len(snapshot.Objects))
	metadataIDs := make(map[string]struct{}, len(snapshot.Objects))
	for _, item := range snapshot.Objects {
		if item.Status == ObjectStatusDeleted {
			continue
		}
		metadataIDs[item.ID] = struct{}{}
		if !SafeSegment(item.OwnerID) || !SafeSegment(item.StorageKey) {
			add(ReconcileDifference{Kind: ReconcileUnsafePath, OwnerID: item.OwnerID, ObjectID: item.ID})
			continue
		}
		path, pathErr := safeObjectFilePath(cleanRoot, item.OwnerID, item.StorageKey)
		if pathErr != nil {
			add(ReconcileDifference{Kind: ReconcileUnsafePath, OwnerID: item.OwnerID, ObjectID: item.ID})
			continue
		}
		expected[path] = item
		if item.Status == ObjectStatusPending && !item.UpdatedAt.IsZero() && !item.UpdatedAt.After(now.Add(-15*time.Minute)) {
			add(ReconcileDifference{Kind: ReconcilePendingObjectExpired, OwnerID: item.OwnerID, ObjectID: item.ID})
		}
	}

	for _, item := range snapshot.Reservations {
		if item.Status == ObjectStatusPending && !item.ExpiresAt.IsZero() && !item.ExpiresAt.After(now) {
			add(ReconcileDifference{Kind: ReconcileReservationExpired, OwnerID: item.OwnerID, ObjectID: item.ObjectID, ReservationID: item.ID})
		}
	}

	seen := make(map[string]struct{}, len(expected))
	physicalByOwner := make(map[string]int64)
	if info, statErr := os.Lstat(cleanRoot); statErr == nil && !info.IsDir() {
		add(ReconcileDifference{Kind: ReconcileUnsafePath})
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return ReconcileReport{}, statErr
	} else if statErr == nil {
		err = filepath.WalkDir(cleanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, relErr := filepath.Rel(cleanRoot, path)
			if relErr != nil || rel == "." {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if entry.Type()&os.ModeSymlink != 0 {
				add(ReconcileDifference{Kind: ReconcileUnsafePath, RelativePath: rel})
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			parts := strings.Split(rel, "/")
			owner := ""
			if len(parts) > 0 && SafeSegment(parts[0]) {
				owner = parts[0]
			}
			fileInfo, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if owner != "" {
				physicalByOwner[owner] += fileInfo.Size()
			} else {
				add(ReconcileDifference{Kind: ReconcileUnsafePath, RelativePath: rel})
				return nil
			}
			if item, ok := expected[path]; ok {
				seen[path] = struct{}{}
				sum, sumErr := localFileSHA256(path)
				if sumErr != nil {
					return sumErr
				}
				if item.SizeBytes != fileInfo.Size() || !strings.EqualFold(item.SHA256, sum) {
					add(ReconcileDifference{Kind: ReconcilePayloadMismatch, OwnerID: owner, ObjectID: item.ID, RelativePath: rel})
				}
				return nil
			}
			if len(parts) == 4 && parts[1] == FileDir && parts[2] == "objects" && SafeSegment(parts[3]) {
				add(ReconcileDifference{Kind: ReconcilePhysicalOrphan, OwnerID: owner, RelativePath: rel})
			} else {
				add(ReconcileDifference{Kind: ReconcileLegacyUnclassified, OwnerID: owner, RelativePath: rel})
			}
			return nil
		})
		if err != nil {
			return ReconcileReport{}, err
		}
	}

	for path, item := range expected {
		if _, ok := seen[path]; !ok && item.Status == ObjectStatusReady {
			rel, _ := filepath.Rel(cleanRoot, path)
			add(ReconcileDifference{Kind: ReconcileMetadataMissingFile, OwnerID: item.OwnerID, ObjectID: item.ID, RelativePath: filepath.ToSlash(rel)})
		}
	}
	for _, account := range snapshot.Accounts {
		if physicalByOwner[account.OwnerID] != account.UsedBytes {
			add(ReconcileDifference{Kind: ReconcileLedgerMismatch, OwnerID: account.OwnerID})
		}
	}
	sort.Slice(report.Differences, func(i, j int) bool {
		left, right := report.Differences[i], report.Differences[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.OwnerID != right.OwnerID {
			return left.OwnerID < right.OwnerID
		}
		if left.ObjectID != right.ObjectID {
			return left.ObjectID < right.ObjectID
		}
		return left.RelativePath < right.RelativePath
	})
	return report, nil
}

func safeObjectFilePath(root, owner, key string) (string, error) {
	if !SafeSegment(owner) || !SafeSegment(key) {
		return "", ErrUnsafePath
	}
	base := filepath.Join(root, owner, FileDir, "objects")
	path := filepath.Join(base, key)
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil || !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		return "", ErrUnsafePath
	}
	return clean, nil
}

func localFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
