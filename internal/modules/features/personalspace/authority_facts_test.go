package space

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"math"
	"reflect"
	"testing"
	"time"
)

func spaceFactsFixture(t *testing.T) (*MemoryRepository, *Space, *StyleSnapshot) {
	t.Helper()
	r := NewMemoryRepository()
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := &Space{ID: "2001", UserID: "1001", Title: "Private space", Theme: "default", Layout: "blog", Visibility: VisibilityPrivate, SyncEnabled: true, CreatedAt: now, UpdatedAt: now, StyleManifest: &StyleManifest{Components: []StyleComponent{{Props: map[string]interface{}{"nested": map[string]interface{}{"items": []interface{}{map[string]interface{}{"text": "owned"}}}}}}}}
	if err := r.Upsert(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	snapshot := &StyleSnapshot{ID: "3001", UserID: "1001", SnapshotType: "before_apply", Theme: "default", Layout: "blog", StyleManifest: cloneSpace(s).StyleManifest, CreatedAt: now}
	if err := r.SaveStyleSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	return r, s, snapshot
}
func spaceFactVersion(t *testing.T, raw json.RawMessage) int64 {
	t.Helper()
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid fact")
	}
	var n int64
	if json.Unmarshal(fields["facts_version"], &n) != nil || n < 1 {
		t.Fatal("invalid nonce", string(raw))
	}
	return n
}
func TestSpaceFactsMemoryWholeRowsAliasesAndMetadataABA(t *testing.T) {
	ctx := context.Background()
	r, s, snapshot := spaceFactsFixture(t)
	raw, err := r.LoadSpaceAuthority(ctx, "1001", false)
	if err != nil {
		t.Fatal(err)
	}
	before := append(json.RawMessage(nil), raw...)
	if spaceFactVersion(t, raw) != s.FactsVersion {
		t.Fatal("model and private row differ")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 26 || fields["deleted_at"] == nil || fields["disabled_actor_kind"] == nil {
		t.Fatal("incomplete actual row", len(fields), string(raw))
	}
	s.StyleManifest.Components[0].Props["nested"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})["text"] = "caller alias"
	loaded, _ := r.GetByUserID(ctx, "1001")
	loaded.StyleManifest.Components[0].Props["nested"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})["text"] = "getter alias"
	raw[0] = 'x'
	current, _ := r.LoadSpaceAuthority(ctx, "1001", false)
	if !reflect.DeepEqual(current, before) {
		t.Fatal("input/getter/raw alias changed stored facts")
	}
	loaded, _ = r.GetByUserID(ctx, "1001")
	loaded.Title = "changed"
	if err := r.Upsert(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	loaded.Title = "Private space"
	if err := r.Upsert(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	after, _ := r.LoadSpaceAuthority(ctx, "1001", false)
	if spaceFactVersion(t, after) <= spaceFactVersion(t, before) {
		t.Fatal("metadata ABA reused nonce")
	}
	version := loaded.FactsVersion
	at := time.Now().UTC()
	if err := r.UpdateSyncStatus(ctx, "1001", &at, "operational retry"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = r.GetByUserID(ctx, "1001")
	if loaded.FactsVersion != version {
		t.Fatal("operational sync advanced metadata nonce")
	}
	rows, _ := r.LoadStyleSnapshotAuthority(ctx, "1001", false)
	if len(rows) != 1 || spaceFactVersion(t, rows[0]) != snapshot.FactsVersion {
		t.Fatal(rows)
	}
	snapshot.StyleManifest.Components[0].Props["nested"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})["text"] = "snapshot alias"
	latest, _ := r.GetLatestStyleSnapshot(ctx, "1001")
	latest.StyleManifest.Components[0].Props["nested"].(map[string]interface{})["items"].([]interface{})[0].(map[string]interface{})["text"] = "latest alias"
	other, _ := r.LoadStyleSnapshotAuthority(ctx, "1001", false)
	if !reflect.DeepEqual(rows, other) {
		t.Fatal("snapshot alias changed facts")
	}
}
func TestSpaceFactsMemoryRollbackHighwaterExhaustionAndEmptyProof(t *testing.T) {
	ctx := context.Background()
	r, _, _ := spaceFactsFixture(t)
	before, _ := r.LoadSpaceAuthority(ctx, "1001", false)
	snapshots, _ := r.LoadStyleSnapshotAuthority(ctx, "1001", false)
	abandoned := int64(0)
	err := transaction.NewMemory(r).Within(ctx, func(tx context.Context) error {
		space, _ := r.GetByUserID(tx, "1001")
		space.Bio = "abandoned"
		if err := r.Upsert(tx, space); err != nil {
			return err
		}
		abandoned = space.FactsVersion
		if err := r.SaveStyleSnapshot(tx, &StyleSnapshot{ID: "3002", UserID: "1001", CreatedAt: time.Now()}); err != nil {
			return err
		}
		return errors.New("required command rejected")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	after, _ := r.LoadSpaceAuthority(ctx, "1001", false)
	rows, _ := r.LoadStyleSnapshotAuthority(ctx, "1001", false)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(rows, snapshots) {
		t.Fatal("rollback did not restore rows")
	}
	space, _ := r.GetByUserID(ctx, "1001")
	space.Bio = "next"
	if err := r.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	if space.FactsVersion <= abandoned {
		t.Fatal("rollback rewound highwater")
	}
	r.factsCounter = math.MaxInt64
	baseline := r.Snapshot()
	space.Title = "exhausted"
	if err := r.Upsert(ctx, space); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, r.Snapshot()) {
		t.Fatal("overflow partially updated metadata")
	}
	if err := r.SaveStyleSnapshot(ctx, &StyleSnapshot{ID: "3003", UserID: "1001"}); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal(err)
	}
	at := time.Now()
	if err := r.SetDisabled(ctx, "1001", &at, "1002", "reason"); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, r.Snapshot()) {
		t.Fatal("overflow partially updated snapshot/lifecycle")
	}
	if _, err := r.LoadSpaceAuthority(ctx, "1001", true); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal("unlocked command accepted", err)
	}
	if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
		empty, err := r.LoadStyleSnapshotAuthority(tx, "1002", true)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatal("empty proof missing", empty, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.LoadStyleSnapshotAuthority(canceled, "1002", false); err == nil {
		t.Fatal("canceled empty read accepted")
	}
}
