package space

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

type AuthenticatedSpaceStyleVariants interface {
	ApplyOwnSpaceCustomHTML(context.Context, string, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
	ApplyOwnSpaceStylePackZip(context.Context, io.ReaderAt, int64, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
	ApplyOwnSpaceSourceStylePack(context.Context, string, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
}

func (s *Service) ApplyOwnSpaceCustomHTML(ctx context.Context, html string, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_html", StylePackage{}, resolve, func(ctx context.Context, facts *spaceStyleFacts) (StylePackage, func(context.Context) error, error) {
		if err := ctx.Err(); err != nil {
			return StylePackage{}, nil, err
		}
		if !ValidateCustomHTMLSnippet(html).Valid {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		return BuildStyleHTMLPackage(facts.Owner.Owner, facts.Owner.Space, html), nil, nil
	})
}

func (s *Service) ApplyOwnSpaceSourceStylePack(ctx context.Context, name string, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_source", StylePackage{}, resolve, func(ctx context.Context, facts *spaceStyleFacts) (StylePackage, func(context.Context) error, error) {
		if err := ctx.Err(); err != nil {
			return StylePackage{}, nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" || slugStyleName(name) != name {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		root, err := filepath.Abs(stylepack.SourceDir("personal-space", name))
		if err != nil {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		pack, validation := stylepack.LoadDirStrict(root)
		if !validation.Valid || !ensureStylePackTarget(pack, "personal-space").Valid {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		check := func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			currentRoot, err := filepath.Abs(stylepack.SourceDir("personal-space", name))
			if err != nil || root != currentRoot {
				return identityport.ErrPolicyFactsChanged
			}
			current, validation := stylepack.LoadDirStrict(root)
			if !validation.Valid || !reflect.DeepEqual(pack, current) {
				return identityport.ErrPolicyFactsChanged
			}
			return ctx.Err()
		}
		return BuildStylePackPackage(facts.Owner.Owner, facts.Owner.Space, pack), check, nil
	})
}

func (s *Service) ApplyOwnSpaceStylePackZip(ctx context.Context, reader io.ReaderAt, size int64, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_zip", StylePackage{}, resolve, func(ctx context.Context, facts *spaceStyleFacts) (StylePackage, func(context.Context) error, error) {
		if err := ctx.Err(); err != nil {
			return StylePackage{}, nil, err
		}
		if corestorage.NilImageAdapter(reader) || size < 1 {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		if size > stylepack.MaxPackageBytes {
			return StylePackage{}, nil, ErrSpaceFileTooLarge
		}
		data, err := io.ReadAll(io.NewSectionReader(reader, 0, size))
		if err != nil || int64(len(data)) != size {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		if err := ctx.Err(); err != nil {
			return StylePackage{}, nil, err
		}
		pack, validation := stylepack.LoadZipStrict(bytes.NewReader(data), int64(len(data)))
		if !validation.Valid || !ensureStylePackTarget(pack, "personal-space").Valid {
			return StylePackage{}, nil, ErrInvalidStyleExport
		}
		return BuildStylePackPackage(facts.Owner.Owner, facts.Owner.Space, pack), nil, nil
	})
}
