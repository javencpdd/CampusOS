package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
)

func governanceUserProof(t *testing.T) identityport.SessionPrincipalResolver {
	t.Helper()
	sessions, users, jwt := newSessionServiceForTest(t)
	ctx := context.Background()
	u, err := users.GetByID(ctx, "22001")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Issue(ctx, u, SessionMetadata{DeviceName: "Governance test"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, sessions, sessions, claims)
	}
}
func governanceAdminProof(t *testing.T, mode string) (*AdminSessionService, identityport.SessionPrincipalResolver) {
	t.Helper()
	env := newAdminSessionEnv()
	ctx := context.Background()
	now := time.Now().UTC()
	const actor = "22001"
	const sessionID = "adms.governance.memory"
	if err := env.admins.CreateAccount(ctx, repository.AdminDomainAccount{ID: actor, Username: "governance-admin", Email: "governance-admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	strength := "mfa"
	at := now.Add(-time.Minute)
	if mode == "password" {
		strength = "password"
	}
	if mode == "expired" {
		at = now.Add(-AdminGovernanceMFAWindow - time.Minute)
	}
	if err := env.sessions.CreateSession(ctx, repository.AdminSession{ID: sessionID, AccountID: actor, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sessionID, AuthenticationStrength: strength, MFAAuthenticatedAt: &at, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	claims := &auth.JWTClaims{UserID: actor, SessionID: sessionID, AuthVersion: 1, TokenType: auth.AccessTokenType}
	return env.service, func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, env.service, claims)
	}
}
func decideGovernanceAuthority(t *testing.T, a identityport.BoardGovernanceAuthority, q identityport.BoardGovernanceQuery) bool {
	t.Helper()
	kind := "thread"
	if q.Action == "community.post.delete" {
		kind = "post"
	}
	r := identityport.ResourcePolicyRequest{Contract: identityport.ResourcePolicyContract, RequestID: "gov-test", Principal: a.Principal, Action: q.Action, Facts: identityport.ResourcePolicyFacts{Kind: kind, ID: "actual-resource", OwnerID: "other-owner", BoardID: q.BoardID, Status: "active", PublicationStatus: "published", Version: "actual-version"}, Grants: a.Grants, EvaluatedAtMS: time.Now().UnixMilli(), PolicyVersion: q.Action + "/v1"}
	d, err := (identitypolicy.ResourcePolicy{}).DecideResource(r)
	if err != nil {
		t.Fatal(err)
	}
	return d.Effect == "allow"
}

func TestBoardGovernanceAuthorityUserSingleWitness(t *testing.T) {
	for _, action := range []string{"community.thread.take_down", "community.post.delete"} {
		for _, mode := range []string{"current", "board", "subject", "future", "expired", "mfa", "suspended", "split", "management", "bound", "missing", "unknown"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				repo := repository.NewMemoryDelegationRepository()
				resolve := governanceUserProof(t)
				now := time.Now().UTC()
				q := identityport.BoardGovernanceQuery{BoardID: "board.actual", Action: action}
				d := repository.Delegation{ID: "grant.actual", Kind: "grant", SubjectKind: "user", SubjectID: "22001", Action: action, BoardID: q.BoardID, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}
				switch mode {
				case "board":
					d.BoardID = "board.other"
				case "subject":
					d.SubjectID = "other-user"
				case "future":
					d.NotBefore = now.Add(time.Minute)
				case "expired":
					d.NotBefore = now.Add(-time.Hour)
					d.ExpiresAt = now.Add(-time.Minute)
				case "mfa":
					d.RequiredStrength = "mfa"
				case "suspended":
					d.Status = "suspended"
				case "unknown":
					q.Action = "community.thread.pin"
				}
				if mode == "management" {
					d.Kind = "management"
					d.SubjectKind = "admin"
					d.Action = "identity.role.assign"
					d.BoardID = ""
					d.RequiredStrength = ""
				}
				if mode == "bound" {
					d.Kind = "bound"
					d.SubjectKind = "admin"
					d.Delegable = true
				}
				if mode != "missing" {
					if err := repo.InsertDelegations(ctx, []repository.Delegation{d}); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "suspended" {
					if _, _, err := repo.SetStatusCAS(ctx, d.ID, 1, "suspended", now); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "split" {
					if _, _, err := repo.SetStatusCAS(ctx, d.ID, 1, "suspended", now); err != nil {
						t.Fatal(err)
					}
					other := d
					other.ID = "grant.other-board"
					other.BoardID = "board.other"
					if err := repo.InsertDelegations(ctx, []repository.Delegation{other}); err != nil {
						t.Fatal(err)
					}
				}
				svc := NewBoardGovernanceAuthorityService(repo, nil, nil)
				a, err := svc.LoadBoardGovernanceAuthority(ctx, q, resolve, false)
				if mode == "unknown" {
					if !errors.Is(err, identityport.ErrResourcePolicyUnknownAction) {
						t.Fatal(err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				allowed := decideGovernanceAuthority(t, a, q)
				if allowed != (mode == "current") {
					t.Fatal(a, allowed)
				}
				if len(a.Grants) > 1 {
					t.Fatal("combined grants")
				}
			})
		}
	}
}
func TestBoardGovernanceAuthorityAdminIndependentDomain(t *testing.T) {
	for _, mode := range []string{"current", "password", "expired", "other-code", "missing-facts", "user-grant-only", "missing-session"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			admin, resolve := governanceAdminProof(t, mode)
			repo := admin.admins.(*repository.MemoryAdminDomainRepository)
			code := "community.thread.take_down"
			if mode == "other-code" {
				code = "community.post.delete"
			}
			if err := repo.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.gov", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
				t.Fatal(err)
			}
			if mode != "missing-facts" && mode != "user-grant-only" {
				if err := repo.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "test-seed"); err != nil {
					t.Fatal(err)
				}
			}
			delegations := repository.NewMemoryDelegationRepository()
			now := time.Now().UTC()
			if err := delegations.InsertDelegations(ctx, []repository.Delegation{{ID: "same-ID-user-grant", Kind: "grant", SubjectKind: "user", SubjectID: "22001", Action: "community.thread.take_down", BoardID: "board.actual", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
				t.Fatal(err)
			}
			var reader identityport.AdminSessionAuthorityReader = admin
			if mode == "missing-session" {
				reader = nil
			}
			svc := NewBoardGovernanceAuthorityService(delegations, repo, reader)
			q := identityport.BoardGovernanceQuery{BoardID: "board.actual", Action: "community.thread.take_down"}
			a, err := svc.LoadBoardGovernanceAuthority(ctx, q, resolve, false)
			if mode == "missing-session" {
				if !errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if decideGovernanceAuthority(t, a, q) != (mode == "current") {
				t.Fatal(a)
			}
			if mode == "current" {
				if a.Grants[0].SubjectKind != "admin" || a.Grants[0].RequiredStrength != "mfa" || a.Grants[0].ExpiresAtMS > now.Add(AdminGovernanceMFAWindow).UnixMilli() {
					t.Fatal(a.Grants)
				}
			}
		})
	}
}

func TestBoardGovernanceAuthorityRechecksFactsAndCredential(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryDelegationRepository()
	resolve := governanceUserProof(t)
	now := time.Now().UTC()
	d := repository.Delegation{ID: "grant.recheck", Kind: "grant", SubjectKind: "user", SubjectID: "22001", Action: "community.thread.take_down", BoardID: "board.actual", NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}
	if err := repo.InsertDelegations(ctx, []repository.Delegation{d}); err != nil {
		t.Fatal(err)
	}
	svc := NewBoardGovernanceAuthorityService(repo, nil, nil)
	q := identityport.BoardGovernanceQuery{BoardID: d.BoardID, Action: d.Action}
	calls := 0
	changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
		calls++
		if calls == 2 {
			if _, _, err := repo.SetStatusCAS(ctx, d.ID, 1, "suspended", now); err != nil {
				t.Fatal(err)
			}
		}
		return resolve(ctx)
	}
	if a, err := svc.LoadBoardGovernanceAuthority(ctx, q, changing, false); !errors.Is(err, identityport.ErrPolicyFactsChanged) || a.Binding != nil {
		t.Fatal("grant changed before return", a, err)
	}
	if _, err := svc.LoadBoardGovernanceAuthority(ctx, q, resolve, true); !errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable) {
		t.Fatal("lock outside transaction", err)
	}
	err := transaction.NewMemory().Within(ctx, func(txCtx context.Context) error {
		_, err := svc.LoadBoardGovernanceAuthority(txCtx, q, resolve, true)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
