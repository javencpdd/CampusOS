package richtext

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreListsOnlyUploaderAssets(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	for _, asset := range []*Asset{
		{ID: "a-1", UploaderID: "1001", FileName: "first.png", CreatedAt: now},
		{ID: "a-2", UploaderID: "1002", FileName: "other.png", CreatedAt: now.Add(time.Minute)},
		{ID: "a-3", UploaderID: "1001", FileName: "latest.png", CreatedAt: now.Add(2 * time.Minute)},
	} {
		if err := store.SaveAsset(context.Background(), asset); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListAssetsByUploader(context.Background(), "1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].FileName != "latest.png" || items[1].FileName != "first.png" {
		t.Fatalf("uploader inventory = %#v, want newest two assets for owner 1001", items)
	}
}
