package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestPostAuthorityMemoryDetachedFactsAndCAS(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryPostRepository()
	parent := "parent"
	original := &domain.Post{ID: "post", ThreadID: "thread", AuthorID: "owner", ParentID: &parent, Content: "original", FactsVersion: 900}
	if err := r.Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	first := original.FactsVersion
	if first < 1 || first == 900 {
		t.Fatal("caller assigned version", first)
	}
	parent = "caller-changed"
	original.Content = "caller-changed"
	row, _ := r.GetByID(ctx, "post")
	if row.Content != "original" || *row.ParentID != "parent" {
		t.Fatal("caller aliased actual facts", row)
	}
	row.Content = "reader-changed"
	*row.ParentID = "reader-changed"
	rows, _, _ := r.ListByThread(ctx, "thread", 1, 20)
	rows[0].Content = "page-changed"
	current, _ := r.GetByID(ctx, "post")
	if current.Content != "original" || *current.ParentID != "parent" {
		t.Fatal(current)
	}
	if _, err := r.LockPostForWrite(ctx, "post"); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	tx := transaction.NewMemory(r)
	if err := tx.Within(ctx, func(ctx context.Context) error {
		candidate, _ := r.LockPostForWrite(ctx, "post")
		candidate.Content = "changed"
		candidate.LikeCount = 999
		if err := r.UpdatePostIfFactsVersion(ctx, candidate, first); err != nil {
			return err
		}
		if candidate.FactsVersion <= first || candidate.LikeCount != 0 {
			t.Fatal("facts or counters overwritten", candidate)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Within(ctx, func(ctx context.Context) error { return r.UpdatePostIfFactsVersion(ctx, current, first) }); !errors.Is(err, ErrPostFactsConflict) {
		t.Fatal("stale allow reused", err)
	}
	after, _ := r.GetByID(ctx, "post")
	noOpVersion := after.FactsVersion
	after.FactsVersion = -1
	after.LikeCount++
	if err := r.Update(ctx, after); err != nil {
		t.Fatal(err)
	}
	if after.FactsVersion != noOpVersion {
		t.Fatal("counter advanced authority version", after)
	}
	if err := tx.Within(ctx, func(ctx context.Context) error {
		return r.DeletePostIfFactsVersion(ctx, "post", after.FactsVersion, identityport.PrincipalRef{Kind: "admin", ID: "owner"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetByID(ctx, "post"); !errors.Is(err, ErrPostNotFound) {
		t.Fatal(err)
	}
	deleted, err := r.ReadPostCommandSnapshot(ctx, "post")
	if err != nil || deleted.DeletedAt == nil || deleted.DeletedActorKind != "admin" || deleted.DeletedActorID != "owner" || deleted.AuthorID != "owner" || deleted.FactsVersion <= after.FactsVersion {
		t.Fatal("domain deletion facts lost", deleted, err)
	}
}

func TestPostAuthorityMemoryRollbackNeverReusesAllocation(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryPostRepository()
	p := &domain.Post{ID: "post", ThreadID: "thread", AuthorID: "owner", Content: "original"}
	r.Create(ctx, p)
	first := p.FactsVersion
	allocated := int64(0)
	sentinel := errors.New("required audit failure")
	err := transaction.NewMemory(r).Within(ctx, func(ctx context.Context) error {
		current, _ := r.GetByID(ctx, p.ID)
		current.Content = "rollback"
		if err := r.UpdatePostIfFactsVersion(ctx, current, first); err != nil {
			return err
		}
		allocated = current.FactsVersion
		if err := r.DeletePostIfFactsVersion(ctx, p.ID, current.FactsVersion, identityport.PrincipalRef{Kind: "admin", ID: "adm.actual"}); err != nil {
			return err
		}
		deleted, _ := r.ReadPostCommandSnapshot(ctx, p.ID)
		allocated = deleted.FactsVersion
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	restored, _ := r.GetByID(ctx, p.ID)
	if restored.FactsVersion != first || restored.Content != "original" || restored.DeletedAt != nil || restored.DeletedActorKind != "legacy_unknown" || restored.DeletedActorID != "" {
		t.Fatal("hidden snapshot facts lost", restored)
	}
	restored.Content = "next"
	if err := r.Update(ctx, restored); err != nil {
		t.Fatal(err)
	}
	if restored.FactsVersion <= allocated {
		t.Fatal("rollback allocation reused", restored)
	}
	old := restored.FactsVersion
	if err := r.Delete(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	replacement := &domain.Post{ID: p.ID, ThreadID: p.ThreadID, AuthorID: p.AuthorID, Content: "replacement"}
	if err := r.Create(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.FactsVersion <= old {
		t.Fatal("replacement reused observed version", replacement)
	}
}
