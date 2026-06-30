package space

import (
	"context"
	"errors"
	"testing"

	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
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
