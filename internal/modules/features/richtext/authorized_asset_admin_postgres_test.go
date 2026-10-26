package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func assertAssetAdminSafePayload(raw, id string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return err
	}
	var asset, status string
	if len(fields) != 2 || json.Unmarshal(fields["asset_id"], &asset) != nil || json.Unmarshal(fields["status"], &status) != nil || asset != id || status != AssetStatusActive && status != AssetStatusQuarantined {
		return fmt.Errorf("unsafe Admin Asset payload fields")
	}
	return nil
}

type assetAdminPG struct {
	s           *Service
	pool        *pgxpool.Pool
	a           *ArticleAttachment
	proof       identityport.SessionPrincipalResolver
	claims      *auth.JWTClaims
	actor, role string
	adapter     *corestorage.LocalAdapter
}

func assetAdminPostgres(t *testing.T, seed int, opaque bool, operation string) assetAdminPG {
	t.Helper()
	ctx := context.Background()
	owner := fmt.Sprint(seed)
	s, pool, _, a, _, adapter := assetOwnerPostgres(t, owner, "trash")
	g := assetAdminPG{s: s, pool: pool, a: a, actor: owner, role: "asset_gov_" + owner, adapter: adapter}
	if opaque {
		g.actor = "adm.asset." + owner
	}
	admins, sessions := identityrepo.NewPgAdminDomainRepository(pool), identityrepo.NewPgAdminSessionRepository(pool)
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: g.actor, Username: "asset-gov-" + owner, Email: "asset-gov-" + owner + "@example.test"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute)
	sid := "adms.asset." + owner
	if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: g.actor, RefreshTokenDigest: fmt.Sprintf("%064x", seed), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "asset-gov-pg-test-key", Issuer: "asset-gov-pg", Audience: "admin", AccessTTL: time.Hour})
	current := identityservice.NewAdminSessionService(admins, sessions, jwt)
	token, err := jwt.GenerateAccessToken(g.actor, "Admin", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	g.claims, err = jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, current, g.claims)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Asset governance drill',FALSE)", g.role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.asset." + owner, AccountID: g.actor, RoleID: g.role, GrantedBy: g.actor}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'personal_space.asset.manage','test')", g.role); err != nil {
		t.Fatal(err)
	}
	s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, current))
	if operation == "restore" {
		if err := s.store.TransitionUserAsset(ctx, a.AssetID, owner, []string{AssetStatusActive}, AssetStatusQuarantined, AssetLifecycleAudit{ActorID: g.actor, ActorType: "admin", Action: "quarantined"}); err != nil {
			t.Fatal(err)
		}
	}
	return g
}
func TestAssetAdminLifecyclePostgreSQLTypedLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for i, op := range []string{"quarantine", "restore"} {
		for k, opaque := range []bool{false, true} {
			for j, failure := range []string{"none", "asset_lifecycle_audits", "platform_command_audits", "platform_outbox", "extra_mutation"} {
				t.Run(fmt.Sprint(op, "/", opaque, "/", failure), func(t *testing.T) {
					g := assetAdminPostgres(t, 165201+i*300+k*100+j*10, opaque, op)
					before := assetOwnerPGSnapshot(t, g.pool, g.a.AssetID)
					usage, err := g.adapter.Usage(g.a.Asset.OwnerID)
					if err != nil {
						t.Fatal(err)
					}
					locked, objectLocked, extraRan := false, false, false
					base := g.s.store
					g.s.store = &assetOwnerPGHook{Store: base, after: func(context.Context) {
						locked = true
						for _, row := range []struct{ sql, id string }{
							{"SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT", g.actor},
							{"SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT", g.claims.SessionID},
							{"SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT", g.role},
							{"SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT", g.role},
							{"SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT", g.role},
							{"SELECT id FROM user_assets WHERE id=$1 FOR UPDATE NOWAIT", g.a.AssetID},
						} {
							assertAttachmentUploadPGLock(t, g.pool, row.sql, row.id)
						}
					}}
					if op == "restore" {
						g.s.objects = &attachmentMetadataObjectHook{ObjectPort: g.s.objects, after: func() {
							objectLocked = true
							assertAttachmentUploadPGLock(t, g.pool, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", g.a.Asset.StorageObjectID)
						}}
					}
					if failure == "extra_mutation" {
						g.s.store = &assetOwnerMutationStore{Store: g.s.store, after: func(tx context.Context, row *UserAsset) {
							extraRan = true
							if _, err := base.(*PgStore).db(tx).Exec(tx, "UPDATE user_assets SET original_name='in-command extra.pdf' WHERE id=$1", row.ID); err != nil {
								t.Fatal(err)
							}
						}}
					} else if failure != "none" {
						if _, err := g.pool.Exec(ctx, "CREATE FUNCTION asset_admin_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required admin asset record rejected'; END $$"); err != nil {
							t.Fatal(err)
						}
						if _, err := g.pool.Exec(ctx, "CREATE TRIGGER asset_admin_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION asset_admin_reject()"); err != nil {
							t.Fatal(err)
						}
						defer func() {
							if _, err := g.pool.Exec(context.Background(), "DROP TRIGGER asset_admin_reject ON "+failure+"; DROP FUNCTION asset_admin_reject()"); err != nil {
								t.Fatal(err)
							}
						}()
					}
					ack, err := g.s.AdminAssetLifecycleForPrincipal(ctx, g.a.AssetID, op, "actual SQL audit", g.proof)
					if !locked || op == "restore" && !objectLocked || (err == nil) != (failure == "none") {
						t.Fatal("real Admin asset execution not reached", ack, err, locked, objectLocked)
					}
					if failure != "none" {
						if failure == "extra_mutation" {
							if !extraRan || !errors.Is(err, identityport.ErrPolicyFactsChanged) {
								t.Fatal(extraRan, err)
							}
						} else {
							var pe *pgconn.PgError
							if !errors.As(err, &pe) || pe.Code != "P0001" {
								t.Fatal("required SQL rejection not reached", err)
							}
						}
						if ack != nil || assetOwnerPGSnapshot(t, g.pool, g.a.AssetID) != before {
							t.Fatal("partial Asset/audit/outbox rollback", ack, err)
						}
					} else {
						var kind, actor, legacy, payload string
						if err := g.pool.QueryRow(ctx, "SELECT actor_kind,actor_ref_id,COALESCE(actor_user_id::text,'') FROM asset_lifecycle_audits WHERE asset_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", g.a.AssetID).Scan(&kind, &actor, &legacy); err != nil || kind != "admin" || actor != g.actor || legacy != "" {
							t.Fatal("independent Admin stored as User", kind, actor, legacy, err)
						}
						if err := g.pool.QueryRow(ctx, "SELECT payload::text FROM platform_outbox WHERE aggregate_type='user_asset' AND aggregate_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", g.a.AssetID).Scan(&payload); err != nil {
							t.Fatal(err)
						}
						if err := assertAssetAdminSafePayload(payload, g.a.AssetID); err != nil {
							t.Fatal(err, payload)
						}
					}
					if after, err := g.adapter.Usage(g.a.Asset.OwnerID); err != nil || after != usage {
						t.Fatal("metadata governance changed bytes", after, usage, err)
					}
				})
			}
		}
	}
}
func TestAssetAdminLifecyclePostgreSQLCurrentFactsABAAndGuards(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"asset_aba", "permission_aba", "session_revoke", "mfa_expire", "object_changed", "facts_guard", "reference_guard", "counter"} {
		t.Run(mode, func(t *testing.T) {
			g := assetAdminPostgres(t, 166501+i*10, true, "restore")
			before := assetOwnerPGSnapshot(t, g.pool, g.a.AssetID)
			calls := 0
			base := g.proof
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					exec := func(sql string, args ...any) {
						if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "asset_aba":
						exec("UPDATE user_assets SET original_name='changed.pdf' WHERE id=$1", g.a.AssetID)
						exec("UPDATE user_assets SET original_name=$2 WHERE id=$1", g.a.AssetID, g.a.Asset.OriginalName)
					case "permission_aba":
						exec("UPDATE admin_role_permissions SET created_by='external' WHERE role_id=$1", g.role)
						exec("UPDATE admin_role_permissions SET created_by='test' WHERE role_id=$1", g.role)
					case "session_revoke":
						repo := identityrepo.NewPgAdminSessionRepository(g.pool)
						row, err := repo.GetSessionByID(ctx, g.claims.SessionID)
						if err != nil {
							t.Fatal(err)
						}
						if _, ok, err := repo.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
							t.Fatal(err, ok)
						}
					case "mfa_expire":
						exec("UPDATE admin_sessions SET mfa_authenticated_at=NOW()-interval '1 hour',version=version+1 WHERE id=$1", g.claims.SessionID)
					case "object_changed":
						exec("UPDATE storage_objects SET mime_type='changed' WHERE id=$1", g.a.Asset.StorageObjectID)
					case "facts_guard":
						exec("ALTER TABLE user_assets DISABLE TRIGGER trg_user_assets_richtext_facts_version")
					case "reference_guard":
						exec("ALTER TABLE richtext_article_assets DROP CONSTRAINT fk_richtext_article_assets_user_asset")
					case "counter":
						exec("UPDATE threads SET view_count=view_count+1 WHERE author_id=$1", g.a.Asset.OwnerID)
					}
				}
				return base(ctx)
			}
			if mode == "facts_guard" {
				defer func() {
					if _, err := g.pool.Exec(context.Background(), "ALTER TABLE user_assets ENABLE TRIGGER trg_user_assets_richtext_facts_version"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			if mode == "reference_guard" {
				var originalForeignKey string
				if err := g.pool.QueryRow(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='richtext_article_assets'::regclass AND conname='fk_richtext_article_assets_user_asset'").Scan(&originalForeignKey); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := g.pool.Exec(context.Background(), "ALTER TABLE richtext_article_assets ADD CONSTRAINT fk_richtext_article_assets_user_asset "+originalForeignKey); err != nil {
						t.Fatal(err)
					}
				}()
			}
			ack, err := g.s.AdminAssetLifecycleForPrincipal(ctx, g.a.AssetID, "restore", "actual committed change", proof)
			if (err == nil) != (mode == "counter") {
				t.Fatal(mode, ack, err)
			}
			if mode != "counter" {
				row, readErr := g.s.store.GetUserAsset(ctx, g.a.AssetID)
				if readErr != nil || row.Status != AssetStatusQuarantined || ack != nil {
					t.Fatal("denial changed Asset", row, ack, readErr)
				}
				if mode != "asset_aba" && assetOwnerPGSnapshot(t, g.pool, g.a.AssetID) != before {
					t.Fatal("denial changed metadata or audits", mode)
				}
			}
		})
	}
}
