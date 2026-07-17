package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestCreateCategoryGeneratesSlugFromEnglishName(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name:        "Campus News",
		Description: "News and notices",
		Icon:        "N",
		SortOrder:   3,
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if category.Slug != "campus-news" {
		t.Fatalf("expected generated slug campus-news, got %q", category.Slug)
	}
	if category.Icon != "N" || category.SortOrder != 3 {
		t.Fatalf("expected icon and sort order to be preserved, got %#v", category)
	}
}

func TestCreateCategoryGeneratesFallbackSlugForChineseName(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name: "校园公告",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if !strings.HasPrefix(category.Slug, "category-") {
		t.Fatalf("expected category-id fallback slug, got %q", category.Slug)
	}
}

func TestCreateCategoryKeepsProvidedSlugNormalized(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name: "Any Name",
		Slug: "  Custom Slug_01  ",
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if category.Slug != "custom-slug-01" {
		t.Fatalf("expected normalized slug custom-slug-01, got %q", category.Slug)
	}
}

func TestCreateCategoryNormalizesDefaultTags(t *testing.T) {
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)

	category, err := svc.Create(context.Background(), domain.CreateCategoryRequest{
		Name:        "Questions",
		DefaultTags: []string{" help ", "Help", "", "campus"},
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	if got := strings.Join(category.DefaultTags, ","); got != "help,campus" {
		t.Fatalf("unexpected default tags: %#v", category.DefaultTags)
	}
}

func TestCategoryHierarchyUsesReliableCommandsAndVersionConflicts(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryCategoryRepository()
	store := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(), store)
	svc := NewCategoryService(repo, nil)
	svc.SetReliability(reliable)

	group, err := svc.CreateForActor(ctx, "9001", domain.CreateCategoryRequest{
		Name: "Campus life", NodeKind: domain.CategoryNodeGroup, Color: "#1a2b3c80",
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.NodeKind != domain.CategoryNodeGroup || group.ParentID != nil || group.Color != "#1A2B3C80" || group.Version != 1 {
		t.Fatalf("unexpected created group: %#v", group)
	}

	board, err := svc.CreateForActor(ctx, "9001", domain.CreateCategoryRequest{
		Name: "Club activity", ParentID: &group.ID, NodeKind: domain.CategoryNodeBoard,
	})
	if err != nil {
		t.Fatalf("create child board: %v", err)
	}
	if board.ParentID == nil || *board.ParentID != group.ID {
		t.Fatalf("child board parent was not persisted: %#v", board)
	}

	updatedName := "Club and activity"
	updated, err := svc.UpdateForActor(ctx, "9001", board.ID, domain.UpdateCategoryRequest{
		Name: &updatedName, Version: board.Version,
	})
	if err != nil {
		t.Fatalf("update board: %v", err)
	}
	if updated.Version != board.Version+1 {
		t.Fatalf("expected version increment, before=%d after=%d", board.Version, updated.Version)
	}
	updatedDescription := "The board accepts club notices and discussion."
	updatedAgain, err := svc.UpdateForActor(ctx, "9001", board.ID, domain.UpdateCategoryRequest{
		Description: &updatedDescription, Version: updated.Version,
	})
	if err != nil {
		t.Fatalf("update board a second time: %v", err)
	}
	if updatedAgain.Version != updated.Version+1 {
		t.Fatalf("expected second version increment, before=%d after=%d", updated.Version, updatedAgain.Version)
	}
	if _, err := svc.MoveForActor(ctx, "9001", board.ID, domain.MoveCategoryRequest{
		ParentID: nil, ParentSpecified: true, Version: board.Version,
	}); !errors.Is(err, repository.ErrCategoryVersionConflict) {
		t.Fatalf("expected stale move to fail with version conflict, got %v", err)
	}
	fresh, err := repo.GetByID(ctx, board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ParentID == nil || *fresh.ParentID != group.ID || fresh.Version != updatedAgain.Version {
		t.Fatalf("stale move mutated board: %#v", fresh)
	}

	events, err := reliable.List(ctx, reliability.EventFilter{Limit: 20})
	if err != nil || len(events) != 4 {
		t.Fatalf("expected one durable event for each successful command, events=%#v err=%v", events, err)
	}
	audits, total, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil || total != 4 || len(audits) != 4 {
		t.Fatalf("expected four command audits, total=%d audits=%#v err=%v", total, audits, err)
	}
}

func TestCategoryArchiveBlocksNewThreadsAndReplies(t *testing.T) {
	ctx := context.Background()
	categoryRepo := repository.NewMemoryCategoryRepository()
	categorySvc := NewCategoryService(categoryRepo, nil)
	board, err := categorySvc.Create(ctx, domain.CreateCategoryRequest{Name: "Archived notice"})
	if err != nil {
		t.Fatal(err)
	}
	threadRepo := repository.NewMemoryThreadRepository()
	threadSvc := NewThreadService(threadRepo, nil)
	threadSvc.SetCategoryRepository(categoryRepo)
	thread, err := threadSvc.CreateThread(ctx, "1001", "alice", domain.CreateThreadRequest{
		Title: "existing thread", Content: "body", CategoryID: board.ID,
	})
	if err != nil {
		t.Fatalf("create existing thread: %v", err)
	}
	postSvc := NewPostService(repository.NewMemoryPostRepository(), nil)
	postSvc.SetThreadRepository(threadRepo)
	postSvc.SetCategoryRepository(categoryRepo)

	if _, err := categorySvc.Archive(ctx, board.ID, board.Version); err != nil {
		t.Fatalf("archive board: %v", err)
	}
	if _, err := threadSvc.CreateThread(ctx, "1002", "bob", domain.CreateThreadRequest{
		Title: "new thread", Content: "body", CategoryID: board.ID,
	}); !errors.Is(err, ErrCategoryPostingUnavailable) {
		t.Fatalf("archived board accepted a new thread: %v", err)
	}
	if _, err := postSvc.CreatePost(ctx, thread.ID, "1002", "bob", domain.CreatePostRequest{Content: "reply"}); !errors.Is(err, ErrCategoryPostingUnavailable) {
		t.Fatalf("archived board accepted a reply: %v", err)
	}
}

func TestCategoryArchiveRejectsActiveChildBoardsAndTreeHidesArchivedNodes(t *testing.T) {
	ctx := context.Background()
	svc := NewCategoryService(repository.NewMemoryCategoryRepository(), nil)
	group, err := svc.Create(ctx, domain.CreateCategoryRequest{Name: "Study", NodeKind: domain.CategoryNodeGroup})
	if err != nil {
		t.Fatal(err)
	}
	board, err := svc.Create(ctx, domain.CreateCategoryRequest{Name: "Exam", ParentID: &group.ID})
	if err != nil {
		t.Fatal(err)
	}
	impact, err := svc.ArchiveImpact(ctx, group.ID)
	if err != nil || impact.ActiveChildBoards != 1 || impact.WillBlockNewPosting {
		t.Fatalf("unexpected group archive impact: %#v err=%v", impact, err)
	}
	if _, err := svc.Archive(ctx, group.ID, group.Version); !errors.Is(err, ErrCategoryHierarchy) {
		t.Fatalf("expected archive to reject active child board, got %v", err)
	}
	if _, err := svc.Archive(ctx, board.ID, board.Version); err != nil {
		t.Fatalf("archive child board: %v", err)
	}
	tree, err := svc.ListTree(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 0 {
		t.Fatalf("public tree must hide archived child board: %#v", tree)
	}
	adminTree, err := svc.ListTree(ctx, true)
	if err != nil || len(adminTree) != 1 || len(adminTree[0].Children) != 1 {
		t.Fatalf("admin tree must retain archived child board: %#v err=%v", adminTree, err)
	}
}

func TestMoveCategoryRequestDistinguishesNullFromOmittedParent(t *testing.T) {
	var omitted domain.MoveCategoryRequest
	if err := json.Unmarshal([]byte(`{"version":1}`), &omitted); err == nil {
		t.Fatal("omitted parent_id must be rejected")
	}
	var root domain.MoveCategoryRequest
	if err := json.Unmarshal([]byte(`{"parent_id":null,"version":2}`), &root); err != nil {
		t.Fatalf("explicit null must be accepted: %v", err)
	}
	if !root.ParentSpecified || root.ParentID != nil || root.Version != 2 {
		t.Fatalf("unexpected root move request: %#v", root)
	}
}

func TestCategoryThreadTypePoliciesAreVersionedAndSeededForBoards(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryCategoryRepository()
	policies := repository.NewMemoryThreadTypePolicyRepository()
	store := reliability.NewMemoryStore()
	svc := NewCategoryService(repo, nil)
	svc.SetThreadTypePolicyRepository(policies)
	svc.SetReliability(reliability.NewService(transaction.NewMemory(), store))
	board, err := svc.CreateForActor(ctx, "9001", domain.CreateCategoryRequest{Name: "Marketplace"})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := svc.ListThreadTypePolicies(ctx, board.ID)
	if err != nil || len(seeded) != 2 || seeded[0].ThreadType != domain.ThreadTypeArticle || seeded[1].ThreadType != domain.ThreadTypeDiscussion {
		t.Fatalf("expected default article/discussion policies, items=%#v err=%v", seeded, err)
	}
	updated, configured, err := svc.UpdateThreadTypePoliciesForActor(ctx, "9001", board.ID, domain.UpdateCategoryThreadTypePolicyRequest{
		Version: board.Version, AllowedTypes: []domain.ThreadType{domain.ThreadTypeDiscussion, domain.ThreadTypeMutualAid},
	})
	if err != nil {
		t.Fatalf("configure policies: %v", err)
	}
	if updated.Version != board.Version+1 || len(configured) != 2 {
		t.Fatalf("unexpected policy update: category=%#v policies=%#v", updated, configured)
	}
	if _, _, err := svc.UpdateThreadTypePoliciesForActor(ctx, "9001", board.ID, domain.UpdateCategoryThreadTypePolicyRequest{
		Version: board.Version, AllowedTypes: []domain.ThreadType{domain.ThreadTypeDiscussion},
	}); !errors.Is(err, repository.ErrCategoryVersionConflict) {
		t.Fatalf("expected stale policy update conflict, got %v", err)
	}
}
