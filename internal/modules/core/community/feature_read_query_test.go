package community

import (
	"context"
	"errors"
	"testing"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestFeatureThreadQueryBranchesAndNoViews(t *testing.T) {
	for name, factory := range publicQueryFactories() {
		for _, publication := range []string{"published", "private", "draft"} {
			t.Run(name+"/"+publication, func(t *testing.T) {
				repo := repository.NewMemoryThreadRepository()
				publicQuerySeed(t, repo, "99201", publication)
				query := factory(service.NewThreadService(repo, nil)).(communityport.AuthenticatedThreadQuery)
				if row, err := query.GetThreadForPrincipal(context.Background(), "99201", nil); (err == nil) != (publication == "published") || (err != nil && row != nil) {
					t.Fatalf("public: %v %v", row, err)
				}
				row, err := query.GetThreadForPrincipal(context.Background(), "99201", ownerQueryResolve)
				if err != nil || row.AuthorID != "99202" || row.ViewCount != 0 {
					t.Fatalf("owner: %v %v", row, err)
				}
				stored, err := repo.GetByID(context.Background(), "99201")
				if err != nil || stored.ViewCount != 0 {
					t.Fatal("feature read incremented views")
				}
			})
		}
	}
}

func TestFeatureThreadQueryRevokedPublicProofAndDrift(t *testing.T) {
	for _, mode := range []string{"revoked", "actor", "credential", "strength", "same-ID-admin"} {
		t.Run(mode, func(t *testing.T) {
			repo := repository.NewMemoryThreadRepository()
			publicQuerySeed(t, repo, "99201", "published")
			query := NewContentQuery(service.NewThreadService(repo, nil)).(communityport.AuthenticatedThreadQuery)
			calls := 0
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := ownerQueryResolve(ctx)
				calls++
				if calls >= 3 {
					switch mode {
					case "revoked":
						return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
					case "actor":
						p.Actor.ID = "other-owner"
					case "credential":
						p.CredentialID = "replacement-session"
					case "strength":
						p.AuthenticationStrength = "mfa"
					case "same-ID-admin":
						p.Actor.Kind, p.Audience = "admin", "admin"
					}
				}
				return p, nil
			}
			row, err := query.GetThreadForPrincipal(context.Background(), "99201", resolve)
			if err == nil || row != nil {
				t.Fatalf("stale authenticated public result: %v %v", row, err)
			}
			if mode != "revoked" && !errors.Is(err, identityport.ErrPolicyFactsChanged) {
				t.Fatalf("drift error: %v", err)
			}
		})
	}
}

func TestFeatureThreadQueryUnavailable(t *testing.T) {
	for _, query := range []communityport.AuthenticatedThreadQuery{(*moduleContentQuery)(nil), &moduleContentQuery{}, (*contentQuery)(nil), &contentQuery{}, (*moduleContentGateway)(nil), &moduleContentGateway{}, (*contentGateway)(nil), &contentGateway{}} {
		row, err := query.GetThreadForPrincipal(context.Background(), "99201", ownerQueryResolve)
		if err == nil || row != nil {
			t.Fatalf("unavailable query returned data: %v %v", row, err)
		}
	}
}
