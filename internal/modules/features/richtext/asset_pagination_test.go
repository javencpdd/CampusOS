package richtext

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestUserAssetKeysetPagination(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := &Service{store: store}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := 1; i <= 231; i++ {
		owner := "owner"
		if i == 231 {
			owner = "other"
		}
		if err := store.CreateUserAsset(ctx, &UserAsset{ID: fmt.Sprint(i), StorageObjectID: fmt.Sprint(i), OwnerID: owner, Status: AssetStatusActive, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := svc.ListMyUserAssetPage(ctx, "owner", "active", cursor, 37)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] || item.OwnerID != "owner" {
				t.Fatalf("duplicate or foreign item: %+v", item)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if _, err := svc.ListMyUserAssetPage(ctx, "other", "active", page.NextCursor, 37); !errors.Is(err, ErrAssetInvalid) {
			t.Fatalf("owner-swapped cursor: %v", err)
		}
		if _, err := svc.ListMyUserAssetPage(ctx, "owner", "trashed", page.NextCursor, 37); !errors.Is(err, ErrAssetInvalid) {
			t.Fatalf("status-swapped cursor: %v", err)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 230 {
		t.Fatalf("got %d assets, want 230", len(seen))
	}
	for _, invalid := range []string{"not-json", "!"} {
		if _, err := svc.ListMyUserAssetPage(ctx, "owner", "active", invalid, 37); !errors.Is(err, ErrAssetInvalid) {
			t.Fatalf("malformed cursor: %v", err)
		}
	}
	for _, limit := range []int{-1, 201} {
		if _, err := svc.ListMyUserAssetPage(ctx, "owner", "active", "", limit); !errors.Is(err, ErrAssetInvalid) {
			t.Fatalf("invalid limit: %v", err)
		}
	}
	if _, err := svc.ListMyUserAssetPage(ctx, "", "active", "", 37); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("anonymous list: %v", err)
	}
}
