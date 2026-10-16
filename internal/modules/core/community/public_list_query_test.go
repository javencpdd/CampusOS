package community

import (
	"context"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
)

type publicQueryRepository struct {
	*repository.MemoryThreadRepository
	calls   int
	filters []domain.ThreadListFilter
	revoke  bool
}

func (r *publicQueryRepository) List(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	r.filters = append(r.filters, filter)
	if r.revoke && r.calls == 2 {
		thread, err := r.MemoryThreadRepository.GetByID(ctx, "99201")
		if err != nil {
			return nil, 0, err
		}
		thread.PublicationStatus = domain.PublicationStatusPrivate
		thread.Content = "withdrawn-private-body"
		if err := r.MemoryThreadRepository.Update(ctx, thread); err != nil {
			return nil, 0, err
		}
	}
	return r.MemoryThreadRepository.List(ctx, filter)
}

func publicQueryFactories() map[string]func(*service.ThreadService) communityport.ContentQuery {
	return map[string]func(*service.ThreadService) communityport.ContentQuery{
		"compatibility": NewContentQuery,
		"module": func(svc *service.ThreadService) communityport.ContentQuery {
			return &moduleContentQuery{module: &Module{threadService: svc}}
		},
	}
}

func publicQuerySeed(t *testing.T, repo *repository.MemoryThreadRepository, id, publication string) {
	t.Helper()
	thread := &domain.Thread{ID: id, AuthorID: "99202", CategoryID: "99203", Title: "title", Content: "body",
		PublicationStatus: domain.PublicationStatus(publication), ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
}

func TestPublicThreadQueryUsesCanonicalList(t *testing.T) {
	for name, factory := range publicQueryFactories() {
		t.Run(name, func(t *testing.T) {
			repo := &publicQueryRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository()}
			publicQuerySeed(t, repo.MemoryThreadRepository, "99201", "published")
			publicQuerySeed(t, repo.MemoryThreadRepository, "99204", "private")
			query := factory(service.NewThreadService(repo, nil))
			rows, total, err := query.ListPublicThreads(context.Background(), domain.ThreadListFilter{
				Status: "all", PublicationStatus: "private", ModerationStatus: "taken_down", DeletionStatus: "trashed", IncludeTrashed: true,
				Page: 1, PageSize: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if total != 1 || len(rows) != 1 || rows[0].ID != "99201" || repo.calls != 2 {
				t.Fatalf("public query did not return a rechecked page: rows=%v total=%d calls=%d", rows, total, repo.calls)
			}
			for _, filter := range repo.filters {
				if filter.Status != "" || filter.PublicationStatus != "published" || filter.ModerationStatus != "clear" ||
					filter.DeletionStatus != "active" || filter.IncludeTrashed {
					t.Fatalf("unsafe filter: %+v", filter)
				}
			}
		})
	}
}

func TestPublicThreadQueryRevocationChangesWholePage(t *testing.T) {
	for name, factory := range publicQueryFactories() {
		t.Run(name, func(t *testing.T) {
			repo := &publicQueryRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository(), revoke: true}
			publicQuerySeed(t, repo.MemoryThreadRepository, "99201", "published")
			query := factory(service.NewThreadService(repo, nil))
			rows, total, err := query.ListPublicThreads(context.Background(), domain.ThreadListFilter{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if rows == nil || len(rows) != 0 || total != 0 || repo.calls != 4 {
				t.Fatalf("revocation retained a stale item/count: rows=%v total=%d calls=%d", rows, total, repo.calls)
			}
		})
	}
}

func TestPublicThreadQueryPreservesAuthorList(t *testing.T) {
	for name, factory := range publicQueryFactories() {
		t.Run(name, func(t *testing.T) {
			repo := &publicQueryRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository()}
			publicQuerySeed(t, repo.MemoryThreadRepository, "99201", "published")
			publicQuerySeed(t, repo.MemoryThreadRepository, "99204", "private")
			query := factory(service.NewThreadService(repo, nil))
			rows, total, err := query.ListAuthorThreads(context.Background(), "99202", domain.ThreadListFilter{Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if total != 2 || len(rows) != 2 || repo.calls != 1 {
				t.Fatalf("author list lost private content or entered public policy: rows=%v total=%d calls=%d", rows, total, repo.calls)
			}
			if repo.filters[0].AuthorID != "99202" || repo.filters[0].PublicationStatus != "" {
				t.Fatalf("author list filter changed: %+v", repo.filters[0])
			}
		})
	}
}

func TestPublicThreadQueryUnavailable(t *testing.T) {
	for name, query := range map[string]communityport.ContentQuery{
		"module": &moduleContentQuery{}, "compatibility": NewContentQuery(nil),
	} {
		t.Run(name, func(t *testing.T) {
			rows, total, err := query.ListPublicThreads(context.Background(), domain.ThreadListFilter{})
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("unavailable query did not fail: rows=%v total=%d err=%v", rows, total, err)
			}
		})
	}
}
