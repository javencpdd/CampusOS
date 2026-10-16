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

type ownerListMemoryHook struct {
	repository.ThreadRepository
	calls   int
	before  func(int)
	after   func(int, []*domain.Thread, *int64) ([]*domain.Thread, error)
	filters []domain.ThreadListFilter
}

func (r *ownerListMemoryHook) ListOwnerThreadSnapshot(ctx context.Context, ownerID string, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	r.filters = append(r.filters, filter)
	if r.before != nil {
		r.before(r.calls)
	}
	rows, total, err := r.ThreadRepository.(repository.OwnerThreadListSnapshotRepository).ListOwnerThreadSnapshot(ctx, ownerID, filter)
	if err == nil && r.after != nil {
		rows, err = r.after(r.calls, rows, &total)
	}
	return rows, total, err
}
func ownerListMemoryFixture(t *testing.T, r repository.ThreadRepository, id string) *domain.Thread {
	t.Helper()
	row := ownerReadFixture(t, r, id)
	row.CreatedAt = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if err := r.Update(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestOwnerThreadListStateMatrixAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryThreadRepository()
	id := 6000
	for _, pub := range []domain.PublicationStatus{"draft", "published", "private"} {
		for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				id++
				row := ownerListMemoryFixture(t, repo, fmt.Sprint(id))
				row.PublicationStatus, row.ModerationStatus, row.DeletionStatus = pub, mod, del
				row.Status = "published" // Legacy projection does not authorize or remove raw states.
				if err := repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	other := ownerListMemoryFixture(t, repo, "6999")
	other.AuthorID = "other-user"
	if err := repo.Update(ctx, other); err != nil {
		t.Fatal(err)
	}
	svc := NewThreadService(repo, nil)
	record := &recordingOwnerPolicy{}
	svc.ownerPolicy = record
	filter := domain.ThreadListFilter{AuthorID: "other-user", Status: "all", PublicationStatus: "published", ModerationStatus: "taken_down", DeletionStatus: "trashed", IncludeTrashed: true, Page: 1, PageSize: 5}
	for _, page := range []int{1, 2, 3, 4} {
		filter.Page = page
		rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerReadResolver, filter)
		want := 5
		if page == 3 {
			want = 2
		}
		if page == 4 {
			want = 0
		}
		if err != nil || total != 12 || len(rows) != want {
			t.Fatalf("page%d rows=%v total=%d err=%v", page, rows, total, err)
		}
		for _, row := range rows {
			if row.AuthorID != "97101" || row.DeletionStatus != "active" || row.ViewCount != 0 {
				t.Fatalf("widened predicate or read side effect: %+v", row)
			}
		}
	}
	if len(record.requests) != 12 {
		t.Fatalf("policy decision count %d", len(record.requests))
	}
	for _, r := range record.requests {
		if r.Principal != ownerReadPrincipal() {
			t.Fatal("page used untrusted principal")
		}
	}
}

func TestOwnerThreadListMemoryRetryWholePage(t *testing.T) {
	for _, change := range []string{"owner", "purge", "trash", "body", "state-aba", "owner-aba", "order", "member", "off-page-total", "empty-growth", "empty-shrink", "counter"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			for _, id := range []string{"7101", "7102", "7103"} {
				ownerListMemoryFixture(t, repo, id)
			}
			filter := domain.ThreadListFilter{Page: 1, PageSize: 2, CategoryID: "97111"}
			if change == "empty-growth" {
				filter.Page = 3
			}
			if change == "empty-shrink" {
				filter.Page = 2
			}
			raced := &ownerListMemoryHook{ThreadRepository: repo}
			raced.before = func(call int) {
				if call != 2 {
					return
				}
				row, err := repo.GetByID(ctx, "7103")
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "owner":
					row.AuthorID = "other-user"
					row.Content = "other owner secret"
				case "purge":
					row.DeletionStatus = "purged"
				case "trash":
					row.DeletionStatus = "trashed"
				case "body":
					row.Content = "fresh private body"
				case "state-aba":
					row.PublicationStatus = "published"
					if err := repo.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
					row.PublicationStatus = "private"
				case "owner-aba":
					row.AuthorID = "other-user"
					if err := repo.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
					row.AuthorID = "97101"
				case "order":
					row, _ = repo.GetByID(ctx, "7101")
					row.IsPinned = true
				case "member":
					row.CategoryID = "other-board"
				case "off-page-total":
					row, _ = repo.GetByID(ctx, "7101")
					row.AuthorID = "other-user"
				case "empty-growth":
					ownerListMemoryFixture(t, repo, "7104")
					ownerListMemoryFixture(t, repo, "7105")
					return
				case "empty-shrink":
					row.AuthorID = "other-user"
				case "counter":
					if _, err := repo.IncrementViewCount(ctx, row.ID); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			svc := NewThreadService(raced, nil)
			record := &recordingOwnerPolicy{}
			svc.ownerPolicy = record
			rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerReadResolver, filter)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 4
			wantTotal := int64(3)
			if change == "counter" {
				wantCalls = 2
			}
			switch change {
			case "owner", "purge", "trash", "member", "off-page-total", "empty-shrink":
				wantTotal = 2
			case "empty-growth":
				wantTotal = 5
			}
			if raced.calls != wantCalls || total != wantTotal {
				t.Fatalf("calls=%d total=%d want%d/%d", raced.calls, total, wantCalls, wantTotal)
			}
			if change == "body" && rows[0].Content != "fresh private body" {
				t.Fatal("returned old private body")
			}
			if change == "order" && rows[0].ID != "7101" {
				t.Fatal("returned old order")
			}
			if change == "counter" && rows[0].ViewCount != 1 {
				t.Fatal("not final counter snapshot")
			}
			if change == "empty-growth" && (len(rows) != 1 || rows[0].ID != "7101") {
				t.Fatal("empty growth was not reauthorized")
			}
			if change == "empty-shrink" && len(rows) != 0 {
				t.Fatal("empty shrink leaked prior page")
			}
			seen := map[string]bool{}
			for _, r := range record.requests {
				if seen[r.RequestID] {
					t.Fatal("retried old request decision")
				}
				seen[r.RequestID] = true
			}
			for _, f := range raced.filters {
				if f.AuthorID != "97101" || f.Status != "" || f.PublicationStatus != "" || f.ModerationStatus != "" || f.DeletionStatus != "active" || f.IncludeTrashed {
					t.Fatal("query not canonical owner")
				}
			}
		})
	}
}

func TestOwnerThreadListPrincipalAndEmptyPageBoundaries(t *testing.T) {
	for _, test := range []string{"nil", "error", "admin", "anonymous", "invalid", "revoked-before", "revoked-after", "empty-before", "empty-after", "actor-swap", "bound-drift"} {
		t.Run(test, func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			if test != "empty-before" && test != "empty-after" {
				ownerListMemoryFixture(t, repo, "7201")
			}
			calls := 0
			resolve := func(context.Context) (port.PrincipalContext, error) {
				calls++
				p := ownerReadPrincipal()
				switch test {
				case "error":
					return p, errors.New("secret proof failure")
				case "admin":
					p.Actor.Kind = "admin"
					p.Audience = "admin"
				case "anonymous":
					p = port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}
				case "invalid":
					p.CredentialID = ""
				case "revoked-before", "empty-before":
					if calls >= 2 {
						return p, errors.New("revoked")
					}
				case "revoked-after", "empty-after":
					if calls >= 3 {
						return p, errors.New("revoked")
					}
				case "actor-swap":
					if calls >= 2 {
						p.Actor.ID = "other-user"
					}
				case "bound-drift":
					if calls >= 2 {
						return p, port.ErrPolicyFactsChanged
					}
				}
				return p, nil
			}
			if test == "nil" {
				resolve = nil
			}
			rows, total, err := NewThreadService(repo, nil).ListThreadsForPrincipal(context.Background(), resolve, domain.ThreadListFilter{Page: 1, PageSize: 2})
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("identity failure returned page/count: %v %d %v", rows, total, err)
			}
			if test == "bound-drift" {
				if !errors.Is(err, port.ErrPolicyFactsChanged) || errors.Is(err, port.ErrUserSessionPrincipalUnavailable) {
					t.Fatalf("valid proof drift must be 409: %v", err)
				}
			} else if !errors.Is(err, port.ErrUserSessionPrincipalUnavailable) {
				t.Fatalf("wrong proof boundary %v", err)
			}
		})
	}
	repo := repository.NewMemoryThreadRepository()
	ownerListMemoryFixture(t, repo, "7201")
	calls := 0
	resolve := func(context.Context) (port.PrincipalContext, error) {
		calls++
		p := ownerReadPrincipal()
		if calls >= 2 {
			p.AuthenticationStrength = "mfa"
		}
		return p, nil
	}
	svc := NewThreadService(repo, nil)
	record := &recordingOwnerPolicy{}
	svc.ownerPolicy = record
	rows, total, err := svc.ListThreadsForPrincipal(context.Background(), resolve, domain.ThreadListFilter{})
	if err != nil || total != 1 || len(rows) != 1 || len(record.requests) != 2 || record.requests[0].Principal.AuthenticationStrength != "password" || record.requests[1].Principal.AuthenticationStrength != "mfa" {
		t.Fatalf("strength drift not new decision: %+v %d %v %+v", rows, total, err, record.requests)
	}
}

func TestOwnerThreadListFailsClosedAndBounded(t *testing.T) {
	for _, fault := range []string{"bad-decision", "initial-fault", "final-fault", "nil-row", "duplicate", "wrong-count", "raw-missing", "foreign-owner", "trash-row", "persistent", "overflow", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			repo := repository.NewMemoryThreadRepository()
			ownerListMemoryFixture(t, repo, "7301")
			raced := &ownerListMemoryHook{ThreadRepository: repo}
			svc := NewThreadService(raced, nil)
			record := &recordingOwnerPolicy{corrupt: fault == "bad-decision"}
			svc.ownerPolicy = record
			if fault == "persistent" {
				raced.before = func(call int) {
					if call%2 == 0 {
						row, _ := repo.GetByID(ctx, "7301")
						row.Content += "!"
						if err := repo.Update(ctx, row); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			raced.after = func(call int, rows []*domain.Thread, total *int64) ([]*domain.Thread, error) {
				if fault == "initial-fault" && call == 1 || fault == "final-fault" && call == 2 {
					return nil, errors.New("storage secret diagnostic")
				}
				if call != 1 {
					return rows, nil
				}
				switch fault {
				case "nil-row":
					rows[0] = nil
				case "duplicate":
					rows = append(rows, rows[0])
					*total = 2
				case "wrong-count":
					*total = 2
				case "raw-missing":
					rows[0].PublicationStatus = ""
				case "foreign-owner":
					rows[0].AuthorID = "other-user"
				case "trash-row":
					rows[0].DeletionStatus = "trashed"
				}
				return rows, nil
			}
			filter := domain.ThreadListFilter{Page: 1, PageSize: 2}
			if fault == "overflow" {
				filter.Page = math.MaxInt
				filter.PageSize = 100
			}
			if fault == "cancel" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			rows, total, err := svc.ListThreadsForPrincipal(ctx, ownerReadResolver, filter)
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("fault returned partial content/count: %v %d %v", rows, total, err)
			}
			if fault == "persistent" && (!errors.Is(err, port.ErrPolicyFactsChanged) || raced.calls != 6 || len(record.requests) != 3) {
				t.Fatalf("unbounded retry %d %d %v", raced.calls, len(record.requests), err)
			}
			if fault == "overflow" && (!errors.Is(err, repository.ErrThreadPaginationOverflow) || raced.calls != 0) {
				t.Fatalf("overflow queried: %d %v", raced.calls, err)
			}
		})
	}
	// An older repository lacking the narrow snapshot port cannot silently use List.
	legacy := &publicListRaceRepository{ThreadRepository: repository.NewMemoryThreadRepository()}
	if rows, total, err := NewThreadService(legacy, nil).ListThreadsForPrincipal(context.Background(), ownerReadResolver, domain.ThreadListFilter{}); !errors.Is(err, port.ErrPolicyDecisionMismatch) || rows != nil || total != 0 || legacy.calls != 0 {
		t.Fatalf("legacy repository fallback: %v %d %v", rows, total, err)
	}
}
