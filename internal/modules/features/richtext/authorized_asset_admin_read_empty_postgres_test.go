package richtext

import (
	"context"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAssetAdminReadPostgreSQLEmptyProjectionStillRechecksAuthority(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_asset_read_empty" {
		t.Fatal("empty projection tests require dedicated owned database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var assets, audits int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM user_assets),(SELECT count(*) FROM asset_lifecycle_audits)").Scan(&assets, &audits); err != nil || assets != 0 || audits != 0 {
		t.Fatal("empty fixture is not actual empty", assets, audits, err)
	}
	for i, mode := range []string{"current", "session_revoke", "permission_aba"} {
		t.Run(mode, func(t *testing.T) {
			actor := fmt.Sprint("adm.empty.", 168501+i)
			role := fmt.Sprint("empty_asset_", 168501+i)
			sid := "session." + actor
			admins, sessions := identityrepo.NewPgAdminDomainRepository(pool), identityrepo.NewPgAdminSessionRepository(pool)
			if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: actor, Username: actor, Email: actor + "@example.test"}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: actor, RefreshTokenDigest: fmt.Sprintf("%064x", 168501+i), TokenFamilyID: sid, AuthenticationStrength: "password", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Empty asset read drill',FALSE)", role); err != nil {
				t.Fatal(err)
			}
			if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding." + actor, AccountID: actor, RoleID: role, GrantedBy: actor}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'personal_space.asset.read_audit','test')", role); err != nil {
				t.Fatal(err)
			}
			jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "empty-asset-admin-pg-key", Issuer: "empty-asset", Audience: "admin", AccessTTL: time.Hour})
			current := identityservice.NewAdminSessionService(admins, sessions, jwt)
			token, err := jwt.GenerateAccessToken(actor, "Empty Admin", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			claims, err := jwt.VerifyAccessToken(token)
			if err != nil {
				t.Fatal(err)
			}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				return identityport.ResolveAdminSessionPrincipal(ctx, current, claims)
			}
			s := NewService(NewPgStore(pool), nil)
			s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, current))
			ran := false
			s.store = &assetAdminReadHook{Store: s.store, after: func(ctx context.Context) {
				ran = true
				if mode == "session_revoke" {
					row, err := sessions.GetSessionByID(ctx, sid)
					if err != nil {
						t.Fatal(err)
					}
					if _, ok, err := sessions.RevokeSessionCAS(ctx, sid, row.Version, "test", time.Now()); err != nil || !ok {
						t.Fatal(err, ok)
					}
				}
				if mode == "permission_aba" {
					if _, err := pool.Exec(ctx, "UPDATE admin_role_permissions SET created_by='external' WHERE role_id=$1", role); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, "UPDATE admin_role_permissions SET created_by='test' WHERE role_id=$1", role); err != nil {
						t.Fatal(err)
					}
				}
			}}
			value, err := s.AssetGovernanceSummaryForPrincipal(ctx, proof)
			if !ran || (err == nil) != (mode == "current") {
				t.Fatal("empty projection bypassed current proof", value, err, ran)
			}
			if mode == "current" && (len(value.Statuses) != 0 || len(value.Actions) != 0 || value.Statuses == nil || value.Actions == nil || value.ExpiredInvocations != 0) {
				t.Fatal(value)
			}
		})
	}
}
