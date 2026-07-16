package space

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

type fakeUserLookup struct {
	byID       map[string]*identitydomain.User
	byUsername map[string]*identitydomain.User
}

func newFakeUserLookup(users ...*identitydomain.User) *fakeUserLookup {
	lookup := &fakeUserLookup{
		byID:       map[string]*identitydomain.User{},
		byUsername: map[string]*identitydomain.User{},
	}
	for _, user := range users {
		lookup.byID[user.ID] = user
		lookup.byUsername[user.Username] = user
	}
	return lookup
}

func (f *fakeUserLookup) GetByID(_ context.Context, id string) (*identitydomain.User, error) {
	user, ok := f.byID[id]
	if !ok {
		return nil, identityrepo.ErrUserNotFound
	}
	return user, nil
}

func (f *fakeUserLookup) GetByUsername(_ context.Context, username string) (*identitydomain.User, error) {
	user, ok := f.byUsername[username]
	if !ok {
		return nil, identityrepo.ErrUserNotFound
	}
	return user, nil
}

func TestGetPublicByUsernameReturnsDefaultSpace(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
		Bio:      "hello",
	}))

	got, err := svc.GetPublicByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("get public space: %v", err)
	}
	if got.Owner.Username != "alice" {
		t.Fatalf("expected owner username alice, got %q", got.Owner.Username)
	}
	if !got.Space.IsDefault {
		t.Fatalf("expected default space")
	}
	if got.Space.Title != "Alice的个人主页" {
		t.Fatalf("unexpected title: %q", got.Space.Title)
	}
	if got.Space.Visibility != VisibilityPublic {
		t.Fatalf("expected public visibility, got %q", got.Space.Visibility)
	}
}

func TestPublicHomepageAlwaysUsesRouteOwnersStyle(t *testing.T) {
	repo := NewMemoryRepository()
	users := newFakeUserLookup(
		&identitydomain.User{ID: "1001", Username: "alice", Nickname: "Alice"},
		&identitydomain.User{ID: "1002", Username: "bob", Nickname: "Bob"},
	)
	svc := NewService(repo, users)
	aliceStyle := StylePackage{Manifest: StyleManifest{
		SchemaVersion: StyleSchemaVersion,
		Name:          "alice-motion",
		Version:       "0.1.0",
		Layout:        "grid",
		Components: []StyleComponent{
			{Slot: "header", Type: "profile-header"},
			{Slot: "main", Type: "content-list"},
		},
		CustomHTMLEnabled: true,
		CustomHTML:        `<section class="alice-intro">Alice</section>`,
		CustomCSS:         `.public-space[data-campusos-space] .alice-intro { color: #157f5b; }`,
	}}
	if _, err := svc.ApplyStylePackage(context.Background(), "1001", aliceStyle); err != nil {
		t.Fatalf("apply alice style: %v", err)
	}

	alice, err := svc.GetPublicByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}
	bob, err := svc.GetPublicByUsername(context.Background(), "bob")
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	if alice.Owner.ID != "1001" || alice.Space.StyleName != "alice-motion" || alice.Space.StyleManifest == nil {
		t.Fatalf("expected Alice's saved style, got %#v", alice)
	}
	if bob.Owner.ID != "1002" || bob.Space.StyleName == "alice-motion" || bob.Space.StyleManifest != nil {
		t.Fatalf("Bob must not inherit Alice's style, got %#v", bob)
	}
}

func TestUpsertOwnSpacePersistsConfig(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	title := " Alice Space "
	theme := "ink"
	visibility := "unlisted"
	syncEnabled := false

	got, err := svc.UpsertOwnSpace(context.Background(), "1001", UpsertSpaceRequest{
		Title:          &title,
		Theme:          &theme,
		Visibility:     &visibility,
		SyncEnabled:    &syncEnabled,
		SyncCategories: []string{"notice", "", "notice", "blog"},
		SyncTags:       []string{"go", "campus", "go"},
	})
	if err != nil {
		t.Fatalf("upsert own space: %v", err)
	}
	if got.Space.IsDefault {
		t.Fatalf("saved space should not be marked as default")
	}
	if got.Space.Title != "Alice Space" {
		t.Fatalf("expected trimmed title, got %q", got.Space.Title)
	}
	if got.Space.Visibility != VisibilityUnlisted {
		t.Fatalf("expected unlisted visibility, got %q", got.Space.Visibility)
	}
	if got.Space.SyncEnabled {
		t.Fatalf("expected sync disabled")
	}
	if len(got.Space.SyncCategories) != 2 || got.Space.SyncCategories[0] != "notice" || got.Space.SyncCategories[1] != "blog" {
		t.Fatalf("unexpected sync categories: %#v", got.Space.SyncCategories)
	}
	if len(got.Space.SyncTags) != 2 || got.Space.SyncTags[0] != "go" || got.Space.SyncTags[1] != "campus" {
		t.Fatalf("unexpected sync tags: %#v", got.Space.SyncTags)
	}

	own, err := svc.GetOwnSpace(context.Background(), "1001")
	if err != nil {
		t.Fatalf("get own space: %v", err)
	}
	if own.Space.Title != "Alice Space" {
		t.Fatalf("expected persisted title, got %q", own.Space.Title)
	}
}

func TestPrivateSpaceIsNotPublic(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	visibility := "private"

	if _, err := svc.UpsertOwnSpace(context.Background(), "1001", UpsertSpaceRequest{Visibility: &visibility}); err != nil {
		t.Fatalf("upsert own space: %v", err)
	}
	_, err := svc.GetPublicByUserID(context.Background(), "1001")
	if !errors.Is(err, ErrSpaceNotPublic) {
		t.Fatalf("expected ErrSpaceNotPublic, got %v", err)
	}
}

func TestUploadAvatarStoresPersonalSpaceAvatar(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	store, err := NewLocalFileStore(FileStorageConfig{
		RootDir:           t.TempDir(),
		URLPrefix:         "/api/v1/spaces/files",
		DefaultQuotaBytes: 10 * 1024 * 1024,
		AvatarKeepLimit:   3,
		MaxAvatarBytes:    1024,
	})
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	svc.SetFileStore(store)

	uploaded, err := svc.UploadAvatar(context.Background(), "1001", "avatar.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatalf("upload avatar: %v", err)
	}
	if !strings.HasPrefix(uploaded.URL, "/api/v1/spaces/files/1001/avatars/") {
		t.Fatalf("unexpected avatar url: %q", uploaded.URL)
	}
	if uploaded.Owner.Avatar != uploaded.URL {
		t.Fatalf("owner avatar should use uploaded space avatar")
	}
	if uploaded.Space == nil || uploaded.Space.Avatar != uploaded.URL {
		t.Fatalf("space avatar was not persisted: %#v", uploaded.Space)
	}
	if uploaded.Storage.UsedBytes <= 0 {
		t.Fatalf("expected used storage bytes")
	}

	own, err := svc.GetOwnSpace(context.Background(), "1001")
	if err != nil {
		t.Fatalf("get own space: %v", err)
	}
	if own.Owner.Avatar != uploaded.URL || own.Space.Avatar != uploaded.URL {
		t.Fatalf("saved avatar not reflected in own space: %#v", own)
	}
}

func TestPersonalSpacePluginDisabledBlocksUserOperations(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	svc.SetPluginEnabledChecker(func() bool { return false })

	if _, err := svc.GetOwnSpace(context.Background(), "1001"); !errors.Is(err, ErrSpacePluginDisabled) {
		t.Fatalf("expected disabled plugin error for own space, got %v", err)
	}
	if _, err := svc.GetPublicByUsername(context.Background(), "alice"); !errors.Is(err, ErrSpacePluginDisabled) {
		t.Fatalf("expected disabled plugin error for public space, got %v", err)
	}
	title := "Blocked"
	if _, err := svc.UpsertOwnSpace(context.Background(), "1001", UpsertSpaceRequest{Title: &title}); !errors.Is(err, ErrSpacePluginDisabled) {
		t.Fatalf("expected disabled plugin error for update, got %v", err)
	}
}
