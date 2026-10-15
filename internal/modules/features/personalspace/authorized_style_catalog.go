package space

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

type AuthenticatedSpaceStyleCatalog interface {
	ListOwnSpaceSourceStylePacks(context.Context, identityport.SessionPrincipalResolver) (*SpaceSourceStylePackList, error)
}

// Explicit wire projection: no filesystem Path or private package RawFiles.
type SpaceSourceStylePack struct {
	Name        string                     `json:"name"`
	Target      string                     `json:"target,omitempty"`
	Version     string                     `json:"version,omitempty"`
	DisplayName string                     `json:"display_name,omitempty"`
	Description string                     `json:"description,omitempty"`
	Checksum    string                     `json:"checksum,omitempty"`
	Previews    *stylepack.PreviewImages   `json:"previews,omitempty"`
	Manifest    *stylepack.Manifest        `json:"manifest,omitempty"`
	Validation  stylepack.ValidationResult `json:"validation"`
}
type SpaceSourceStylePackList struct {
	Items []SpaceSourceStylePack `json:"items"`
}
type spaceSourceCatalogFacts struct {
	Owner        *spaceOwnerFacts
	User         *identitydomain.User
	Root         string
	ResolvedRoot string
	Exists       bool
	Items        []stylepack.SourcePackInfo
	Packages     map[string]*stylepack.Package
}

func (s *Service) loadOwnSpaceSourceCatalog(ctx context.Context, p identityport.PrincipalContext) (*spaceSourceCatalogFacts, error) {
	owner, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	r := spaceOwnerRequest(owner, p, "style_catalog", "space-source-catalog")
	d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
	if err != nil || d.Effect != "allow" {
		return nil, ErrSpaceOwnerDenied
	}
	user, err := s.users.GetByID(ctx, p.Actor.ID)
	if err != nil || user == nil || user.ID != p.Actor.ID || user.Status != identitydomain.UserStatusActive {
		return nil, ErrSpaceOwnerDenied
	}
	// User's credential-free domain projection has value fields only.
	detached := *user
	detached.CreatedAt, detached.UpdatedAt = detached.CreatedAt.UTC(), detached.UpdatedAt.UTC()
	root, err := filepath.Abs(stylepack.SourceRoot("personal-space"))
	if err != nil {
		return nil, ErrSpaceFactsUnavailable
	}
	f := &spaceSourceCatalogFacts{Owner: owner, User: &detached, Root: root}
	resolved, err := filepath.EvalSymlinks(root)
	if os.IsNotExist(err) {
		f.Items, f.Packages = []stylepack.SourcePackInfo{}, map[string]*stylepack.Package{}
	} else if err != nil {
		return nil, ErrSpaceFactsUnavailable
	} else {
		f.Exists, f.ResolvedRoot = true, resolved
		f.Items, f.Packages, err = stylepack.SnapshotSourcePacksStrict("personal-space")
		if err != nil {
			return nil, ErrSpaceFactsUnavailable
		}
		for i := range f.Items {
			ensureSourcePackInfoTarget(&f.Items[i], "personal-space")
			// Validators inspect maps of files; diagnostic order is not a fact.
			v := &f.Items[i].Validation
			sort.Strings(v.Errors)
			sort.Strings(v.Warnings)
			sort.Slice(v.Issues, func(a, b int) bool {
				x, y := v.Issues[a], v.Issues[b]
				return x.Code+"\x00"+x.File+"\x00"+x.Viewport+"\x00"+x.Message < y.Code+"\x00"+y.File+"\x00"+y.Viewport+"\x00"+y.Message
			})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Service) ListOwnSpaceSourceStylePacks(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceSourceStylePackList, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false, s.loadOwnSpaceSourceCatalog,
		func(ctx context.Context, p identityport.PrincipalContext, f *spaceSourceCatalogFacts) (*SpaceSourceStylePackList, error) {
			result := &SpaceSourceStylePackList{Items: make([]SpaceSourceStylePack, 0, len(f.Items))}
			for _, item := range f.Items {
				result.Items = append(result.Items, SpaceSourceStylePack{Name: item.Name, Target: item.Target, Version: item.Version, DisplayName: item.DisplayName, Description: item.Description, Checksum: item.Checksum, Previews: item.Previews, Manifest: item.Manifest, Validation: item.Validation})
			}
			return result, nil
		}, nil)
}
