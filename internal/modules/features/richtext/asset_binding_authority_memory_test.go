package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func assetBindingMemoryFixture(t *testing.T) (*MemoryStore, []*UserAsset, []*ArticleAttachment) {
	t.Helper()
	s := NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"article.memory", "article.other"} {
		if err := s.CreateArticle(ctx, &Article{ID: id, ThreadID: "thread." + id, Title: "Memory Article", CreatedBy: "owner.memory", UpdatedBy: "owner.memory", Status: StatusDraft, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	assets, bindings := []*UserAsset{}, []*ArticleAttachment{}
	for _, id := range []string{"a", "b", "c"} {
		a := &UserAsset{ID: "asset." + id, OwnerID: "owner.memory", Kind: AssetKindAttachment, OriginalName: "original.pdf", StorageObjectID: "object." + id, MimeType: "application/pdf", SizeBytes: 100, Status: AssetStatusActive, Version: 7, CreatedAt: now, UpdatedAt: now}
		if err := s.CreateUserAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
		article := "article.memory"
		if id == "c" {
			article = "article.other"
		}
		// Embedded client DTO facts are never the stored relationship source.
		b := &ArticleAttachment{ID: "binding." + id, ArticleContentID: article, AssetID: a.ID, DisplayName: "display.pdf", CreatedAt: now, UpdatedAt: now, Asset: UserAsset{ID: "forged", OwnerID: "someone.else"}}
		if err := s.CreateAttachment(ctx, b); err != nil {
			t.Fatal(err)
		}
		assets, bindings = append(assets, a), append(bindings, b)
	}
	return s, assets, bindings
}

func memoryFactString(t *testing.T, raw json.RawMessage, field string) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := json.Unmarshal(fields[field], &value); err != nil {
		t.Fatal(field, err)
	}
	return value
}

func TestAssetBindingMemoryScopeNonceAndAliases(t *testing.T) {
	s, assets, bindings := assetBindingMemoryFixture(t)
	ctx := context.Background()
	rows, err := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for i, row := range rows {
		if memoryFactString(t, row.Binding, "id") != bindings[i].ID || memoryFactString(t, row.Binding, "asset_id") != assets[i].ID || memoryFactString(t, row.Asset, "id") != assets[i].ID || memoryFactString(t, row.Asset, "owner_user_id") != assets[i].OwnerID {
			t.Fatal("scope or actual relationship lost", i, row)
		}
		assetBindingNonce(t, row.Binding)
		assetBindingNonce(t, row.Asset)
	}
	other, err := s.LoadArticleBindingAuthorityFacts(ctx, "article.other", false)
	if err != nil || len(other) != 1 || memoryFactString(t, other[0].Binding, "id") != bindings[2].ID {
		t.Fatal(other, err)
	}
	rows[0].Asset[0], rows[0].Binding[0] = '!', '!'
	fresh, err := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if err != nil || !json.Valid(fresh[0].Asset) || !json.Valid(fresh[0].Binding) {
		t.Fatal("raw snapshots alias", fresh, err)
	}
	assets[0].OwnerID, bindings[0].DisplayName = "forged.after.create", "forged.after.create"
	actual, _ := s.GetUserAsset(ctx, "asset.a")
	bound, _ := s.GetAttachment(ctx, "article.memory", "binding.a")
	if actual.OwnerID != "owner.memory" || bound.DisplayName != "display.pdf" || bound.Asset.OwnerID != "owner.memory" {
		t.Fatal("create/embedded DTO alias", actual, bound)
	}
	now := time.Now().UTC()
	a := &UserAsset{ID: "asset.dates", OwnerID: "owner.memory", Kind: AssetKindAttachment, OriginalName: "date.pdf", StorageObjectID: "object.dates", Status: AssetStatusTrashed, Version: 1, CreatedAt: now, UpdatedAt: now, TrashedAt: &now}
	original := now
	if err := s.CreateUserAsset(ctx, a); err != nil {
		t.Fatal(err)
	}
	*a.TrashedAt = now.Add(time.Hour)
	read, _ := s.GetUserAsset(ctx, a.ID)
	*read.TrashedAt = now.Add(2 * time.Hour)
	read, _ = s.GetUserAsset(ctx, a.ID)
	if !read.TrashedAt.Equal(original) {
		t.Fatal("date aliases changed stored facts", read)
	}
	article := &Article{ID: "article.nested", ThreadID: "thread.nested", CreatedBy: "owner.memory", UpdatedBy: "owner.memory", Status: StatusDraft, Attachments: []ArticleAttachment{{Asset: *read}}}
	if err := s.CreateArticle(ctx, article); err != nil {
		t.Fatal(err)
	}
	*article.Attachments[0].Asset.TrashedAt = original.Add(3 * time.Hour)
	loaded, _ := s.GetArticleByThreadID(ctx, article.ThreadID)
	*loaded.Attachments[0].Asset.TrashedAt = original.Add(4 * time.Hour)
	loaded, _ = s.GetArticleByThreadID(ctx, article.ThreadID)
	if !loaded.Attachments[0].Asset.TrashedAt.Equal(original) {
		t.Fatal("nested article date alias", loaded)
	}
	if _, err := s.LoadAssetAuthorityFacts(ctx, "asset.a", true); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
		t.Fatal("lock outside transaction", err)
	}
	if _, err := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", true); !errors.Is(err, ErrAssetBindingFactsUnavailable) {
		t.Fatal("aggregate lock outside transaction", err)
	}
	if err := transaction.NewMemory(s).Within(ctx, func(tx context.Context) error {
		if _, err := s.LoadAssetAuthorityFacts(tx, "asset.a", true); err != nil {
			return err
		}
		empty, err := s.LoadArticleBindingAuthorityFacts(tx, "article.empty", true)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatal(empty, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if err := s.UpdateAttachmentDisplayName(ctx, "article.memory", "binding.a", "changed.pdf"); err != nil {
		t.Fatal(err)
	}
	changed, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if err := s.UpdateAttachmentDisplayName(ctx, "article.memory", "binding.a", "display.pdf"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if assetBindingNonce(t, changed[0].Binding) <= assetBindingNonce(t, before[0].Binding) || assetBindingNonce(t, after[0].Binding) <= assetBindingNonce(t, changed[0].Binding) || !bytes.Equal(assetBindingOriginal(t, before[0].Binding), assetBindingOriginal(t, after[0].Binding)) {
		t.Fatal("name ABA not visible")
	}
	if err := s.ReorderAttachments(ctx, "article.memory", []string{"binding.b", "binding.a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderAttachments(ctx, "article.memory", []string{"binding.a", "binding.b"}); err != nil {
		t.Fatal(err)
	}
	reordered, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if assetBindingNonce(t, reordered[0].Binding) <= assetBindingNonce(t, after[0].Binding) {
		t.Fatal("order ABA not visible")
	}
	if err := s.RemoveAttachment(ctx, "article.memory", "binding.a"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUserAsset(ctx, "asset.a", "owner.memory"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUserAsset(ctx, actual); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAttachment(ctx, bound); err != nil {
		t.Fatal(err)
	}
	reinserted, _ := s.LoadAssetAuthorityFacts(ctx, "asset.a", false)
	if assetBindingNonce(t, reinserted) <= assetBindingNonce(t, before[0].Asset) {
		t.Fatal("asset reinsert reused nonce")
	}
	list, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	for _, row := range list {
		if memoryFactString(t, row.Binding, "id") == "binding.a" && assetBindingNonce(t, row.Binding) <= assetBindingNonce(t, reordered[0].Binding) {
			t.Fatal("binding reinsert reused nonce")
		}
	}
	wire, _ := json.Marshal(actual)
	if bytes.Contains(wire, []byte("facts_version")) {
		t.Fatal("internal facts exported")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.LoadArticleBindingAuthorityFacts(canceled, "article.memory", false); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestAssetBindingMemoryRollbackAtomicFailureAndExhaustion(t *testing.T) {
	s, _, _ := assetBindingMemoryFixture(t)
	ctx := context.Background()
	before := s.Snapshot()
	var high int64
	err := transaction.NewMemory(s).Within(ctx, func(tx context.Context) error {
		if err := s.UpdateAttachmentDisplayName(tx, "article.memory", "binding.a", "rollback.pdf"); err != nil {
			return err
		}
		if err := s.TransitionUserAsset(tx, "asset.a", "owner.memory", []string{AssetStatusActive}, AssetStatusTrashed, AssetLifecycleAudit{ActorID: "owner.memory", ActorType: "user", Action: "trashed"}); err != nil {
			return err
		}
		raw, err := s.LoadAssetAuthorityFacts(tx, "asset.a", true)
		if err != nil {
			return err
		}
		high = assetBindingNonce(t, raw)
		return errors.New("required command failure")
	})
	if err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("original content/private facts/audit not restored", err)
	}
	if err := s.UpdateAttachmentDisplayName(ctx, "article.memory", "binding.a", "next.pdf"); err != nil {
		t.Fatal(err)
	}
	facts, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if assetBindingNonce(t, facts[0].Binding) <= high {
		t.Fatal("rollback allocation reused")
	}
	if err := s.UpdateAttachmentDisplayName(ctx, "article.memory", "binding.a", "next.pdf"); err != nil {
		t.Fatal(err)
	}
	noop, _ := s.LoadArticleBindingAuthorityFacts(ctx, "article.memory", false)
	if assetBindingNonce(t, noop[0].Binding) != assetBindingNonce(t, facts[0].Binding) {
		t.Fatal("timestamp-only rename advanced nonce")
	}
	for _, ids := range [][]string{{"binding.b", "missing"}, {"binding.b", "binding.b"}, {"binding.b"}, {"binding.c", "binding.a"}} {
		base := s.Snapshot()
		if err := s.ReorderAttachments(ctx, "article.memory", ids); err == nil || !reflect.DeepEqual(base, s.Snapshot()) {
			t.Fatal("invalid reorder partially changed actual inventory", ids, err)
		}
	}
	for _, mode := range []string{"rename", "transition", "create", "reorder", "manual_version"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _ := assetBindingMemoryFixture(t)
			store.assetBindingCounter = math.MaxInt64
			if mode == "reorder" {
				store.assetBindingCounter = math.MaxInt64 - 1
			}
			if mode == "manual_version" {
				store.assetBindingCounter = 10
				store.userAssets["asset.a"].Version = math.MaxInt64
			}
			base, counter := store.Snapshot(), store.assetBindingCounter
			var err error
			switch mode {
			case "rename":
				err = store.UpdateAttachmentDisplayName(ctx, "article.memory", "binding.a", "exhausted.pdf")
			case "transition", "manual_version":
				err = store.TransitionUserAsset(ctx, "asset.a", "owner.memory", []string{AssetStatusActive}, AssetStatusTrashed, AssetLifecycleAudit{})
			case "create":
				err = store.CreateUserAsset(ctx, &UserAsset{ID: "asset.new", OwnerID: "owner.memory", StorageObjectID: "object.new"})
			case "reorder":
				err = store.ReorderAttachments(ctx, "article.memory", []string{"binding.b", "binding.a"})
			}
			if !errors.Is(err, ErrAssetBindingFactsUnavailable) || !reflect.DeepEqual(base, store.Snapshot()) || counter != store.assetBindingCounter {
				t.Fatal("exhaustion partially changed data/audit/high water", mode, err)
			}
		})
	}
}
