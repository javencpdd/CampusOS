package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type publicListRaceRepository struct {
	repository.ThreadRepository
	calls  int
	before func(int)
	after  func(int, []*domain.Thread, *int64) ([]*domain.Thread, error)
}

func (r *publicListRaceRepository) List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	if r.before != nil {
		r.before(r.calls)
	}
	rows, total, err := r.ThreadRepository.List(ctx, filter)
	if err == nil && r.after != nil {
		rows, err = r.after(r.calls, rows, &total)
	}
	return rows, total, err
}

func publicListFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	thread := publicReadFixture(t, repo, id)
	thread.CreatedAt = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	if err := repo.Update(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestPublicThreadListStateMatrixAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryThreadRepository()
	id := 1000
	for _, pub := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				id++
				row := publicListFixture(t, repo, fmt.Sprint(id))
				row.PublicationStatus, row.ModerationStatus, row.DeletionStatus = pub, mod, del
				if err := repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	svc := NewThreadService(repo, nil)
	rows, total, err := svc.ListPublicThreads(ctx, domain.ThreadListFilter{Status: "all", PublicationStatus: "private", ModerationStatus: "taken_down", DeletionStatus: "trashed", IncludeTrashed: true})
	if err != nil || total != 1 || len(rows) != 1 || !rows[0].IsPublic() {
		t.Fatalf("matrix: rows=%v total=%d err=%v", rows, total, err)
	}
	for _, id := range []string{"2001", "2002", "2003"} {
		publicListFixture(t, repo, id)
	}
	for page, expected := range map[int][]string{1: {"2003", "2002"}, 2: {"2001", "1013"}, 3: {}} {
		rows, total, err := svc.ListPublicThreads(ctx, domain.ThreadListFilter{Page: page, PageSize: 2})
		if err != nil || total != 4 || len(rows) != len(expected) {
			t.Fatalf("page%d rows=%v total=%d err=%v", page, rows, total, err)
		}
		for i, id := range expected {
			if rows[i].ID != id {
				t.Fatalf("page%d got%s want%s", page, rows[i].ID, id)
			}
		}
	}
}

func TestPublicThreadListRetryWholeSnapshot(t *testing.T) {
	for _, change := range []string{"private", "taken_down", "trashed", "content", "aba", "member", "order", "counter", "empty-growth", "empty-shrink"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			for _, id := range []string{"3001", "3002", "3003"} {
				publicListFixture(t, repo, id)
			}
			filter := domain.ThreadListFilter{Page: 1, PageSize: 2}
			if change == "empty-growth" || change == "empty-shrink" {
				filter.Page = 3
			}
			traced := &publicListRaceRepository{ThreadRepository: repo}
			traced.before = func(call int) {
				if call != 2 {
					return
				}
				current, err := repo.GetByID(ctx, "3003")
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "private":
					current.PublicationStatus = "private"
				case "taken_down":
					current.ModerationStatus = "taken_down"
				case "trashed":
					current.DeletionStatus = "trashed"
				case "content":
					current.Content = "fresh body"
				case "aba":
					current.PublicationStatus = "private"
					if err := repo.Update(ctx, current); err != nil {
						t.Fatal(err)
					}
					current.PublicationStatus = "published"
				case "member":
					current.CategoryID = "other-board"
				case "order":
					current, _ = repo.GetByID(ctx, "3001")
					current.IsPinned = true
				case "counter":
					if _, err := repo.IncrementViewCount(ctx, current.ID); err != nil {
						t.Fatal(err)
					}
					return
				case "empty-growth":
					publicListFixture(t, repo, "3004")
					publicListFixture(t, repo, "3005")
					return
				case "empty-shrink":
					current.PublicationStatus = "private"
				}
				if err := repo.Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			if change == "member" {
				filter.CategoryID = "97111"
			}
			svc := NewThreadService(traced, nil)
			recorder := &recordingPublicPolicy{}
			svc.publicPolicy = recorder
			rows, total, err := svc.ListPublicThreads(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 4
			if change == "counter" {
				wantCalls = 2
			}
			if traced.calls != wantCalls {
				t.Fatalf("calls=%d want%d", traced.calls, wantCalls)
			}
			wantTotal := int64(3)
			if change == "private" || change == "taken_down" || change == "trashed" || change == "member" || change == "empty-shrink" {
				wantTotal = 2
			}
			if change == "empty-growth" {
				wantTotal = 5
			}
			if total != wantTotal {
				t.Fatalf("total=%d want%d", total, wantTotal)
			}
			if change == "order" && rows[0].ID != "3001" {
				t.Fatal("returned stale order")
			}
			if change == "content" && rows[0].Content != "fresh body" {
				t.Fatal("returned stale body")
			}
			if change == "counter" && rows[0].ViewCount != 1 {
				t.Fatal("did not return final counters")
			}
			if change == "empty-growth" && (len(rows) != 1 || rows[0].ID != "3001") {
				t.Fatal("empty page was not re-read")
			}
			seen := map[string]bool{}
			for _, r := range recorder.requests {
				if seen[r.RequestID] {
					t.Fatal("reused request decision")
				}
				seen[r.RequestID] = true
			}
		})
	}
}

func TestPublicThreadListFailsClosed(t *testing.T) {
	for _, fault := range []string{"nil", "duplicate", "count", "state", "owner", "version", "decision", "repository"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			publicListFixture(t, repo, "4001")
			publicListFixture(t, repo, "4002")
			traced := &publicListRaceRepository{ThreadRepository: repo}
			traced.after = func(call int, rows []*domain.Thread, total *int64) ([]*domain.Thread, error) {
				if call != 1 {
					return rows, nil
				}
				switch fault {
				case "nil":
					rows[0] = nil
				case "duplicate":
					rows[1] = rows[0]
				case "count":
					*total = 3
				case "state":
					rows[0].PublicationStatus = ""
				case "owner":
					rows[0].AuthorID = ""
				case "version":
					rows[0].FactsVersion = 0
				case "repository":
					return nil, errors.New("internal sensitive database failure")
				}
				return rows, nil
			}
			svc := NewThreadService(traced, nil)
			svc.publicPolicy = &recordingPublicPolicy{corrupt: fault == "decision"}
			rows, total, err := svc.ListPublicThreads(ctx, domain.ThreadListFilter{})
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("fault %s returned rows=%v total=%d err=%v", fault, rows, total, err)
			}
		})
	}
}

func TestPublicThreadListSustainedConflictAndCancellation(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryThreadRepository()
	publicListFixture(t, repo, "5001")
	traced := &publicListRaceRepository{ThreadRepository: repo}
	traced.before = func(call int) {
		if call%2 == 0 {
			row, _ := repo.GetByID(ctx, "5001")
			row.Content = fmt.Sprint(call)
			if err := repo.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
		}
	}
	svc := NewThreadService(traced, nil)
	recorder := &recordingPublicPolicy{}
	svc.publicPolicy = recorder
	rows, total, err := svc.ListPublicThreads(ctx, domain.ThreadListFilter{})
	if !errors.Is(err, port.ErrPolicyFactsChanged) || rows != nil || total != 0 || traced.calls != 6 || len(recorder.requests) != 3 {
		t.Fatalf("conflict: rows=%v total=%d calls=%d decisions=%d err=%v", rows, total, traced.calls, len(recorder.requests), err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	traced.calls = 0
	if _, _, err = svc.ListPublicThreads(ctx, domain.ThreadListFilter{}); !errors.Is(err, context.Canceled) || traced.calls != 0 {
		t.Fatalf("cancel err=%v calls=%d", err, traced.calls)
	}
}

func TestPublicThreadListDispatchAndBoundaries(t *testing.T) {
	for _, path := range []string{"all", "legacy-published", "canonical"} {
		t.Run(path, func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			publicListFixture(t, repo, "6001")
			traced := &publicListRaceRepository{ThreadRepository: repo}
			svc := NewThreadService(traced, nil)
			recorder := &recordingPublicPolicy{}
			svc.publicPolicy = recorder
			filter := domain.ThreadListFilter{Status: "published"}
			wantCalls, wantDecisions := 1, 0
			switch path {
			case "all":
				filter.Status = "all"
			case "canonical":
				filter.PublicationStatus, filter.ModerationStatus, filter.DeletionStatus = "published", "clear", "active"
				wantCalls, wantDecisions = 2, 1
			}
			if _, _, err := svc.ListThreads(context.Background(), filter); err != nil {
				t.Fatal(err)
			}
			if traced.calls != wantCalls || len(recorder.requests) != wantDecisions {
				t.Fatalf("calls=%d decisions=%d", traced.calls, len(recorder.requests))
			}
		})
	}
	for _, afterCall := range []int{1, 2} {
		t.Run(fmt.Sprintf("cancel-after-snapshot-%d", afterCall), func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			publicListFixture(t, repo, "6002")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			traced := &publicListRaceRepository{ThreadRepository: repo}
			traced.after = func(call int, rows []*domain.Thread, _ *int64) ([]*domain.Thread, error) {
				if call == afterCall {
					cancel()
				}
				return rows, nil
			}
			rows, total, err := NewThreadService(traced, nil).ListPublicThreads(ctx, domain.ThreadListFilter{})
			if !errors.Is(err, context.Canceled) || rows != nil || total != 0 || traced.calls != afterCall {
				t.Fatalf("rows=%v total=%d calls=%d err=%v", rows, total, traced.calls, err)
			}
		})
	}
	traced := &publicListRaceRepository{ThreadRepository: repository.NewMemoryThreadRepository()}
	if _, _, err := NewThreadService(traced, nil).ListPublicThreads(context.Background(), domain.ThreadListFilter{Page: math.MaxInt, PageSize: 100}); !errors.Is(err, repository.ErrThreadPaginationOverflow) || traced.calls != 0 {
		t.Fatalf("overflow err=%v calls=%d", err, traced.calls)
	}
}
