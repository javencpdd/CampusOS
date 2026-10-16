package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	editor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// ImageBytes is a bounded, immutable response snapshot, never a physical path.
type ImageBytes struct {
	Data       []byte
	MimeType   string
	ModifiedAt time.Time
}

func NilImageAdapter(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// ReadImageBytes is only a filesystem primitive. Its owning business caller
// must authorize before calling and recheck its facts before returning bytes.
func ReadImageBytes(path string, maxBytes int64) (ImageBytes, error) {
	if maxBytes < 1 {
		return ImageBytes{}, ErrImageNotFound
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maxBytes {
		return ImageBytes{}, ErrImageNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		return ImageBytes{}, ErrImageNotFound
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return ImageBytes{}, ErrImageNotFound
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	after, statErr := f.Stat()
	current, currentErr := os.Lstat(path)
	if err != nil || statErr != nil || currentErr != nil || !os.SameFile(after, current) ||
		!current.Mode().IsRegular() || int64(len(data)) != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ImageBytes{}, identityport.ErrPolicyFactsChanged
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return ImageBytes{}, ErrImageUnsupported
	}
	return ImageBytes{Data: data, MimeType: mime, ModifiedAt: after.ModTime().UTC()}, nil
}

type imageSnapshot struct {
	ThreadID, Owner, Category, Body string
	Version                         int64
	Publication                     domain.PublicationStatus
	Moderation                      domain.ModerationStatus
	Deletion                        domain.DeletionStatus
	Image                           ImageBytes
}

func (s *ContentImageStore) ReadForPrincipal(ctx context.Context, owner, name, threadID string, query communityport.AuthenticatedThreadQuery, resolve identityport.SessionPrincipalResolver) (ImageBytes, error) {
	if s == nil || !SafeSegment(owner) || !SafeSegment(name) {
		return ImageBytes{}, ErrImageNotFound
	}
	url := ContentImageURLPrefix + "/" + owner + "/" + name
	return identityport.ReadBoundProjection(ctx, resolve, true,
		func(ctx context.Context, p identityport.PrincipalContext) (imageSnapshot, error) {
			snapshot := imageSnapshot{Owner: owner}
			if threadID == "" {
				// Unbound uploaded media is an independent owner inventory. It is
				// never published merely because its filename is known.
				if p.Actor.Kind != "user" || p.Actor.ID != owner {
					return imageSnapshot{}, ErrImageNotFound
				}
			} else {
				if NilImageAdapter(query) {
					return imageSnapshot{}, identityport.ErrPolicyDecisionMismatch
				}
				t, err := query.GetThreadForPrincipal(ctx, threadID, identityport.SessionPrincipalResolverFromContext(ctx))
				if err != nil {
					if errors.Is(err, communityport.ErrThreadNotFound) {
						return imageSnapshot{}, ErrImageNotFound
					}
					return imageSnapshot{}, err
				}
				if t == nil || t.ID != threadID || t.AuthorID != owner || t.FactsVersion < 1 || t.DeletionStatus != domain.DeletionStatusActive ||
					t.ContentFormat != editor.FormatSafeHTML || !editor.ReferencesImage(t.Content, url) {
					return imageSnapshot{}, ErrImageNotFound
				}
				snapshot.ThreadID, snapshot.Category, snapshot.Body, snapshot.Version = t.ID, t.CategoryID, t.Content, t.FactsVersion
				snapshot.Publication, snapshot.Moderation, snapshot.Deletion = t.PublicationStatus, t.ModerationStatus, t.DeletionStatus
			}
			path, err := s.Path(owner, name)
			if err != nil {
				return imageSnapshot{}, ErrImageNotFound
			}
			snapshot.Image, err = ReadImageBytes(path, s.MaxBytes())
			return snapshot, err
		}, func(_ context.Context, _ identityport.PrincipalContext, snap imageSnapshot) (ImageBytes, error) {
			image := snap.Image
			image.Data = append([]byte(nil), image.Data...)
			return image, nil
		}, nil)
}

func (s *ContentImageStore) ListForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) ([]ContentImage, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(_ context.Context, p identityport.PrincipalContext) ([]ContentImage, error) {
			return s.ListOwned(p.Actor.ID)
		},
		func(_ context.Context, _ identityport.PrincipalContext, images []ContentImage) ([]ContentImage, error) {
			return append([]ContentImage{}, images...), nil
		}, nil)
}
