package pluginui

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testStore struct{ item *Invocation }

func (s *testStore) CreateInvocation(_ context.Context, v *Invocation) error {
	copy := *v
	s.item = &copy
	return nil
}
func (s *testStore) GetInvocation(_ context.Context, id string) (*Invocation, error) {
	if s.item == nil || s.item.ID != id {
		return nil, ErrNotFound
	}
	copy := *s.item
	return &copy, nil
}
func (s *testStore) MarkInvocationOpened(context.Context, string, string) error { return nil }

func TestContextPinsUserResourceVersionAndLivePolicy(t *testing.T) {
	ctx := context.Background()
	store := &testStore{}
	version, resource := "plugin-version-1", "object-version-1"
	allowed, enabled := true, true
	s := Service{Store: store,
		Resolve: func(_ context.Context, user string, v *Invocation) (string, error) {
			if user != "alice" || v.AssetID != "asset-1" {
				return "", ErrNotFound
			}
			return resource, nil
		},
		Surface: func(_ context.Context, v *Invocation) (string, error) {
			if !enabled || v.PluginKey != "reader" || v.SurfaceID != "reader.preview" {
				return "", ErrNotFound
			}
			return version, nil
		},
		Authorize: func(context.Context, *Invocation, string) error {
			if !allowed {
				return ErrNotFound
			}
			return nil
		},
	}
	binding := Invocation{UserID: "alice", PluginKey: "reader", SurfaceID: "reader.preview", ContextKind: "personal_asset", AssetID: "asset-1", Presentation: "modal"}
	v, err := s.Issue(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	if v.ContextDigest == "" || time.Until(v.ExpiresAt) > 10*time.Minute {
		t.Fatal("missing digest or unbounded TTL")
	}
	if _, err = s.Open(ctx, "alice", v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx, "bob", v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user access: %v", err)
	}
	version = "plugin-version-2"
	if _, err = s.Open(ctx, "alice", v.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("upgraded version: %v", err)
	}
	version = "plugin-version-1"
	resource = "object-version-2"
	if _, err = s.Open(ctx, "alice", v.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("changed resource version: %v", err)
	}
	resource = "object-version-1"
	allowed = false
	if _, err = s.Open(ctx, "alice", v.ID); err == nil {
		t.Fatal("revoked authorization allowed")
	}
	allowed = true
	enabled = false
	if _, err = s.Open(ctx, "alice", v.ID); err == nil {
		t.Fatal("stopped plugin allowed")
	}
	enabled = true
	store.item.ExpiresAt = time.Now().Add(-time.Second)
	if _, err = s.Open(ctx, "alice", v.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired context: %v", err)
	}
	binding.PersonalDocumentID = "another-context"
	if _, err = s.Issue(ctx, binding); err == nil {
		t.Fatal("mixed resource types accepted")
	}
	binding.PersonalDocumentID = ""
	binding.PluginKey = "other-plugin"
	if _, err = s.Issue(ctx, binding); err == nil {
		t.Fatal("foreign plugin surface accepted")
	}
	if _, err = (Service{Store: store}).Issue(ctx, binding); err == nil {
		t.Fatal("unwired policy failed open")
	}
}
