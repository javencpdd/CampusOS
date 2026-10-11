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

func ownerReadPrincipal() port.PrincipalContext {
	return port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "user", ID: "97101"},
		Audience: "user", AuthenticationStrength: "password", CredentialID: "verified-session"}
}

func ownerReadResolver(_ context.Context) (port.PrincipalContext, error) {
	return ownerReadPrincipal(), nil
}

type ownerReadRaceRepository struct {
	repository.ThreadRepository
	before   func(int)
	initial  func(*domain.Thread)
	final    func(*domain.Thread)
	rechecks int
}

func (r *ownerReadRaceRepository) GetByID(ctx context.Context, id string) (*domain.Thread, error) {
	row, err := r.ThreadRepository.GetByID(ctx, id)
	if err == nil && r.initial != nil {
		r.initial(row)
	}
	return row, err
}

func (r *ownerReadRaceRepository) RecheckOwnerThread(ctx context.Context, id, owner string, version int64) (*domain.Thread, error) {
	r.rechecks++
	if r.before != nil {
		r.before(r.rechecks)
	}
	row, err := r.ThreadRepository.(repository.OwnerThreadSnapshotRepository).RecheckOwnerThread(ctx, id, owner, version)
	if err == nil && r.final != nil {
		r.final(row)
	}
	return row, err
}

type recordingOwnerPolicy struct {
	requests []port.ThreadOwnerReadRequest
	corrupt  bool
}

func (p *recordingOwnerPolicy) DecideThreadOwnerRead(r port.ThreadOwnerReadRequest) (port.ThreadOwnerReadDecision, error) {
	p.requests = append(p.requests, r)
	d, err := (policy.ThreadOwnerReadPolicy{}).DecideThreadOwnerRead(r)
	if p.corrupt {
		d.Principal.CredentialID = "another-session"
	}
	return d, err
}

func ownerReadFixture(t *testing.T, repo repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	row := publicReadFixture(t, repo, id)
	row.PublicationStatus = "private"
	if err := repo.Update(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestOwnerThreadDetailStateMatrix(t *testing.T) {
	for _, pub := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(fmt.Sprintf("%s/%s/%s", pub, mod, del), func(t *testing.T) {
					ctx := context.Background()
					repo := repository.NewMemoryThreadRepository()
					row := ownerReadFixture(t, repo, "owner-state")
					row.PublicationStatus, row.ModerationStatus, row.DeletionStatus = pub, mod, del
					if err := repo.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
					got, err := NewThreadService(repo, nil).GetThreadForPrincipal(ctx, row.ID, ownerReadResolver)
					if del == "purged" {
						if !errors.Is(err, repository.ErrThreadNotFound) || got != nil {
							t.Fatalf("purged got=%v err=%v", got, err)
						}
						return
					}
					if err != nil || got == nil {
						t.Fatalf("owner got=%v err=%v", got, err)
					}
					wantViews := int64(0)
					if pub == "published" && mod == "clear" && del == "active" {
						wantViews = 1
					}
					if got.ViewCount != wantViews {
						t.Fatalf("views=%d want%d", got.ViewCount, wantViews)
					}
				})
			}
		}
	}
}

func TestOwnerThreadDetailMemoryRetryAndRevocation(t *testing.T) {
	for _, change := range []string{"owner", "purge", "content", "board", "aba", "trash", "counter"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			row := ownerReadFixture(t, repo, "owner-race")
			traced := &ownerReadRaceRepository{ThreadRepository: repo}
			traced.before = func(call int) {
				if call != 1 {
					return
				}
				current, err := repo.GetByID(ctx, row.ID)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "owner":
					current.AuthorID = "other-owner"
				case "purge":
					current.DeletionStatus = "purged"
				case "content":
					current.Content = "new private body"
				case "board":
					current.CategoryID = "another-board"
				case "aba":
					current.AuthorID = "other-owner"
					if err := repo.Update(ctx, current); err != nil {
						t.Fatal(err)
					}
					current.AuthorID = row.AuthorID
				case "trash":
					current.DeletionStatus = "trashed"
				case "counter":
					if _, err := repo.IncrementViewCount(ctx, row.ID); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := repo.Update(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			svc := NewThreadService(traced, nil)
			recorder := &recordingOwnerPolicy{}
			svc.ownerPolicy = recorder
			got, err := svc.GetThreadForPrincipal(ctx, row.ID, ownerReadResolver)
			if change == "owner" || change == "purge" {
				if !errors.Is(err, repository.ErrThreadNotFound) || got != nil {
					t.Fatalf("revoked got=%v err=%v", got, err)
				}
				return
			}
			if err != nil || got == nil {
				t.Fatalf("got=%v err=%v", got, err)
			}
			wantCalls := 2
			if change == "counter" {
				wantCalls = 1
			}
			if traced.rechecks != wantCalls || len(recorder.requests) != wantCalls {
				t.Fatalf("calls=%d decisions=%d", traced.rechecks, len(recorder.requests))
			}
			if change == "content" && got.Content != "new private body" {
				t.Fatal("returned stale body")
			}
			if change == "trash" && got.DeletionStatus != "trashed" {
				t.Fatal("returned stale trash state")
			}
			if change == "aba" && recorder.requests[0].Facts.FactsVersion == recorder.requests[1].Facts.FactsVersion {
				t.Fatal("ABA did not advance")
			}
			if wantCalls == 2 && recorder.requests[0].RequestID == recorder.requests[1].RequestID {
				t.Fatal("reused decision")
			}
		})
	}
}

func TestOwnerThreadDetailPrincipalAndFailureBoundaries(t *testing.T) {
	for _, fault := range []string{"missing-proof", "invalid-proof", "revoked-before", "revoked-after", "credential-change", "admin", "other-user", "state", "version", "decision", "persistent", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			row := ownerReadFixture(t, repo, "owner-fault")
			traced := &ownerReadRaceRepository{ThreadRepository: repo}
			resolves := 0
			resolve := func(ctx context.Context) (port.PrincipalContext, error) {
				resolves++
				p := ownerReadPrincipal()
				switch fault {
				case "invalid-proof":
					p.CredentialID = ""
				case "revoked-before":
					if resolves >= 2 {
						return p, errors.New("revoked secret session")
					}
				case "revoked-after":
					if resolves >= 3 {
						return p, errors.New("revoked secret session")
					}
				case "credential-change":
					if resolves >= 2 {
						p.CredentialID = "new-verified-session"
					}
				case "admin":
					p.Actor.Kind, p.Audience = "admin", "admin"
				case "other-user":
					p.Actor.ID = "another-user"
				}
				return p, nil
			}
			if fault == "missing-proof" {
				resolve = nil
			}
			traced.initial = func(row *domain.Thread) {
				if fault == "state" {
					row.PublicationStatus = ""
				}
				if fault == "version" {
					row.FactsVersion = 0
				}
			}
			traced.before = func(call int) {
				if fault == "persistent" {
					current, _ := repo.GetByID(ctx, row.ID)
					current.Content = fmt.Sprint(call)
					if err := repo.Update(ctx, current); err != nil {
						t.Fatal(err)
					}
				}
			}
			if fault == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				traced.before = func(int) { cancel() }
			}
			svc := NewThreadService(traced, nil)
			recorder := &recordingOwnerPolicy{corrupt: fault == "decision"}
			svc.ownerPolicy = recorder
			got, err := svc.GetThreadForPrincipal(ctx, row.ID, resolve)
			if fault == "credential-change" {
				if err != nil || got == nil || len(recorder.requests) != 2 {
					t.Fatalf("credential reload got=%v decisions=%d err=%v", got, len(recorder.requests), err)
				}
				return
			}
			if got != nil || err == nil {
				t.Fatalf("fault%s got=%v err=%v", fault, got, err)
			}
			if fault == "persistent" && (!errors.Is(err, port.ErrPolicyFactsChanged) || traced.rechecks != 3) {
				t.Fatalf("conflict calls=%d err=%v", traced.rechecks, err)
			}
			if fault == "admin" || fault == "other-user" {
				if !errors.Is(err, repository.ErrThreadNotFound) {
					t.Fatal(err)
				}
			}
			if fault == "missing-proof" || fault == "invalid-proof" || fault == "revoked-before" || fault == "revoked-after" {
				if !errors.Is(err, ErrThreadOwnerPrincipalUnavailable) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOwnerThreadDetailPublicBranchNeverFallsBack(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryThreadRepository()
	row := publicReadFixture(t, repo, "owner-public")
	traced := &publicReadRaceRepository{ThreadRepository: repo}
	traced.beforeRecheck = func() {
		traced.beforeRecheck = nil
		current, _ := repo.GetByID(ctx, row.ID)
		current.PublicationStatus = "private"
		current.Content = "new secret body"
		if err := repo.Update(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewThreadService(traced, nil).GetThreadForPrincipal(ctx, row.ID, ownerReadResolver)
	if got != nil || !errors.Is(err, repository.ErrThreadNotFound) {
		t.Fatalf("owner fallback got=%v err=%v", got, err)
	}
}
