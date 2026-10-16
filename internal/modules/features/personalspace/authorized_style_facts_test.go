package space

import (
	"context"
	"reflect"
	"testing"
)

func TestSpaceStyleOwnerCapabilitiesFactsStayDetached(t *testing.T) {
	ctx := context.Background()
	repo, space, snapshot := spaceFactsFixture(t)
	space.StyleManifest.Capabilities = []string{"space.display"}
	if err := repo.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	snapshot.ID = "3002"
	snapshot.StyleManifest = cloneManifest(space.StyleManifest)
	if err := repo.SaveStyleSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	before, err := repo.LoadSpaceAuthority(ctx, "1001", false)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := repo.LoadStyleSnapshotAuthority(ctx, "1001", false)
	if err != nil {
		t.Fatal(err)
	}
	space.StyleManifest.Capabilities[0] = "input mutation"
	snapshot.StyleManifest.Capabilities[0] = "snapshot mutation"
	loaded, err := repo.GetByUserID(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	loaded.StyleManifest.Capabilities[0] = "getter mutation"
	latest, err := repo.GetLatestStyleSnapshot(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	latest.StyleManifest.Capabilities[0] = "latest mutation"
	cloned := cloneManifest(loaded.StyleManifest)
	cloned.Capabilities[0] = "clone mutation"
	if loaded.StyleManifest.Capabilities[0] != "getter mutation" {
		t.Fatal("cloneManifest retained capabilities alias")
	}
	after, err := repo.LoadSpaceAuthority(ctx, "1001", false)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadStyleSnapshotAuthority(ctx, "1001", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(rows, current) {
		t.Fatal("capabilities alias bypassed complete facts and nonce")
	}
}
