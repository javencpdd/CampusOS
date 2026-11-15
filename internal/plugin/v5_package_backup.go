package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// The repository contains compressed, inert distribution files only. It is
// never an execution/package lookup root and contains no grants or user data.
type v5PackageBackup struct {
	Contract string `json:"contract"`
	V5Coordinate
	Digest string `json:"package_digest"`
	Size   int    `json:"size_bytes"`
}

func v5BackupMetadata(r V5PackageRelease) ([]byte, error) {
	if !r.valid() || !v5Digest(r.Digest) || r.Size < 1 || r.Size > PluginV5MaxPackageBytes {
		return nil, ErrV5Invalid
	}
	raw, err := json.Marshal(v5PackageBackup{"campusos.plugin-backup/v1", r.V5Coordinate, r.Digest, r.Size})
	if err != nil {
		return nil, err
	}
	return v5BackupJSON(raw)
}

func v5BackupJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > PluginV5ManifestMaxBytes {
		return nil, ErrV5Invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document map[string]any
	if decoder.Decode(&document) != nil || document == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrV5Invalid
	}
	return json.Marshal(document)
}

// BackupPackage durably publishes the original compressed bytes and detached
// signature under the host-private temporary repository. Repeated backups
// verify existing immutable files; conflicting or unsafe files are rejected.
func (f *V5ManagedFiles) BackupPackage(r V5PackageRelease) error {
	metadata, err := v5BackupMetadata(r)
	if err != nil {
		return err
	}
	manifest, err := v5BackupJSON(r.Manifest)
	if err != nil {
		return err
	}
	release, err := v5BackupJSON(r.Release)
	if err != nil {
		return err
	}
	raw, err := f.Package(r.Digest)
	if err == ErrV5NotFound {
		_, err = f.RepositoryPackage(r)
		return err
	}
	if err != nil || len(raw) != r.Size {
		return ErrPluginV5PackageDigest
	}
	d, err := f.durableDirectory("repository/" + r.Publisher + "/" + r.Key + "/" + r.Version)
	if err != nil {
		return err
	}
	defer d.Close()
	for _, file := range []struct {
		name string
		raw  []byte
	}{
		{"package.tar.gz", raw}, {"release.json", release}, {"manifest.json", manifest}, {"metadata.json", metadata},
	} {
		if err := v5PublishFile(d, file.name, file.raw); err != nil {
			return err
		}
	}
	_, err = f.RepositoryPackage(r)
	return err
}

// RepositoryPackage is exclusively for backup verification and data snapshots.
// Runtime admission, UI assets and imports continue to use Package and current
// signature/trust checks; these bytes can never silently revive old execution.
func (f *V5ManagedFiles) RepositoryPackage(r V5PackageRelease) ([]byte, error) {
	metadata, err := v5BackupMetadata(r)
	if err != nil {
		return nil, err
	}
	d, err := f.directory("repository/" + r.Publisher + "/" + r.Key + "/" + r.Version)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	for _, file := range []struct {
		name string
		raw  []byte
	}{
		{"metadata.json", metadata}, {"manifest.json", r.Manifest}, {"release.json", r.Release},
	} {
		expected, err := v5BackupJSON(file.raw)
		if err != nil {
			return nil, err
		}
		actual, err := v5ReadFile(d, file.name, PluginV5ManifestMaxBytes)
		if err != nil || !bytes.Equal(actual, expected) {
			return nil, ErrV5Rollback
		}
	}
	raw, err := v5ReadFile(d, "package.tar.gz", PluginV5MaxPackageBytes)
	if err != nil || len(raw) != r.Size || v5BytesDigest(raw) != r.Digest {
		return nil, ErrV5Rollback
	}
	return raw, nil
}

func (s *V5Platform) processPackagePurge(ctx context.Context, op V5Operation) error {
	for phase := 0; phase < 2; phase++ {
		continueCleanup := false
		err := s.system(ctx, op, "purge_release", func(tx context.Context, current *V5Operation) error {
			if current.Lease != op.Lease || current.Status != "working" && !(current.Status == "queued" && current.CleanupAfter != "") {
				return errV5Replay
			}
			r, err := s.release(tx, op.V5Coordinate)
			if err != nil {
				return err
			}
			if current.CleanupAfter != "" {
				if r.Storage != "purged" || current.CleanupAfter != "repository:"+v5Counter(r.Revision) {
					current.Status = "superseded"
					return nil
				}
				if err := s.canPurgeRelease(tx, op.V5Coordinate); err != nil {
					return err
				}
				if _, err := s.files.RepositoryPackage(r); err != nil {
					return err
				}
				if err := s.files.RemovePackage(r.Digest); err != nil {
					return err
				}
				current.Status = "done"
				return nil
			}
			if r.Revision != op.StorageRevision {
				current.Status = "superseded"
				return nil
			}
			if err := s.canPurgeRelease(tx, op.V5Coordinate); err != nil {
				return err
			}
			if err := s.files.BackupPackage(r); err != nil {
				return err
			}
			r.Storage = "purged"
			r.Revision, err = s.store.next(tx)
			if err != nil {
				return err
			}
			if err := s.store.save(tx, v5Release, r.id(), r); err != nil {
				return err
			}
			// Commit the verified backup and non-executable state before deleting
			// hot bytes. Audit/commit failure leaves the original package intact;
			// later retries resume this phase, and a reimport supersedes deletion.
			current.CleanupAfter = "repository:" + v5Counter(r.Revision)
			current.Status = "queued"
			current.LeaseUntil = 0
			continueCleanup = true
			return nil
		})
		if err != nil {
			return err
		}
		if !continueCleanup {
			return nil
		}
	}
	return nil
}
