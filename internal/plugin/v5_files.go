package plugin

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

var (
	ErrV5PlatformUnavailable = errors.New("plugin.platform_unavailable")
	ErrV5NotFound            = errors.New("plugin.not_found")
	ErrV5Conflict            = errors.New("plugin.lifecycle_generation_conflict")
	ErrV5Denied              = errors.New("plugin.permission_denied")
	ErrV5Invalid             = errors.New("plugin.lifecycle_invalid")
	ErrV5Rollback            = errors.New("plugin.lifecycle_rollback_unverifiable")
)

// V5ManagedFiles never extracts archives or accepts a client filesystem path.
// All writes are under the operator-configured, private v5 namespace.
type V5ManagedFiles struct{ root *os.Root }

func NewV5ManagedFiles(directory string) (*V5ManagedFiles, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	f := &V5ManagedFiles{root: r}
	for _, name := range []string{"packages", "prepared", "data"} {
		d, err := f.directory(name)
		if err != nil {
			r.Close()
			return nil, err
		}
		d.Close()
	}
	return f, nil
}
func (f *V5ManagedFiles) Close() error {
	if f == nil || f.root == nil {
		return nil
	}
	return f.root.Close()
}
func v5RandomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func v5OwnerValid(owner string) bool {
	if owner == "" {
		return true
	}
	n, e := strconv.ParseInt(owner, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == owner
}
func v5OwnerName(owner string) string {
	if owner == "" {
		return "system"
	}
	return "user-" + owner
}
func v5FamilyValid(p, k string) bool { return v5Name(p) && v5Name(k) }
func (f *V5ManagedFiles) directory(name string) (*os.Root, error) {
	if f == nil || f.root == nil || !v5PackagePath(name) {
		return nil, ErrV5PlatformUnavailable
	}
	parts := strings.Split(name, "/")
	prefix := ""
	for _, part := range parts {
		if prefix != "" {
			prefix += "/"
		}
		prefix += part
		if err := f.root.Mkdir(prefix, 0700); err != nil && !os.IsExist(err) {
			return nil, ErrV5PlatformUnavailable
		}
		info, err := f.root.Lstat(prefix)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrV5PlatformUnavailable
		}
	}
	info, err := f.root.Lstat(name)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	d, err := f.root.OpenRoot(name)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	actual, err := d.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		d.Close()
		return nil, ErrV5PlatformUnavailable
	}
	return d, nil
}
func v5ReadFile(r *os.Root, name string, max int64) ([]byte, error) {
	info, err := r.Lstat(name)
	if os.IsNotExist(err) {
		return nil, ErrV5NotFound
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > max {
		return nil, ErrV5PlatformUnavailable
	}
	f, err := r.Open(name)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, ErrV5PlatformUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, ErrV5PlatformUnavailable
	}
	return b, nil
}

// Linking a synced private temporary file publishes an entire immutable file
// atomically without replacing an existing entry, also across host processes.
func v5PublishFile(r *os.Root, name string, raw []byte) error {
	if b, err := v5ReadFile(r, name, int64(len(raw))); err == nil {
		if bytes.Equal(b, raw) {
			return v5SyncDirectory(r)
		}
		return ErrV5Conflict
	} else if err != ErrV5NotFound {
		return err
	}
	id := v5RandomID()
	if id == "" {
		return ErrV5PlatformUnavailable
	}
	tmp := ".pending-" + id
	f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	defer r.Remove(tmp)
	if n, err := f.Write(raw); err != nil || n != len(raw) {
		f.Close()
		return ErrV5PlatformUnavailable
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return ErrV5PlatformUnavailable
	}
	if f.Close() != nil {
		return ErrV5PlatformUnavailable
	}
	if err := r.Link(tmp, name); err != nil {
		if b, e := v5ReadFile(r, name, int64(len(raw))); e == nil && bytes.Equal(b, raw) {
			return v5SyncDirectory(r)
		}
		return ErrV5PlatformUnavailable
	}
	return v5SyncDirectory(r)
}
func v5SyncDirectory(r *os.Root) error {
	d, err := r.Open(".")
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (f *V5ManagedFiles) PutPackage(raw []byte, digest string) error {
	if !v5Digest(digest) || len(raw) > PluginV5MaxPackageBytes || v5BytesDigest(raw) != digest {
		return ErrPluginV5PackageDigest
	}
	d, err := f.directory("packages")
	if err != nil {
		return err
	}
	defer d.Close()
	return v5PublishFile(d, digest[7:]+".tar.gz", raw)
}
func (f *V5ManagedFiles) Package(digest string) ([]byte, error) {
	if !v5Digest(digest) {
		return nil, ErrV5Invalid
	}
	d, err := f.directory("packages")
	if err != nil {
		return nil, err
	}
	defer d.Close()
	b, err := v5ReadFile(d, digest[7:]+".tar.gz", PluginV5MaxPackageBytes)
	if err != nil {
		return nil, err
	}
	if v5BytesDigest(b) != digest {
		return nil, ErrPluginV5PackageDigest
	}
	return b, nil
}
func (f *V5ManagedFiles) RemovePackage(digest string) error {
	if !v5Digest(digest) {
		return ErrV5Invalid
	}
	d, err := f.directory("packages")
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Remove(digest[7:] + ".tar.gz"); err != nil && !os.IsNotExist(err) {
		return ErrV5PlatformUnavailable
	}
	return v5SyncDirectory(d)
}
func (f *V5ManagedFiles) Prepare(p, k, owner string, generation int64, raw []byte) (string, error) {
	if !v5FamilyValid(p, k) || !v5OwnerValid(owner) || generation < 1 || len(raw) > PluginV5ManifestMaxBytes {
		return "", ErrV5Invalid
	}
	d, err := f.directory("prepared/" + p + "/" + k + "/" + v5OwnerName(owner))
	if err != nil {
		return "", err
	}
	defer d.Close()
	if err := v5PublishFile(d, strconv.FormatInt(generation, 10)+".json", raw); err != nil {
		return "", err
	}
	return v5BytesDigest(raw), nil
}
func (f *V5ManagedFiles) VerifyPrepared(p, k, owner string, generation int64, digest string) error {
	if !v5FamilyValid(p, k) || !v5OwnerValid(owner) || generation < 1 || !v5Digest(digest) {
		return ErrV5Invalid
	}
	d, err := f.directory("prepared/" + p + "/" + k + "/" + v5OwnerName(owner))
	if err != nil {
		return err
	}
	defer d.Close()
	raw, err := v5ReadFile(d, strconv.FormatInt(generation, 10)+".json", PluginV5ManifestMaxBytes)
	if err != nil {
		return err
	}
	if v5BytesDigest(raw) != digest {
		return ErrPluginV5PackageDigest
	}
	return nil
}
func (f *V5ManagedFiles) PurgeOwner(p, k, owner string) error {
	if f == nil || f.root == nil || !v5FamilyValid(p, k) || !v5OwnerValid(owner) {
		return ErrV5Invalid
	}
	for _, prefix := range []string{"data/", "prepared/"} {
		parent, err := f.directory(prefix + p + "/" + k)
		if err != nil {
			return err
		}
		// Open the verified parent first; neither intermediate symlinks nor the
		// final owner entry may redirect removal into another owner namespace.
		if err := parent.RemoveAll(v5OwnerName(owner)); err != nil {
			parent.Close()
			return ErrV5PlatformUnavailable
		}
		err = v5SyncDirectory(parent)
		parent.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
