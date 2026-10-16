package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestArticleAuthorityMemoryNonceActorSnapshotAndCAS(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Now().UTC()
	a := &Article{ID: "article.facts", ThreadID: "thread.facts", Title: "before", Summary: "original", ContentHTML: "<p>private</p>", ContentJSON: json.RawMessage(`{"value":9007199254740993}`), SanitizedHTML: "<p>private</p>", RenderHTML: RenderArticleHTML("<p>private</p>"), Status: StatusDraft, CreatedBy: "owner", UpdatedBy: "owner", CreatedAt: now, UpdatedAt: now, FactsVersion: 999}
	published := now.Add(-time.Hour)
	originalPublished := published
	a.PublishedAt = &published
	if err := store.CreateArticle(ctx, a); err != nil {
		t.Fatal(err)
	}
	if a.FactsVersion < 1 || a.FactsVersion == 999 || a.UpdatedActorKind != "user" || a.UpdatedActorID != "owner" {
		t.Fatal(a)
	}
	version := a.FactsVersion
	*a.PublishedAt = now
	read, _ := store.GetArticleByThreadID(ctx, a.ThreadID)
	if !read.PublishedAt.Equal(originalPublished) || read.FactsVersion != version {
		t.Fatal("creation date alias changed stored authority", read)
	}
	*read.PublishedAt = now.Add(time.Hour)
	a, _ = store.GetArticleByThreadID(ctx, a.ThreadID)
	if !a.PublishedAt.Equal(originalPublished) || a.FactsVersion != version {
		t.Fatal("read date alias changed stored authority", a)
	}
	a.UpdatedAt = now.Add(time.Minute)
	a.FactsVersion = 999
	if err := store.UpdateArticle(ctx, a); err != nil || a.FactsVersion != version {
		t.Fatal("display timestamp advanced facts", a, err)
	}
	a.Summary = "changed"
	if err := store.UpdateArticle(ctx, a); err != nil {
		t.Fatal(err)
	}
	first := a.FactsVersion
	a.Summary = "original"
	if err := store.UpdateArticle(ctx, a); err != nil || a.FactsVersion <= first {
		t.Fatal("metadata ABA nonce reused", a, err)
	}
	if _, err := store.LockArticleAuthority(ctx, a.ThreadID); !errors.Is(err, ErrArticleFactsUnavailable) {
		t.Fatal("lock outside transaction", err)
	}
	base, _ := store.GetArticleByThreadID(ctx, a.ThreadID)
	high := base.FactsVersion
	if err := transaction.NewMemory(store).Within(ctx, func(tx context.Context) error {
		actual, err := store.LockArticleAuthority(tx, a.ThreadID)
		if err != nil {
			return err
		}
		if err := store.UpdateArticleIfFacts(tx, actual, actual.FactsVersion-1); !errors.Is(err, ErrArticleFactsConflict) {
			t.Fatal("stale snapshot accepted", err)
		}
		actual.UpdatedBy = ""
		actual.UpdatedActorKind, actual.UpdatedActorID = "admin", "adm.opaque"
		actual.Summary = "admin change"
		if err := store.UpdateArticleIfFacts(tx, actual, actual.FactsVersion); err != nil {
			return err
		}
		high = actual.FactsVersion
		actual.ContentJSON[0] = 'x'
		current, err := store.LockArticleAuthority(tx, a.ThreadID)
		if err != nil || !json.Valid(current.ContentJSON) || current.UpdatedBy != "" || current.UpdatedActorID != "adm.opaque" {
			t.Fatal("snapshot alias/domain", current, err)
		}
		wire, _ := json.Marshal(current)
		var fields map[string]any
		json.Unmarshal(wire, &fields)
		for _, key := range []string{"facts_version", "updated_actor_kind", "updated_actor_id"} {
			if _, ok := fields[key]; ok {
				t.Fatal("host facts leaked", key)
			}
		}
		return errors.New("forced rollback")
	}); err == nil {
		t.Fatal("rollback ignored")
	}
	actual, _ := store.GetArticleByThreadID(ctx, a.ThreadID)
	if actual.Summary != base.Summary || actual.FactsVersion != base.FactsVersion || actual.UpdatedActorKind != "user" || actual.UpdatedActorID != "owner" {
		t.Fatal("hidden snapshot not restored", actual)
	}
	actual.Summary = "next"
	if err := store.UpdateArticle(ctx, actual); err != nil || actual.FactsVersion <= high {
		t.Fatal("rollback allocation reused", actual, err)
	}
}
