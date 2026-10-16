package space

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

type AuthenticatedSpaceStyleReads interface {
	ValidateOwnSpaceStylePackage(context.Context, StylePackage, identityport.SessionPrincipalResolver) (*StyleValidationResult, error)
	PreviewOwnSpaceStylePackage(context.Context, StylePackage, identityport.SessionPrincipalResolver) (*SpaceStylePreview, error)
	ExportOwnSpaceStylePackage(context.Context, StyleExportRequest, identityport.SessionPrincipalResolver) (*StyleExportResult, error)
	ValidateOwnSpaceCustomHTML(context.Context, string, identityport.SessionPrincipalResolver) (*StyleValidationResult, error)
	OwnSpaceCustomHTMLExample(context.Context, identityport.SessionPrincipalResolver) (*StyleHTMLExampleResult, error)
	ValidateOwnSpaceStylePackZip(context.Context, io.ReaderAt, int64, identityport.SessionPrincipalResolver) (*StylePackResult, error)
	OwnSpaceStylePackExample(context.Context, identityport.SessionPrincipalResolver) (*stylepack.FileBundle, error)
	OwnSpaceStylePackExampleZip(context.Context, identityport.SessionPrincipalResolver) (*StylePackZipExample, error)
}

type StylePackZipExample struct {
	Filename string
	Data     []byte
}

// Preview renders the checked request package. Raw owner/Space operational
// records are private comparison facts and are not part of this response.
type SpaceStylePreview struct {
	Validation StyleValidationResult `json:"validation"`
	Manifest   *StyleManifest        `json:"manifest,omitempty"`
	Layout     string                `json:"layout,omitempty"`
	Components []StyleComponent      `json:"components,omitempty"`
	Tokens     map[string]string     `json:"tokens,omitempty"`
	Assets     []StyleAsset          `json:"assets,omitempty"`
}

type spaceStyleReadFacts struct {
	Owner *spaceOwnerFacts
	User  identitydomain.User
}

func (s *Service) loadOwnSpaceStyleRead(ctx context.Context, p identityport.PrincipalContext) (*spaceStyleReadFacts, error) {
	owner, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	r := spaceOwnerRequest(owner, p, "style_read", "space-style-read")
	d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
	if err != nil || d.Effect != "allow" {
		return nil, ErrSpaceOwnerDenied
	}
	user, err := s.users.GetByID(ctx, p.Actor.ID)
	if err != nil || user == nil || user.ID != p.Actor.ID || user.Status != identitydomain.UserStatusActive {
		return nil, ErrSpaceOwnerDenied
	}
	u := *user
	u.CreatedAt, u.UpdatedAt = u.CreatedAt.UTC(), u.UpdatedAt.UTC()
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	return &spaceStyleReadFacts{Owner: owner, User: u}, nil
}

// The owning module supplies this callback. No DTO can select an operation or
// pass a Policy/fact builder. Results are fully buffered before final checks.
func readOwnSpaceStyle[T any](ctx context.Context, s *Service, resolve identityport.SessionPrincipalResolver, project func(context.Context, *spaceStyleReadFacts) (T, error)) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrSpaceFactsUnavailable
	}
	return identityport.ReadBoundProjection(ctx, resolve, false, s.loadOwnSpaceStyleRead, func(ctx context.Context, p identityport.PrincipalContext, f *spaceStyleReadFacts) (T, error) {
		return project(ctx, f)
	}, nil)
}

func copyStyleReadPackage(pkg StylePackage) (StylePackage, error) {
	raw, err := json.Marshal(pkg)
	var detached StylePackage
	if err != nil || json.Unmarshal(raw, &detached) != nil {
		return StylePackage{}, ErrInvalidStyleExport
	}
	return detached, nil
}

func (s *Service) ValidateOwnSpaceStylePackage(ctx context.Context, pkg StylePackage, resolve identityport.SessionPrincipalResolver) (*StyleValidationResult, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StyleValidationResult, error) {
		detached, err := copyStyleReadPackage(pkg)
		if err != nil {
			return nil, err
		}
		result := ValidateStylePackage(detached)
		return &result, nil
	})
}

func (s *Service) PreviewOwnSpaceStylePackage(ctx context.Context, pkg StylePackage, resolve identityport.SessionPrincipalResolver) (*SpaceStylePreview, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*SpaceStylePreview, error) {
		detached, err := copyStyleReadPackage(pkg)
		if err != nil {
			return nil, err
		}
		result := BuildStylePreview(f.Owner.Owner, f.Owner.Space, detached)
		return &SpaceStylePreview{Validation: result.Validation, Manifest: result.Manifest, Layout: result.Layout, Components: result.Components, Tokens: result.Tokens, Assets: result.Assets}, nil
	})
}

func (s *Service) ExportOwnSpaceStylePackage(ctx context.Context, req StyleExportRequest, resolve identityport.SessionPrincipalResolver) (*StyleExportResult, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StyleExportResult, error) {
		result := BuildStyleExport(f.Owner.Owner, f.Owner.Space, req)
		if !result.Validation.Valid {
			return nil, ErrInvalidStyleExport
		}
		return &result, nil
	})
}

func (s *Service) ValidateOwnSpaceCustomHTML(ctx context.Context, html string, resolve identityport.SessionPrincipalResolver) (*StyleValidationResult, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StyleValidationResult, error) {
		result := ValidateCustomHTMLSnippet(html)
		return &result, nil
	})
}

func (s *Service) OwnSpaceCustomHTMLExample(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*StyleHTMLExampleResult, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StyleHTMLExampleResult, error) {
		result := BuildStyleHTMLExample(f.Owner.Owner, f.Owner.Space)
		return &result, nil
	})
}

func (s *Service) ValidateOwnSpaceStylePackZip(ctx context.Context, reader io.ReaderAt, size int64, resolve identityport.SessionPrincipalResolver) (*StylePackResult, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StylePackResult, error) {
		if corestorage.NilImageAdapter(reader) || size < 1 {
			return nil, ErrInvalidStyleExport
		}
		if size > stylepack.MaxPackageBytes {
			return nil, ErrSpaceFileTooLarge
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.NewSectionReader(reader, 0, size))
		if err != nil || int64(len(data)) != size {
			return nil, ErrInvalidStyleExport
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pack, validation := stylepack.LoadZipStrict(bytes.NewReader(data), int64(len(data)))
		if validation.Valid && !ensureStylePackTarget(pack, "personal-space").Valid {
			validation = ensureStylePackTarget(pack, "personal-space")
		}
		return &StylePackResult{Validation: validation, Package: pack}, nil
	})
}

func (s *Service) OwnSpaceStylePackExample(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*stylepack.FileBundle, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*stylepack.FileBundle, error) {
		result := BuildStylePackExample(f.Owner.Owner, f.Owner.Space)
		return &result, nil
	})
}

func (s *Service) OwnSpaceStylePackExampleZip(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*StylePackZipExample, error) {
	return readOwnSpaceStyle(ctx, s, resolve, func(ctx context.Context, f *spaceStyleReadFacts) (*StylePackZipExample, error) {
		example := BuildStylePackExample(f.Owner.Owner, f.Owner.Space)
		data, err := stylepack.ZipBundle(example)
		if err != nil {
			return nil, ErrInvalidStyleExport
		}
		return &StylePackZipExample{Filename: example.Filename + ".zip", Data: data}, nil
	})
}
