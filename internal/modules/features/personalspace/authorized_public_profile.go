package space

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

// Public appearance is an explicit wire projection. Operational filters, error
// text, governance attribution and the complete private snapshot never cross it.
type SpacePublicAppearance struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	Title         string         `json:"title"`
	Bio           string         `json:"bio"`
	Avatar        string         `json:"avatar,omitempty"`
	CoverImage    string         `json:"cover_image,omitempty"`
	Theme         string         `json:"theme"`
	Layout        string         `json:"layout"`
	StyleName     string         `json:"style_name,omitempty"`
	StyleVersion  string         `json:"style_version,omitempty"`
	StyleManifest *StyleManifest `json:"style_manifest,omitempty"`
	Visibility    Visibility     `json:"visibility"`
	SyncEnabled   bool           `json:"sync_enabled"`
	IsDefault     bool           `json:"is_default"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
type SpacePublicProfile struct {
	Owner Owner                 `json:"owner"`
	Space SpacePublicAppearance `json:"space"`
}

func projectPublicSpace(f *spaceAdminFacts) *SpacePublicProfile {
	value := buildPublicSpace(identityUser(f.User), f.Space.Space)
	s := value.Space
	return &SpacePublicProfile{Owner: value.Owner, Space: SpacePublicAppearance{ID: s.ID, UserID: s.UserID, Title: s.Title, Bio: s.Bio, Avatar: s.Avatar, CoverImage: s.CoverImage, Theme: s.Theme, Layout: s.Layout, StyleName: s.StyleName, StyleVersion: s.StyleVersion, StyleManifest: cloneManifest(s.StyleManifest), Visibility: s.Visibility, SyncEnabled: s.SyncEnabled, IsDefault: s.IsDefault, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}}
}
func validateCurrentSpaceKey(key string, byName bool) (string, error) {
	if byName {
		if key == "" || strings.TrimSpace(key) != key || len([]rune(key)) > 64 {
			return "", ErrSpaceOwnerInvalid
		}
		return key, nil
	}
	n, err := strconv.ParseInt(key, 10, 64)
	if err != nil || n < 1 {
		return "", ErrSpaceOwnerInvalid
	}
	return strconv.FormatInt(n, 10), nil
}
func (s *Service) loadCurrentReadSpace(ctx context.Context, key string, byName, public bool, operation string, p identityport.PrincipalContext) (*spaceAdminFacts, error) {
	if s == nil || corestorage.NilImageAdapter(s.users) {
		return nil, ErrSpaceFactsUnavailable
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	owner := p.Actor.ID
	var expected identityport.User
	lookup := func() (identityport.User, error) {
		var user *identitydomain.User
		var err error
		if byName {
			user, err = s.users.GetByUsername(ctx, key)
		} else {
			user, err = s.users.GetByID(ctx, key)
		}
		if errors.Is(err, identityport.ErrUserNotFound) {
			return identityport.User{}, ErrSpaceNotFound
		}
		if err != nil || user == nil {
			return identityport.User{}, ErrSpaceFactsUnavailable
		}
		return spaceAdminUser(user), nil
	}
	if public {
		var err error
		expected, err = lookup()
		if err != nil {
			return nil, err
		}
		if byName && !strings.EqualFold(expected.Username, key) {
			return nil, identityport.ErrPolicyFactsChanged
		}
		owner = expected.ID
	}
	f, err := s.loadAdminSpaceFacts(ctx, owner, false)
	if err != nil {
		return nil, err
	}
	// The identity projection used for appearance must match the complete current
	// reader, including changes that an adapter could make during the Space read.
	expectedOwner := Owner{ID: f.User.ID, Username: f.User.Username, Nickname: f.User.Nickname, Avatar: f.User.Avatar, Bio: f.User.Bio}
	if !reflect.DeepEqual(expectedOwner, f.Space.Owner) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if public {
		final, err := lookup()
		if err != nil {
			return nil, err
		}
		if expected != f.User || final != expected {
			return nil, identityport.ErrPolicyFactsChanged
		}
		r := identityport.PersonalSpacePublicRequest{RequestID: "space-current-public-read", PolicyVersion: identityport.PersonalSpacePublicPolicyVersion, Operation: operation, OwnerID: owner, Visibility: string(f.Space.Space.Visibility), UserStatus: f.User.Status, Principal: p, Exists: f.Space.Raw != nil, Disabled: f.Space.Space.DisabledAt != nil, FactsVersion: f.Space.Space.FactsVersion}
		d, err := (identitypolicy.PersonalSpacePublicPolicy{}).DecidePersonalSpacePublic(r)
		if err != nil {
			return nil, ErrSpaceFactsUnavailable
		}
		if identityport.CheckPersonalSpacePublicDecision(r, d, r) != nil {
			return nil, ErrSpaceNotPublic
		}
	} else {
		if f.User.Status != "active" {
			return nil, ErrSpaceOwnerDenied
		}
		r := spaceOwnerRequest(f.Space, p, operation, "space-current-owner-read")
		d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
		if err != nil || d.Effect != "allow" {
			return nil, ErrSpaceOwnerDenied
		}
	}
	return f, nil
}
func (s *Service) currentPublicProfile(ctx context.Context, key string, byName bool) (*SpacePublicProfile, error) {
	key, err := validateCurrentSpaceKey(key, byName)
	if err != nil {
		return nil, err
	}
	return identityport.ReadBoundProjection(ctx, nil, true, func(ctx context.Context, p identityport.PrincipalContext) (*spaceAdminFacts, error) {
		return s.loadCurrentReadSpace(ctx, key, byName, true, "profile", p)
	}, func(_ context.Context, _ identityport.PrincipalContext, f *spaceAdminFacts) (*SpacePublicProfile, error) {
		return projectPublicSpace(f), nil
	}, nil)
}
func (s *Service) PublicProfileByUserID(ctx context.Context, id string) (*SpacePublicProfile, error) {
	return s.currentPublicProfile(ctx, id, false)
}
func (s *Service) PublicProfileByUsername(ctx context.Context, name string) (*SpacePublicProfile, error) {
	return s.currentPublicProfile(ctx, name, true)
}
