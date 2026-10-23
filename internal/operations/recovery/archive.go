// Package recovery implements trusted, local, offline maintenance. It exposes
// no HTTP or plugin capability and never treats a checksum as authentication.
package recovery

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var ErrArchive = errors.New("recovery.invalid_archive")

type Limits struct {
	Bytes   int64
	Entries int
}

var DefaultLimits = Limits{Bytes: 16 << 30, Entries: 250000}

type Archive struct{ Directory, Digest, Format string }

func (a *Archive) Close() {
	if a != nil {
		_ = os.RemoveAll(a.Directory)
	}
}

func archiveName(raw string, directory bool) (string, error) {
	n := strings.TrimPrefix(raw, "./")
	if directory {
		n = strings.TrimSuffix(n, "/")
	}
	if directory && (n == "" || n == ".") {
		return ".", nil
	}
	if n == "" || strings.ContainsAny(n, "\\:\x00\r\n") || strings.HasPrefix(n, "/") || path.Clean(n) != n || n == ".." || strings.HasPrefix(n, "../") {
		return "", ErrArchive
	}
	return n, nil
}

// unpack permits only regular files/directories and writes them itself. It
// never delegates untrusted link handling or ownership/modes to tar(1).
func unpack(input io.Reader, output string, limits Limits, outer bool) error {
	if limits.Bytes <= 0 || limits.Entries <= 0 {
		return ErrArchive
	}
	buffered := bufio.NewReader(input)
	gz, err := gzip.NewReader(buffered)
	if err != nil {
		return ErrArchive
	}
	defer gz.Close()
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	root, err := os.OpenRoot(output)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil || count >= limits.Entries {
			return ErrArchive
		}
		directory := h.Typeflag == tar.TypeDir
		if !directory && h.Typeflag != tar.TypeReg {
			return ErrArchive
		}
		name, e := archiveName(h.Name, directory)
		if e != nil || seen[name] || h.Size < 0 || h.Size > limits.Bytes-total {
			return ErrArchive
		}
		seen[name] = true
		total += h.Size
		if outer {
			if directory || (name != "database.sql" && name != "files.tar.gz" && name != "metadata.txt" && name != "SHA256SUMS") {
				return ErrArchive
			}
			if (name == "metadata.txt" || name == "SHA256SUMS") && h.Size > 65536 {
				return ErrArchive
			}
		} else if name != "." {
			allowed := name == "migrations" || strings.HasPrefix(name, "migrations/") || name == "modules" || strings.HasPrefix(name, "modules/") || name == "data"
			for _, part := range []string{"plugins", "plugin_data", "module_data", "resources", "personal-space", "images", "config", "skills"} {
				base := "data/" + part
				allowed = allowed || name == base || strings.HasPrefix(name, base+"/")
			}
			if !allowed {
				return ErrArchive
			}
		}
		if name == "." {
			continue
		}
		if directory {
			if e = root.MkdirAll(name, 0700); e != nil {
				return ErrArchive
			}
			continue
		}
		if e = root.MkdirAll(path.Dir(name), 0700); e != nil {
			return ErrArchive
		}
		f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return ErrArchive
		}
		written, e := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if e != nil || closeErr != nil || written != h.Size {
			return ErrArchive
		}
	}
	// GNU tar may add zero padding. Reject appended archives, nonzero trailing
	// payload and extra gzip members; bound even this otherwise ignored tail.
	tail, err := io.ReadAll(io.LimitReader(gz, 10241))
	if err != nil || len(tail) > 10240 {
		return ErrArchive
	}
	for _, b := range tail {
		if b != 0 {
			return ErrArchive
		}
	}
	if _, err = buffered.Peek(1); err != io.EOF {
		return ErrArchive
	}
	if outer && len(seen) != 4 {
		return ErrArchive
	}
	return nil
}

func hashFile(name string) (string, error) {
	f, e := os.Open(name)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Prepare validates both layers and the exact checksum manifest in private
// staging. The caller must Close even when it only wants verification.
func Prepare(filename string, limits Limits) (_ *Archive, result error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, ErrArchive
	}
	defer f.Close()
	dir, err := os.MkdirTemp("", "campusos-recovery-")
	if err != nil {
		return nil, err
	}
	a := &Archive{Directory: dir}
	defer func() {
		if result != nil {
			a.Close()
		}
	}()
	h := sha256.New()
	if err = unpack(io.TeeReader(f, h), dir, limits, true); err != nil {
		return nil, err
	}
	a.Digest = hex.EncodeToString(h.Sum(nil))
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, ErrArchive
	}
	names := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return nil, ErrArchive
		}
		name := line[66:]
		if names[name] || (name != "database.sql" && name != "files.tar.gz" && name != "metadata.txt") {
			return nil, ErrArchive
		}
		names[name] = true
		actual, e := hashFile(filepath.Join(dir, name))
		if e != nil || line[:64] != actual {
			return nil, ErrArchive
		}
	}
	if len(names) != 3 {
		return nil, ErrArchive
	}
	meta, err := os.ReadFile(filepath.Join(dir, "metadata.txt"))
	if err != nil {
		return nil, ErrArchive
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(meta), "\n"), "\n") {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || kv[0] == "" {
			return nil, ErrArchive
		}
		if _, ok := fields[kv[0]]; ok {
			return nil, ErrArchive
		}
		fields[kv[0]] = kv[1]
	}
	a.Format = fields["format"]
	// Legacy data may include .env. It must be migrated under a separate,
	// trusted procedure; never quietly reinstate credential export/import.
	if a.Format != "campusos-single-node-v3" || fields["root_dotenv"] != "excluded" || fields["deployment_keys"] != "separate_custody_required" {
		return nil, ErrArchive
	}
	files := filepath.Join(dir, "files")
	if err = os.Mkdir(files, 0700); err != nil {
		return nil, err
	}
	inner, err := os.Open(filepath.Join(dir, "files.tar.gz"))
	if err != nil {
		return nil, ErrArchive
	}
	defer inner.Close()
	if err = unpack(inner, files, limits, false); err != nil {
		return nil, err
	}
	return a, nil
}

// Extract publishes a fully checked tree only to an absent or empty private
// destination. Existing nonempty destinations and links fail closed.
func (a *Archive) Extract(destination string) error {
	dest, err := filepath.Abs(destination)
	if err != nil || dest == string(filepath.Separator) {
		return ErrArchive
	}
	if info, e := os.Lstat(dest); e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrArchive
		}
		entries, e := os.ReadDir(dest)
		if e != nil || len(entries) != 0 {
			return ErrArchive
		}
	} else if !os.IsNotExist(e) {
		return ErrArchive
	}
	parent := filepath.Dir(dest)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".campusos-extract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = copyTree(a.Directory, stage); err != nil {
		return err
	}
	// os.Rename may replace an empty directory on Linux. On other systems the
	// empty target is removed only after complete staging, never live content.
	if err = os.Rename(stage, dest); err != nil {
		return fmt.Errorf("recovery.extract_publish_failed: %w", err)
	}
	return nil
}

func copyTree(source, destination string) error {
	var directories []string
	err := filepath.WalkDir(source, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if d.IsDir() {
			directories = append(directories, target)
			return os.MkdirAll(target, 0700)
		}
		if !d.Type().IsRegular() {
			return ErrArchive
		}
		in, err := os.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		d, e := os.Open(directories[i])
		if e != nil {
			return e
		}
		e = d.Sync()
		closeErr := d.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
