package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrInlineImageCleanupPending = errors.New("inline image cleanup is pending")
var ErrInlineImageDenied = errors.New("inline image upload denied")

// InlineImageFile is a built-in filesystem primitive, never authentication.
// It retains its own private receipt and cannot be constructed from an HTTP DTO.
type InlineImageFile struct {
	image    OptimizedImage
	metadata ContentImage
	path     string
	info     os.FileInfo
	digest   [32]byte
}

func PrepareInlineImage(ctx context.Context, reader io.Reader, maxBytes int64) (*InlineImageFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reader == nil || maxBytes < 1 {
		return nil, ErrImageUnsupported
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrImageTooLarge
	}
	image, err := OptimizeImage(data)
	if err != nil {
		return nil, err
	}
	if int64(len(image.Data)) > maxBytes {
		return nil, ErrImageTooLarge
	}
	return &InlineImageFile{image: image, digest: sha256.Sum256(image.Data)}, nil
}
func (f *InlineImageFile) Size() int64            { return int64(len(f.image.Data)) }
func (f *InlineImageFile) Metadata() ContentImage { return f.metadata }

func (f *InlineImageFile) Write(ctx context.Context, storage Port, owner, namespace, prefix string) error {
	if namespace != "content" && namespace != "richtext" {
		return ErrUnsafePath
	}
	return f.write(ctx, storage, owner, namespace, prefix, false)
}

// WriteAvatarForCommand is the fixed legacy avatar layout, never a caller path.
func (f *InlineImageFile) WriteAvatarForCommand(ctx context.Context, storage Port, owner, prefix string) error {
	return f.write(ctx, storage, owner, "avatars", prefix, true)
}

func (f *InlineImageFile) write(ctx context.Context, storage Port, owner, namespace, prefix string, avatar bool) error {
	if f == nil || f.path != "" || NilImageAdapter(storage) || !SafeSegment(owner) || !transaction.Active(ctx) {
		return ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := storage.EnsureLayout(owner); err != nil {
		return err
	}
	dir, err := storage.Path(owner, ImageDir, namespace)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name, err := randomImageName(f.image.Extension)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.path = path
	f.info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return errors.Join(ErrInlineImageCleanupPending, err)
	}
	_, writeErr := io.Copy(file, bytes.NewReader(f.image.Data))
	info, statErr := file.Stat()
	closeErr := file.Close()
	if writeErr != nil || statErr != nil || closeErr != nil {
		return errors.Join(writeErr, statErr, closeErr)
	}
	f.info = info
	f.metadata = ContentImage{FileURL: prefix + "/" + owner + "/" + name, FileName: name, FileSize: f.Size(), MimeType: f.image.MimeType, Width: f.image.Width, Height: f.image.Height, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if avatar {
		f.metadata.FileURL = prefix + "/" + owner + "/avatars/" + name
	}
	return f.Check(ctx)
}
func (f *InlineImageFile) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f == nil || f.path == "" || f.info == nil || f.metadata.FileSize != f.Size() {
		return identityport.ErrPolicyFactsChanged
	}
	current, err := os.Lstat(f.path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(current, f.info) || current.Size() != f.Size() || !current.ModTime().Equal(f.info.ModTime()) {
		return identityport.ErrPolicyFactsChanged
	}
	image, err := ReadImageBytes(f.path, f.Size())
	if err != nil || sha256.Sum256(image.Data) != f.digest || image.MimeType != f.image.MimeType {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	return ctx.Err()
}

// Discard removes only the inode made by Write. Changed/replaced files are left
// in place and reported as pending rather than deleting somebody else's bytes.
func (f *InlineImageFile) Discard() error {
	if f == nil || f.path == "" {
		return nil
	}
	current, err := os.Lstat(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || f.info == nil || !current.Mode().IsRegular() || !os.SameFile(current, f.info) {
		return errors.Join(ErrInlineImageCleanupPending, err)
	}
	if f.metadata.FileName != "" {
		if err := f.Check(context.Background()); err != nil {
			return errors.Join(ErrInlineImageCleanupPending, err)
		}
	}
	if err := os.Remove(f.path); err != nil {
		return errors.Join(ErrInlineImageCleanupPending, err)
	}
	return nil
}
