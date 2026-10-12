package space

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func ownContentsPrincipal() identityport.PrincipalContext {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract,
		Actor: identityport.PrincipalRef{Kind: "user", ID: "98401"}, Audience: "user",
		AuthenticationStrength: "password", CredentialID: "session-98401"}
}

func ownContentsResolver(context.Context) (identityport.PrincipalContext, error) {
	return ownContentsPrincipal(), nil
}

type authenticatedSpaceQuery struct {
	capturingContentQuery
	filter communitydomain.ThreadListFilter
	calls  int
	query  func(context.Context, func(context.Context) (identityport.PrincipalContext, error), communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error)
}

func (q *authenticatedSpaceQuery) ListAuthorThreadsForPrincipal(ctx context.Context, resolve func(context.Context) (identityport.PrincipalContext, error), filter communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	q.calls++
	q.filter = filter
	if q.query != nil {
		return q.query(ctx, resolve, filter)
	}
	if _, err := resolve(ctx); err != nil {
		return nil, 0, err
	}
	return q.authorThreads, int64(len(q.authorThreads)), nil
}

func ownContentsService(query *authenticatedSpaceQuery) *Service {
	svc := NewService(NewMemoryRepository(), newFakeUserLookup(&identitydomain.User{ID: "98401", Username: "owner", Nickname: "Owner"}))
	if query != nil {
		svc.SetContentQuery(query)
	}
	return svc
}

func TestOwnContentsPrincipalBoundary(t *testing.T) {
	for _, name := range []string{"nil-resolver", "invalid-contract", "same-id-admin", "worker-effective-owner", "missing-query", "typed-nil-query", "legacy-query", "disabled-missing-query", "cancelled", "feature-disabled"} {
		t.Run(name, func(t *testing.T) {
			q := &authenticatedSpaceQuery{}
			svc := ownContentsService(q)
			ctx := context.Background()
			resolve := ownContentsResolver
			want := identityport.ErrUserSessionPrincipalUnavailable
			switch name {
			case "nil-resolver":
				resolve = nil
			case "invalid-contract", "same-id-admin", "worker-effective-owner":
				resolve = func(context.Context) (identityport.PrincipalContext, error) {
					p := ownContentsPrincipal()
					if name == "invalid-contract" {
						p.Contract = "unknown"
					} else if name == "same-id-admin" {
						p.Actor.Kind, p.Audience = "admin", "admin"
					} else {
						p.Actor = identityport.PrincipalRef{Kind: "worker", ID: "worker-98401"}
						p.Audience, p.AuthenticationStrength = "worker", "workload"
						p.DelegationID = "delegation-98401"
						owner := identityport.PrincipalRef{Kind: "user", ID: "98401"}
						p.EffectiveSubject = &owner
					}
					return p, nil
				}
			case "missing-query", "disabled-missing-query":
				svc.contentQuery = nil
				want = ErrAuthenticatedAuthorQueryUnavailable
				if name == "disabled-missing-query" {
					if err := svc.repo.Upsert(ctx, &Space{UserID: "98401", SyncEnabled: false}); err != nil {
						t.Fatal(err)
					}
				}
			case "legacy-query":
				svc.SetContentQuery(&capturingContentQuery{})
				want = ErrAuthenticatedAuthorQueryUnavailable
			case "typed-nil-query":
				var query *authenticatedSpaceQuery
				svc.SetContentQuery(query)
				want = ErrAuthenticatedAuthorQueryUnavailable
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "feature-disabled":
				svc.SetPluginEnabledChecker(func() bool { return false })
				want = ErrSpacePluginDisabled
			}
			rows, total, err := svc.ListOwnContentsForPrincipal(ctx, resolve, 1, 20)
			if !errors.Is(err, want) || rows != nil || total != 0 || q.calls != 0 {
				t.Fatalf("boundary leaked a list: rows=%v total=%d calls=%d err=%v want=%v", rows, total, q.calls, err, want)
			}
		})
	}
}

func TestOwnContentsPrincipalFiltersAndProjection(t *testing.T) {
	q := &authenticatedSpaceQuery{capturingContentQuery: capturingContentQuery{authorThreads: []*communitydomain.Thread{{
		ID: "98411", AuthorID: "98401", CategoryID: "98421", Title: " private title ", Content: "<p>Owner only body</p>",
		PublicationStatus: communitydomain.PublicationStatusPrivate, ModerationStatus: communitydomain.ModerationStatusTakenDown,
		DeletionStatus: communitydomain.DeletionStatusActive, ModerationReason: "owner only reason", FactsVersion: 1, CreatedAt: time.Now(),
	}}}}
	svc := ownContentsService(q)
	if err := svc.repo.Upsert(context.Background(), &Space{UserID: "98401", Visibility: VisibilityPrivate, SyncEnabled: true,
		SyncCategories: []string{" 98421 ", "98421"}, SyncTags: []string{" Campus ", "campus"}}); err != nil {
		t.Fatal(err)
	}
	proofs := 0
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		proofs++
		return ownContentsResolver(ctx)
	}
	rows, total, err := svc.ListOwnContentsForPrincipal(context.Background(), resolve, 0, 101)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("owner projection failed: rows=%v total=%d err=%v", rows, total, err)
	}
	if proofs != 3 || q.calls != 1 || q.authorCalls != 0 || q.publicCalls != 0 {
		t.Fatalf("owner proof or query path changed: proofs=%d query=%+v", proofs, q)
	}
	if q.filter.AuthorID != "" || q.filter.Status != "" || q.filter.Page != 1 || q.filter.PageSize != 100 ||
		!reflect.DeepEqual(q.filter.CategoryIDs, []string{"98421"}) || !reflect.DeepEqual(q.filter.AnyTags, []string{"Campus"}) {
		t.Fatalf("configuration filters changed or bare owner was passed: %+v", q.filter)
	}
	if rows[0].UserID != "98401" || rows[0].Title != "private title" || rows[0].Excerpt != "Owner only body" || rows[0].ModerationReason != "owner only reason" {
		t.Fatalf("owner projection lost canonical fields: %+v", rows[0])
	}
}

func TestOwnContentsPrincipalEmptyAndProofChanges(t *testing.T) {
	for _, name := range []string{"empty", "disabled", "disabled-revoked", "disabled-new-session", "empty-new-actor", "empty-new-strength", "query-revoked", "query-new-session"} {
		t.Run(name, func(t *testing.T) {
			q := &authenticatedSpaceQuery{}
			svc := ownContentsService(q)
			disabled := name == "disabled" || name == "disabled-revoked" || name == "disabled-new-session"
			if disabled {
				if err := svc.repo.Upsert(context.Background(), &Space{UserID: "98401", SyncEnabled: false}); err != nil {
					t.Fatal(err)
				}
			}
			proofs := 0
			resolve := func(context.Context) (identityport.PrincipalContext, error) {
				proofs++
				p := ownContentsPrincipal()
				last := 3
				if disabled {
					last = 2
				}
				if proofs == last {
					switch name {
					case "disabled-revoked", "query-revoked":
						return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
					case "disabled-new-session", "query-new-session":
						p.CredentialID = "new-session"
					case "empty-new-actor":
						p.Actor.ID = "98402"
					case "empty-new-strength":
						p.AuthenticationStrength = "mfa"
					}
				}
				return p, nil
			}
			rows, total, err := svc.ListOwnContentsForPrincipal(context.Background(), resolve, 3, 20)
			if name == "empty" || name == "disabled" {
				if err != nil || rows == nil || len(rows) != 0 || total != 0 {
					t.Fatalf("empty result: rows=%v total=%d err=%v", rows, total, err)
				}
			} else {
				want := identityport.ErrPolicyFactsChanged
				if name == "disabled-revoked" || name == "query-revoked" {
					want = identityport.ErrUserSessionPrincipalUnavailable
				}
				if !errors.Is(err, want) || rows != nil || total != 0 {
					t.Fatalf("changed proof consumed empty page: rows=%v total=%d err=%v", rows, total, err)
				}
			}
			if disabled && q.calls != 0 {
				t.Fatal("disabled sync queried Community")
			}
		})
	}
}

func TestOwnContentsPrincipalQueryFailures(t *testing.T) {
	for _, name := range []string{"internal", "conflict", "wrong-owner", "nil-row", "negative-total"} {
		t.Run(name, func(t *testing.T) {
			q := &authenticatedSpaceQuery{query: func(context.Context, func(context.Context) (identityport.PrincipalContext, error), communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
				switch name {
				case "internal":
					return nil, 0, errors.New("private-db-dsn-do-not-leak")
				case "conflict":
					return nil, 0, identityport.ErrPolicyFactsChanged
				case "wrong-owner":
					return []*communitydomain.Thread{{ID: "98411", AuthorID: "98402", Content: "other private body"}}, 1, nil
				case "nil-row":
					return []*communitydomain.Thread{nil}, 1, nil
				default:
					return []*communitydomain.Thread{}, -1, nil
				}
			}}
			rows, total, err := ownContentsService(q).ListOwnContentsForPrincipal(context.Background(), ownContentsResolver, 1, 20)
			if err == nil || rows != nil || total != 0 {
				t.Fatalf("query failure returned data: rows=%v total=%d err=%v", rows, total, err)
			}
		})
	}
}
