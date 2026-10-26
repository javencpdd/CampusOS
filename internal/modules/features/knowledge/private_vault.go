package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

// PrivateVault is deliberately outside application data and database backups.
// Per-selection keys are never exported. A durable retirement marker precedes
// key deletion, so restoring an older database cannot resurrect withdrawn data.
type PrivateVault struct {
	path string
	mu   sync.Mutex
	scan *os.File
}

func NewPrivateVault(path string, excludedRoots ...string) (*PrivateVault, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrPrivateUnavailable
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil || actual != path {
		return nil, ErrPrivateUnavailable
	}
	for _, excluded := range excludedRoots {
		base, err := filepath.Abs(excluded)
		if evaluated, e := filepath.EvalSymlinks(base); e == nil {
			base = evaluated
		}
		if err != nil {
			return nil, ErrPrivateUnavailable
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, ErrPrivateUnavailable
		}
	}
	v := &PrivateVault{path: path}
	r, err := v.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	probe := privateID() + ".probe"
	f, err := r.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, ErrPrivateUnavailable
	}
	defer r.Remove(probe)
	if _, err = f.Write([]byte("private-vault-probe")); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return nil, ErrPrivateUnavailable
	}
	if err = syncPrivateDir(r); err != nil {
		return nil, ErrPrivateUnavailable
	}
	return v, nil
}
func privateID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return "kp_" + hex.EncodeToString(b[:])
}
func validPrivateID(id string) bool { return opaque("kp_", id) }
func (v *PrivateVault) root() (*os.Root, error) {
	if v == nil {
		return nil, ErrPrivateUnavailable
	}
	s, e := os.Lstat(v.path)
	if e != nil || !s.IsDir() || s.Mode().Perm() != 0700 || !privateVaultOwned(s) {
		return nil, ErrPrivateUnavailable
	}
	r, e := os.OpenRoot(v.path)
	if e != nil {
		return nil, ErrPrivateUnavailable
	}
	return r, nil
}
func syncPrivateDir(r *os.Root) error {
	f, e := r.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func retiredPrivate(r *os.Root, id string) bool {
	_, e := r.Lstat(id + ".retired")
	return !errors.Is(e, os.ErrNotExist)
}
func (v *PrivateVault) ring(r *os.Root, id string) (*security.Keyring, error) {
	if !validPrivateID(id) || retiredPrivate(r, id) {
		return nil, ErrPrivateUnavailable
	}
	s, e := r.Lstat(id + ".key")
	if e != nil || !s.Mode().IsRegular() || s.Mode().Perm() != 0600 || s.Size() != 32 || !privateVaultOwned(s) {
		return nil, ErrPrivateUnavailable
	}
	f, e := r.Open(id + ".key")
	if e != nil {
		return nil, ErrPrivateUnavailable
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 33))
	defer clear(b)
	if e != nil || len(b) != 32 {
		return nil, ErrPrivateUnavailable
	}
	ring, e := security.NewKeyring("private-v1", map[string][]byte{"private-v1": b})
	if e != nil {
		return nil, ErrPrivateUnavailable
	}
	return ring, nil
}
func (v *PrivateVault) Available() bool {
	r, e := v.root()
	if e != nil {
		return false
	}
	r.Close()
	return true
}
func (v *PrivateVault) SealNew(id string, plain, aad []byte) (security.Envelope, error) {
	if v == nil || !validPrivateID(id) {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r, e := v.root()
	if e != nil {
		return security.Envelope{}, e
	}
	defer r.Close()
	if retiredPrivate(r, id) {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	key := make([]byte, 32)
	defer clear(key)
	if _, e = rand.Read(key); e != nil {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	f, e := r.OpenFile(id+".key", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	_, e = f.Write(key)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil || syncPrivateDir(r) != nil {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	ring, e := v.ring(r, id)
	if e != nil {
		return security.Envelope{}, e
	}
	out, e := ring.Seal(plain, aad)
	if e != nil || retiredPrivate(r, id) {
		return security.Envelope{}, ErrPrivateUnavailable
	}
	return out, nil
}
func (v *PrivateVault) Open(id string, envelope security.Envelope, aad []byte) ([]byte, error) {
	if v == nil {
		return nil, ErrPrivateUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r, e := v.root()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	ring, e := v.ring(r, id)
	if e != nil {
		return nil, e
	}
	plain, e := ring.Open(envelope, aad)
	if e != nil || retiredPrivate(r, id) {
		clear(plain)
		return nil, ErrPrivateUnavailable
	}
	return plain, nil
}
func (v *PrivateVault) Has(id string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r, e := v.root()
	if e != nil {
		return false
	}
	defer r.Close()
	_, e = v.ring(r, id)
	return e == nil
}
func (v *PrivateVault) Retire(id string) error {
	if v == nil || !validPrivateID(id) {
		return ErrPrivateUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	r, e := v.root()
	if e != nil {
		return e
	}
	defer r.Close()
	f, e := r.OpenFile(id+".retired", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e == nil {
		e = f.Sync()
		ce := f.Close()
		if e != nil || ce != nil {
			return ErrPrivateUnavailable
		}
	} else if !errors.Is(e, os.ErrExist) {
		return ErrPrivateUnavailable
	}
	if syncPrivateDir(r) != nil {
		return ErrPrivateUnavailable
	}
	if e = r.Remove(id + ".key"); e != nil && !errors.Is(e, os.ErrNotExist) {
		return ErrPrivateUnavailable
	}
	if syncPrivateDir(r) != nil {
		return ErrPrivateUnavailable
	}
	return nil
}

// PruneOrphans visits at most64 entries. A one-hour grace protects an in-flight
// staged selection; Select has a five-second deadline. Crash leftovers cannot
// expose content and eventually lose their unreferenced key as well.
func (v *PrivateVault) PruneOrphans(exists func(string) (bool, error)) error {
	if v == nil {
		return ErrPrivateUnavailable
	}
	v.mu.Lock()
	r, e := v.root()
	if e != nil {
		v.mu.Unlock()
		return e
	}
	if v.scan == nil {
		v.scan, e = r.Open(".")
	}
	if e != nil {
		r.Close()
		v.mu.Unlock()
		return e
	}
	entries, readErr := v.scan.ReadDir(64)
	if readErr != nil {
		v.scan.Close()
		v.scan = nil
	}
	ids := []string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".key") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".key")
		stat, e := r.Lstat(entry.Name())
		if e == nil && validPrivateID(id) && time.Since(stat.ModTime()) > time.Hour {
			ids = append(ids, id)
		}
	}
	r.Close()
	v.mu.Unlock()
	for _, id := range ids {
		found, e := exists(id)
		if e != nil {
			return e
		}
		if !found {
			if e = v.Retire(id); e != nil {
				return e
			}
		}
	}
	return nil
}
func (v *PrivateVault) closeScan() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.scan != nil {
		v.scan.Close()
		v.scan = nil
	}
}
