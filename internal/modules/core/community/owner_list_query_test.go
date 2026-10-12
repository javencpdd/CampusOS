package community

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type ownerQueryRepository struct {
	*repository.MemoryThreadRepository
	calls  int
	revoke bool
}

func (r *ownerQueryRepository) ListOwnerThreadSnapshot(ctx context.Context, owner string, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	r.calls++
	if r.revoke && r.calls == 2 {
		row, err := r.GetByID(ctx, "99201")
		if err != nil {
			return nil, 0, err
		}
		row.AuthorID = "99299"
		row.Content = "other owner's private body"
		if err := r.Update(ctx, row); err != nil {
			return nil, 0, err
		}
	}
	return r.MemoryThreadRepository.ListOwnerThreadSnapshot(ctx, owner, filter)
}
func ownerQueryResolve(context.Context) (identityport.PrincipalContext, error) {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "99202"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "verified-session"}, nil
}
func TestOwnerThreadListQueryPort(t *testing.T) {
	for name, factory := range publicQueryFactories() {
		for _, revoke := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/stable", true: "/revoked"}[revoke], func(t *testing.T) {
				repo := &ownerQueryRepository{MemoryThreadRepository: repository.NewMemoryThreadRepository(), revoke: revoke}
				publicQuerySeed(t, repo.MemoryThreadRepository, "99201", "private")
				query, ok := factory(service.NewThreadService(repo, nil)).(communityport.AuthenticatedAuthorContentQuery)
				if !ok {
					t.Fatal("authenticated author port unavailable")
				}
				rows, total, err := query.ListAuthorThreadsForPrincipal(context.Background(), ownerQueryResolve, domain.ThreadListFilter{AuthorID: "99299", Status: "published", Page: 1, PageSize: 10})
				want := 1
				calls := 2
				if revoke {
					want = 0
					calls = 4
				}
				if err != nil || len(rows) != want || total != int64(want) || repo.calls != calls {
					t.Fatalf("rows=%v total=%d calls=%d err=%v", rows, total, repo.calls, err)
				}
				if len(rows) > 0 && (rows[0].AuthorID != "99202" || rows[0].PublicationStatus != "private" || rows[0].ViewCount != 0) {
					t.Fatal("public or bare-ID path consumed instead of owner policy")
				}
				_, _, err = query.ListAuthorThreadsForPrincipal(context.Background(), nil, domain.ThreadListFilter{})
				if !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
					t.Fatalf("missing principal resolver: %v", err)
				}
			})
		}
	}
}
func TestOwnerThreadListQueryUnavailable(t *testing.T) {
	for name, query := range map[string]communityport.AuthenticatedAuthorContentQuery{"module": &moduleContentQuery{}, "compat": NewContentQuery(nil).(communityport.AuthenticatedAuthorContentQuery), "typed-nil-module": (*moduleContentQuery)(nil), "typed-nil-compat": (*contentQuery)(nil)} {
		t.Run(name, func(t *testing.T) {
			rows, total, err := query.ListAuthorThreadsForPrincipal(context.Background(), ownerQueryResolve, domain.ThreadListFilter{})
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("missing query returned data: %v %d %v", rows, total, err)
			}
		})
	}
}
