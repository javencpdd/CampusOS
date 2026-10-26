package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
)

// A checkpoint contains no Grant, Consent, session or executable authority.
// Only its immutable identity/digest is stored with the durable operation.
type V5Checkpoint struct {
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Version string `json:"version"`
}

func v5ActivateCheckpoint(i *V5Installation, op V5Operation) {
	if i.Version != i.DesiredVersion && op.Checkpoint.ID != "" {
		i.Rollback = op.Checkpoint
	}
	i.PendingCheckpoint = V5Checkpoint{}
	i.PendingRestore = V5Checkpoint{}
	if i.PendingDataGeneration != 0 {
		i.DataGeneration = i.PendingDataGeneration
		i.PendingDataGeneration = 0
	}
}

type v5CheckpointBody struct {
	Contract      string            `json:"contract"`
	Coordinate    V5Coordinate      `json:"release"`
	Owner         string            `json:"owner_user_id"`
	PackageDigest string            `json:"package_digest"`
	Configuration V5Configuration   `json:"configuration"`
	Files         map[string][]byte `json:"files"`
}

const v5CheckpointMaxBytes = 2 << 20

func (f *V5ManagedFiles) dataView(i V5Installation, generation int64) (*os.Root, error) {
	if !v5FamilyValid(i.Publisher, i.Key) || !v5OwnerValid(i.Owner) || generation < 0 {
		return nil, ErrV5Invalid
	}
	name := "data/" + i.Publisher + "/" + i.Key + "/" + v5OwnerName(i.Owner)
	if generation > 0 {
		name = "restored-data/" + i.Publisher + "/" + i.Key + "/" + v5OwnerName(i.Owner) + "/" + v5Counter(generation)
		if generation == i.DataGeneration {
			parent, err := f.directory(path.Dir(name))
			if err != nil {
				return nil, ErrV5Rollback
			}
			defer parent.Close()
			info, err := parent.Lstat(path.Base(name))
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, ErrV5Rollback
			}
			view, err := parent.OpenRoot(path.Base(name))
			if err != nil {
				return nil, ErrV5Rollback
			}
			actual, err := view.Stat(".")
			if err != nil || !os.SameFile(info, actual) {
				view.Close()
				return nil, ErrV5Rollback
			}
			return view, nil
		}
		return f.durableDirectory(name)
	}
	return f.directory(name)
}

func (f *V5ManagedFiles) durableDirectory(name string) (*os.Root, error) {
	d, err := f.directory(name)
	if err != nil {
		return nil, err
	}
	// Sync newly published directory links as well as file contents before a
	// database pointer can select this view. Empty snapshots also need a link.
	if err := v5SyncDirectory(d); err != nil {
		d.Close()
		return nil, err
	}
	for parent := path.Dir(name); ; parent = path.Dir(parent) {
		r, err := f.root.OpenRoot(parent)
		if err != nil {
			d.Close()
			return nil, ErrV5PlatformUnavailable
		}
		err = v5SyncDirectory(r)
		r.Close()
		if err != nil {
			d.Close()
			return nil, err
		}
		if parent == "." {
			break
		}
	}
	return d, nil
}
func v5CheckpointFiles(r *os.Root, budget int64) (map[string][]byte, error) {
	files := map[string][]byte{}
	entries := 0
	err := fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return ErrV5Rollback
		}
		if name == "." {
			return nil
		}
		entries++
		if entries > 256 || len(name) > 256 || strings.Count(name, "/") > 7 || !v5PackagePath(name) {
			return ErrV5Rollback
		}
		info, err := r.Lstat(name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrV5Rollback
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > budget {
			return ErrV5Rollback
		}
		raw, err := v5ReadFile(r, name, budget)
		if err != nil {
			return ErrV5Rollback
		}
		budget -= int64(len(raw))
		files[name] = raw
		return nil
	})
	return files, err
}
func v5PrivateBudget(m *PluginV5Manifest) int64 {
	return min(m.Data.PrivateKVBytes+m.Data.PrivateObjectBytes, int64(1<<20))
}
func (s *V5Platform) captureCheckpoint(ctx context.Context, i V5Installation, id string) (V5Checkpoint, error) {
	ref := V5Checkpoint{ID: id, Version: i.Version}
	if !v5EventIDPattern.MatchString(id) {
		return V5Checkpoint{}, ErrV5Rollback
	}
	release, err := s.release(ctx, i.coordinate())
	if err != nil {
		return V5Checkpoint{}, err
	}
	if release.Storage != "available" {
		return V5Checkpoint{}, ErrV5Rollback
	}
	if _, err := s.files.Package(release.Digest); err != nil {
		return V5Checkpoint{}, ErrV5Rollback
	}
	m, err := ParsePluginV5Manifest(release.Manifest)
	if err != nil {
		return V5Checkpoint{}, err
	}
	cfg, err := s.configuration(ctx, i.coordinate(), i.Owner, m)
	if err != nil {
		return V5Checkpoint{}, err
	}
	view, err := s.files.dataView(i, i.DataGeneration)
	if err != nil {
		return V5Checkpoint{}, err
	}
	defer view.Close()
	files, err := v5CheckpointFiles(view, v5PrivateBudget(m))
	if err != nil {
		return V5Checkpoint{}, err
	}
	verified, err := v5CheckpointFiles(view, v5PrivateBudget(m))
	if err != nil || !reflect.DeepEqual(files, verified) {
		return V5Checkpoint{}, ErrV5Rollback
	}
	body := v5CheckpointBody{"campusos.plugin-checkpoint/v1", i.coordinate(), i.Owner, release.Digest, cfg, files}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > v5CheckpointMaxBytes {
		return V5Checkpoint{}, ErrV5Rollback
	}
	d, err := s.files.durableDirectory("checkpoints/" + i.Publisher + "/" + i.Key + "/" + v5OwnerName(i.Owner))
	if err != nil {
		return V5Checkpoint{}, err
	}
	defer d.Close()
	if err := v5PublishFile(d, id+".json", raw); err != nil {
		return V5Checkpoint{}, err
	}
	ref.Digest = v5BytesDigest(raw)
	return ref, nil
}
func (s *V5Platform) checkpoint(ctx context.Context, i V5Installation, ref V5Checkpoint) (v5CheckpointBody, error) {
	var body v5CheckpointBody
	if !v5EventIDPattern.MatchString(ref.ID) || !v5Digest(ref.Digest) || !v5Version(ref.Version) {
		return body, ErrV5Rollback
	}
	d, err := s.files.directory("checkpoints/" + i.Publisher + "/" + i.Key + "/" + v5OwnerName(i.Owner))
	if err != nil {
		return body, ErrV5Rollback
	}
	defer d.Close()
	raw, err := v5ReadFile(d, ref.ID+".json", v5CheckpointMaxBytes)
	if err != nil || v5BytesDigest(raw) != ref.Digest || json.Unmarshal(raw, &body) != nil {
		return body, ErrV5Rollback
	}
	expected := V5Coordinate{i.Publisher, i.Key, ref.Version}
	release, err := s.release(ctx, expected)
	if err != nil || body.Contract != "campusos.plugin-checkpoint/v1" || body.Coordinate != expected || body.Owner != i.Owner || body.PackageDigest != release.Digest || body.Configuration.V5Coordinate != expected || body.Configuration.Owner != i.Owner || body.Files == nil {
		return body, ErrV5Rollback
	}
	return body, nil
}

// Restore writes a new private view. The live pointer is switched together with
// the ready version in Observe, so audit failure/cancellation never overwrites
// the current version's data. Partial views are immutable, retryable orphans.
func (s *V5Platform) restoreCheckpoint(ctx context.Context, i *V5Installation, op V5Operation, m *PluginV5Manifest) error {
	if op.Restore.ID == "" {
		return nil
	}
	body, err := s.checkpoint(ctx, *i, op.Restore)
	if err != nil {
		return err
	}
	cfg := body.Configuration
	if err := s.validateConfig(ctx, body.Coordinate, i.Owner, m, cfg); err != nil {
		return err
	}
	current, err := s.configuration(ctx, body.Coordinate, i.Owner, m)
	if err != nil {
		return err
	}
	if !v5ConfigurationJSONEqual(current.Values, cfg.Values) || !reflect.DeepEqual(current.SecretRefs, cfg.SecretRefs) {
		cfg.Revision, err = s.store.next(ctx)
		if err != nil {
			return err
		}
		if err := s.store.save(ctx, v5Config, v5VersionOwnerID(cfg.V5Coordinate, cfg.Owner), cfg); err != nil {
			return err
		}
	}
	view, err := s.files.dataView(*i, i.Generation)
	if err != nil {
		return err
	}
	defer view.Close()
	var size int64
	for name, raw := range body.Files {
		size += int64(len(raw))
		if !v5PackagePath(name) || len(name) > 256 || strings.Count(name, "/") > 7 || size > v5PrivateBudget(m) || len(body.Files) > 256 {
			return ErrV5Rollback
		}
		// Each verified parent is opened through os.Root; no caller path or symlink
		// can redirect a restored file into another owner or host directory.
		var parent *os.Root
		if path.Dir(name) == "." {
			parent, err = s.files.dataView(*i, i.Generation)
		} else {
			parent, err = s.files.durableDirectory("restored-data/" + i.Publisher + "/" + i.Key + "/" + v5OwnerName(i.Owner) + "/" + strconv.FormatInt(i.Generation, 10) + "/" + path.Dir(name))
		}
		if err != nil {
			return ErrV5Rollback
		}
		err = v5PublishFile(parent, path.Base(name), raw)
		parent.Close()
		if err != nil {
			return ErrV5Rollback
		}
	}
	actual, err := v5CheckpointFiles(view, v5PrivateBudget(m))
	if err != nil || !reflect.DeepEqual(actual, body.Files) {
		return ErrV5Rollback
	}
	i.PendingDataGeneration = i.Generation
	return nil
}

func v5ConfigurationJSONEqual(a, b json.RawMessage) bool {
	var left, right any
	decode := func(raw json.RawMessage, out *any) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		return d.Decode(out)
	}
	return decode(a, &left) == nil && decode(b, &right) == nil && reflect.DeepEqual(left, right)
}

func (s *V5Platform) verifyUpgradeCheckpoint(ctx context.Context, i V5Installation, op V5Operation) error {
	if op.Checkpoint.ID != "" {
		if _, err := s.checkpoint(ctx, i, op.Checkpoint); err != nil {
			return err
		}
	}
	if op.Restore.ID == "" {
		return nil
	}
	body, err := s.checkpoint(ctx, i, op.Restore)
	if err != nil || i.PendingDataGeneration != i.Generation {
		return ErrV5Rollback
	}
	view, err := s.files.dataView(i, i.PendingDataGeneration)
	if err != nil {
		return ErrV5Rollback
	}
	defer view.Close()
	actual, err := v5CheckpointFiles(view, 1<<20)
	if err != nil || !reflect.DeepEqual(actual, body.Files) {
		return ErrV5Rollback
	}
	return nil
}
