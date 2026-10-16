package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type publicReadRaceRepository struct {
	repository.ThreadRepository
	beforeRecheck func()
	initial       func(*domain.Thread)
	final         func(*domain.Thread)
	rechecks      int
}

func (r *publicReadRaceRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	t, err := r.ThreadRepository.GetByID(ctx, id)
	if err == nil && r.initial != nil {
		r.initial(t)
	}
	return t, err
}

func (r *publicReadRaceRepository) RecheckPublicThread(ctx context.Context, id string, version int64, view bool) (*domain.Thread, error) {
	r.rechecks++
	if r.beforeRecheck != nil {
		r.beforeRecheck()
	}
	t, err := r.ThreadRepository.(repository.PublicThreadSnapshotRepository).RecheckPublicThread(ctx, id, version, view)
	if err == nil && r.final != nil {
		r.final(t)
	}
	return t, err
}

type recordingPublicPolicy struct {
	requests []port.ThreadPublicReadRequest
	corrupt  bool
}

func (p *recordingPublicPolicy) DecideThreadPublicRead(r port.ThreadPublicReadRequest) (port.ThreadPublicReadDecision, error) {
	p.requests = append(p.requests, r)
	d, err := (policy.ThreadPublicReadPolicy{}).DecideThreadPublicRead(r)
	if p.corrupt {
		d.RequestID = "other-request"
	}
	return d, err
}

func publicReadFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	thread := &domain.Thread{ID: id, AuthorID: "97101", CategoryID: "97111", Title: "public title", Content: "old public body",
		Status: domain.ThreadStatusPublished, PublicationStatus: domain.PublicationStatusPublished,
		ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive, CurrentRevision: 1}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestPublicThreadDetailPolicyStateMatrix(t *testing.T) {
	ctx := context.Background()
	for _, publication := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(fmt.Sprintf("%s/%s/%s", publication, moderation, deletion), func(t *testing.T) {
					repo := repository.NewMemoryThreadRepository()
					thread := publicReadFixture(t, repo, "state-case")
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					got, err := NewThreadService(repo, nil).GetThread(ctx, thread.ID)
					allowed := publication == "published" && moderation == "clear" && deletion == "active"
					if allowed && (err != nil || got == nil || got.ViewCount != 1) {
						t.Fatalf("public detail: got=%+v err=%v", got, err)
					}
					if !allowed && (!errors.Is(err, repository.ErrThreadNotFound) || got != nil) {
						t.Fatalf("nonpublic detail returned: got=%+v err=%v", got, err)
					}
				})
			}
		}
	}
}

// A writer commits between the first row snapshot and final read. Both public
// APIs must refuse the changed body; the owner has no public-read exception.
func exercisePublicReadRevocation(t *testing.T, repo repository.ThreadRepository, id string) {
	t.Helper()
	ctx := context.Background()
	caseIndex := 0
	for _, change := range []string{"private", "taken_down", "trashed"} {
		for _, path := range []string{"public", "interactive", "owner-public"} {
			caseIndex++
			caseID := fmt.Sprintf("%s%d", id, caseIndex)
			t.Run(change+"/"+path, func(t *testing.T) {
				id := caseID
				thread := publicReadFixture(t, repo, id)
				ready, done := make(chan struct{}), make(chan error, 1)
				go func() {
					<-ready
					current, err := repo.GetByID(ctx, thread.ID)
					if err == nil {
						current.Content = "secret after revocation"
						switch change {
						case "private":
							current.PublicationStatus = "private"
						case "taken_down":
							current.ModerationStatus = "taken_down"
						case "trashed":
							current.DeletionStatus = "trashed"
						}
						err = repo.Update(ctx, current)
					}
					done <- err
				}()
				raced := &publicReadRaceRepository{ThreadRepository: repo}
				raced.beforeRecheck = func() {
					raced.beforeRecheck = nil
					close(ready)
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				svc := NewThreadService(raced, nil)
				var got *domain.Thread
				var err error
				switch path {
				case "interactive":
					got, err = svc.GetThread(ctx, id)
				case "owner-public":
					got, err = svc.GetThreadForViewer(ctx, id, thread.AuthorID)
				default:
					got, err = svc.GetPublicThread(ctx, id)
				}
				if !errors.Is(err, repository.ErrThreadNotFound) || got != nil {
					t.Fatalf("revoked body returned: got=%+v err=%v", got, err)
				}
				stored, err := repo.GetByID(ctx, id)
				if err != nil || stored.ViewCount != 0 {
					t.Fatalf("rejected read inflated views: stored=%+v err=%v", stored, err)
				}
				if err := repo.Purge(ctx, id); err != nil {
					// PostgreSQL Purge requires trashed state.
					stored.DeletionStatus = "trashed"
					if err = repo.Update(ctx, stored); err != nil {
						t.Fatal(err)
					}
					if err = repo.Purge(ctx, id); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPublicThreadDetailMemoryRevocation(t *testing.T) {
	exercisePublicReadRevocation(t, repository.NewMemoryThreadRepository(), "race-thread")
}

func TestPublicThreadDetailRetryReloadsFactsAndDecision(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"content", "board", "owner", "aba"} {
		t.Run(kind, func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			thread := publicReadFixture(t, repo, "retry-thread")
			raced := &publicReadRaceRepository{ThreadRepository: repo}
			raced.beforeRecheck = func() {
				raced.beforeRecheck = nil
				current, _ := repo.GetByID(ctx, thread.ID)
				switch kind {
				case "content":
					current.Content = "new public body"
				case "board":
					current.CategoryID = "97112"
				case "owner":
					current.AuthorID = "97102"
				case "aba":
					current.PublicationStatus = "private"
					if err := repo.Update(ctx, current); err != nil {
						t.Fatal(err)
					}
					current.PublicationStatus = "published"
				}
				if err := repo.Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			svc := NewThreadService(raced, nil)
			engine := &recordingPublicPolicy{}
			svc.publicPolicy = engine
			got, err := svc.GetThread(ctx, thread.ID)
			if err != nil || got.ViewCount != 1 || len(engine.requests) != 2 {
				t.Fatalf("fresh retry: got=%+v err=%v requests=%d", got, err, len(engine.requests))
			}
			first, second := engine.requests[0], engine.requests[1]
			if first.RequestID == second.RequestID || first.Facts.FactsVersion == second.Facts.FactsVersion {
				t.Fatal("retry reused a decision or ABA version")
			}
			if kind == "content" && got.Content != "new public body" {
				t.Fatal("returned old body")
			}
			if kind == "board" && second.Facts.Board.ID != "97112" {
				t.Fatal("reused client/old board")
			}
			if kind == "owner" && second.Facts.Owner.ID != "97102" {
				t.Fatal("reused old owner")
			}
		})
	}
}

func TestPublicThreadDetailFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, mutation := range []string{"initial-status", "initial-version", "final-owner", "final-version", "decision"} {
		t.Run(mutation, func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			publicReadFixture(t, repo, "invalid-thread")
			raced := &publicReadRaceRepository{ThreadRepository: repo}
			svc := NewThreadService(raced, nil)
			switch mutation {
			case "initial-status":
				raced.initial = func(t *domain.Thread) { t.PublicationStatus = "" }
			case "initial-version":
				raced.initial = func(t *domain.Thread) { t.FactsVersion = 0 }
			case "final-owner":
				raced.final = func(t *domain.Thread) { t.AuthorID = "forged-owner" }
			case "final-version":
				raced.final = func(t *domain.Thread) { t.FactsVersion++ }
			case "decision":
				svc.publicPolicy = &recordingPublicPolicy{corrupt: true}
			}
			got, err := svc.GetPublicThread(ctx, "invalid-thread")
			if got != nil || err == nil {
				t.Fatalf("invalid facts/decision returned: %+v %v", got, err)
			}
			if mutation == "decision" && raced.rechecks != 0 {
				t.Fatal("bad decision consumed")
			}
		})
	}
	// Every attempt races with a public edit. Bound work and return conflict.
	repo := repository.NewMemoryThreadRepository()
	publicReadFixture(t, repo, "busy-thread")
	raced := &publicReadRaceRepository{ThreadRepository: repo}
	raced.beforeRecheck = func() {
		current, _ := repo.GetByID(ctx, "busy-thread")
		current.Content += "!"
		if err := repo.Update(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewThreadService(raced, nil).GetThread(ctx, "busy-thread")
	if got != nil || !errors.Is(err, port.ErrPolicyFactsChanged) || raced.rechecks != 3 {
		t.Fatalf("unbounded/unsafe retry: got=%+v err=%v attempts=%d", got, err, raced.rechecks)
	}
}
