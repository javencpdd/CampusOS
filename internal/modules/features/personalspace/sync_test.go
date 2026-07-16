package space

import (
	"context"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestSyncThreadUsesDefaultPublicSpace(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))

	thread := testThread("1001", "1", []string{"go"})
	if err := svc.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("sync thread: %v", err)
	}

	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list public contents: %v", err)
	}
	if total != 1 || len(contents) != 1 {
		t.Fatalf("expected one synced content, total=%d len=%d", total, len(contents))
	}
	if contents[0].ThreadID != thread.ID {
		t.Fatalf("expected thread id %q, got %q", thread.ID, contents[0].ThreadID)
	}
	if contents[0].Excerpt == "" {
		t.Fatalf("expected excerpt")
	}
}

func TestSyncThreadAppliesCategoryAndTagFilters(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	visibility := "public"
	if _, err := svc.UpsertOwnSpace(context.Background(), "1001", UpsertSpaceRequest{
		Visibility:     &visibility,
		SyncCategories: []string{"2"},
		SyncTags:       []string{"campus"},
	}); err != nil {
		t.Fatalf("upsert own space: %v", err)
	}

	if err := svc.SyncThread(context.Background(), testThread("1001", "1", []string{"campus"})); err != nil {
		t.Fatalf("sync non-matching category: %v", err)
	}
	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 0 || len(contents) != 0 {
		t.Fatalf("expected no content after category mismatch, total=%d len=%d", total, len(contents))
	}

	if err := svc.SyncThread(context.Background(), testThread("1001", "2", []string{"other"})); err != nil {
		t.Fatalf("sync non-matching tag: %v", err)
	}
	contents, total, err = svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 0 || len(contents) != 0 {
		t.Fatalf("expected no content after tag mismatch, total=%d len=%d", total, len(contents))
	}

	thread := testThread("1001", "2", []string{"Campus"})
	if err := svc.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("sync matching thread: %v", err)
	}
	contents, total, err = svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 1 || contents[0].ThreadID != thread.ID {
		t.Fatalf("expected matching content, total=%d contents=%#v", total, contents)
	}
}

func TestHandleThreadEventDecodesMapPayload(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))

	err := svc.HandleThreadEvent(context.Background(), eventbus.Event{
		Type: eventbus.EventThreadCreated,
		Data: map[string]interface{}{
			"id":          "2001",
			"title":       "hello",
			"content":     "hello campus",
			"author_id":   "1001",
			"author_name": "Alice",
			"category_id": "1",
			"status":      "published",
			"tags":        []interface{}{"go"},
			"created_at":  time.Now().UTC(),
			"updated_at":  time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("handle thread event: %v", err)
	}

	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	if total != 1 || contents[0].ThreadID != "2001" {
		t.Fatalf("expected decoded content, total=%d contents=%#v", total, contents)
	}
}

func TestListPublicContentsBackfillsExistingThreads(t *testing.T) {
	repo := NewMemoryRepository()
	threadRepo := communityrepo.NewMemoryThreadRepository()
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}))
	svc.SetThreadRepository(threadRepo)

	thread := testThread("1001", "1", []string{"go"})
	if err := threadRepo.Create(context.Background(), thread); err != nil {
		t.Fatalf("create thread: %v", err)
	}

	contents, total, err := svc.ListPublicContentsByUserID(context.Background(), "1001", 1, 20)
	if err != nil {
		t.Fatalf("list public contents: %v", err)
	}
	if total != 1 || len(contents) != 1 {
		t.Fatalf("expected backfilled content, total=%d len=%d", total, len(contents))
	}
	if contents[0].ThreadID != thread.ID {
		t.Fatalf("expected backfilled thread id %q, got %q", thread.ID, contents[0].ThreadID)
	}
}

func TestSyncThreadNormalizesNilTags(t *testing.T) {
	repo := NewMemoryRepository()
	contentRepo := &capturingContentRepository{}
	svc := NewService(repo, newFakeUserLookup(&identitydomain.User{
		ID:       "1001",
		Username: "alice",
		Nickname: "Alice",
	}), contentRepo)

	thread := testThread("1001", "1", nil)
	if err := svc.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("sync thread: %v", err)
	}

	if contentRepo.last == nil {
		t.Fatalf("expected synced content")
	}
	if contentRepo.last.Tags == nil {
		t.Fatalf("expected nil thread tags to be normalized to an empty slice")
	}
	if len(contentRepo.last.Tags) != 0 {
		t.Fatalf("expected empty tags, got %#v", contentRepo.last.Tags)
	}
}

func TestListOwnContentsUsesAuthorFactAndKeepsGovernanceDetailsPrivate(t *testing.T) {
	user := &identitydomain.User{ID: "1001", Username: "alice", Nickname: "Alice"}
	query := &capturingContentQuery{authorThreads: []*communitydomain.Thread{{
		ID:                "2002",
		Title:             "Corrected article",
		Content:           "author-only moderation status",
		AuthorID:          user.ID,
		AuthorName:        user.Nickname,
		CategoryID:        "2",
		Status:            communitydomain.ThreadStatusArchived,
		PublicationStatus: communitydomain.PublicationStatusPublished,
		ModerationStatus:  communitydomain.ModerationStatusTakenDown,
		DeletionStatus:    communitydomain.DeletionStatusActive,
		ModerationReason:  "please add the source",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}}}
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(user))
	svc.SetContentQuery(query)

	contents, total, err := svc.ListOwnContents(context.Background(), user.ID, 1, 20)
	if err != nil {
		t.Fatalf("list own contents: %v", err)
	}
	if total != 1 || len(contents) != 1 {
		t.Fatalf("expected one author content item, total=%d contents=%#v", total, contents)
	}
	if contents[0].ModerationStatus != string(communitydomain.ModerationStatusTakenDown) || contents[0].ModerationReason != "please add the source" {
		t.Fatalf("owner must receive canonical governance state: %#v", contents[0])
	}
	if query.authorCalls != 1 || query.publicCalls != 0 {
		t.Fatalf("owner view must use author fact query only: %#v", query)
	}
}

func testThread(authorID, categoryID string, tags []string) *communitydomain.Thread {
	now := time.Now().UTC()
	return &communitydomain.Thread{
		ID:         "2001",
		Title:      "CampusOS Sync",
		Content:    "CampusOS content sync keeps a public personal space updated.",
		AuthorID:   authorID,
		AuthorName: "Alice",
		CategoryID: categoryID,
		Status:     communitydomain.ThreadStatusPublished,
		Tags:       tags,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

type capturingContentRepository struct {
	last *SpaceContent
}

var _ communityport.ContentQuery = (*capturingContentQuery)(nil)

type capturingContentQuery struct {
	publicCalls   int
	authorCalls   int
	authorThreads []*communitydomain.Thread
}

func (q *capturingContentQuery) GetPublicThread(_ context.Context, _ string) (*communitydomain.Thread, error) {
	q.publicCalls++
	return nil, communityrepo.ErrThreadNotFound
}

func (q *capturingContentQuery) ListPublicThreads(_ context.Context, _ communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	q.publicCalls++
	return []*communitydomain.Thread{}, 0, nil
}

func (q *capturingContentQuery) ListAuthorThreads(_ context.Context, _ string, _ communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	q.authorCalls++
	return q.authorThreads, int64(len(q.authorThreads)), nil
}

func (r *capturingContentRepository) UpsertContent(_ context.Context, content *SpaceContent) error {
	copied := *content
	r.last = &copied
	return nil
}

func (r *capturingContentRepository) DeleteContent(_ context.Context, _ string) error {
	return nil
}

func (r *capturingContentRepository) ListContentsByUserID(_ context.Context, _ string, _, _ int) ([]*SpaceContent, int64, error) {
	return []*SpaceContent{}, 0, nil
}
