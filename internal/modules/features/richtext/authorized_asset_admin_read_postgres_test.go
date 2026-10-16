package richtext

import (
	"context"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func TestAssetAdminReadPostgreSQLCurrentProjectionAndCommittedChanges(t *testing.T) {
	for k, op := range []string{"summary", "preview"} {
		for i, mode := range []string{"current", "permission_aba", "session_revoke", "metadata_aba", "references", "facts_guard", "counter"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				g := assetAdminPostgres(t, 167201+k*200+i*10, true, "restore")
				ctx := context.Background()
				code := "personal_space.asset.manage"
				if op == "summary" {
					code = "personal_space.asset.read_audit"
				}
				if _, err := g.pool.Exec(ctx, "UPDATE admin_role_permissions SET permission_code=$2 WHERE role_id=$1", g.role, code); err != nil {
					t.Fatal(err)
				}
				base := g.s.store
				ran := false
				g.s.store = &assetAdminReadHook{Store: base, after: func(ctx context.Context) {
					ran = true
					exec := func(sql string, args ...any) {
						if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "permission_aba":
						exec("UPDATE admin_role_permissions SET created_by='external' WHERE role_id=$1", g.role)
						exec("UPDATE admin_role_permissions SET created_by='test' WHERE role_id=$1", g.role)
					case "session_revoke":
						exec("UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='test',version=version+1 WHERE id=$1", g.claims.SessionID)
					case "metadata_aba":
						exec("UPDATE user_assets SET status='trashed' WHERE id=$1", g.a.AssetID)
						exec("UPDATE user_assets SET status='quarantined' WHERE id=$1", g.a.AssetID)
					case "references":
						exec("INSERT INTO richtext_article_attachments(id,article_content_id,asset_id,display_name,display_order,created_at,updated_at) VALUES($1,$2,$3,'new reference.pdf',0,NOW(),NOW())", fmt.Sprint(168201+k*200+i*10), g.a.ArticleContentID, g.a.AssetID)
					case "facts_guard":
						exec("ALTER TABLE user_assets DISABLE TRIGGER trg_user_assets_richtext_facts_version")
					case "counter":
						exec("UPDATE threads SET view_count=view_count+1 WHERE author_id=$1", g.a.Asset.OwnerID)
					}
				}}
				if mode == "facts_guard" {
					defer func() {
						if _, err := g.pool.Exec(context.Background(), "ALTER TABLE user_assets ENABLE TRIGGER trg_user_assets_richtext_facts_version"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				before := assetOwnerPGSnapshot(t, g.pool, g.a.AssetID)
				var err error
				if op == "summary" {
					_, err = g.s.AssetGovernanceSummaryForPrincipal(ctx, g.proof)
				} else {
					_, err = g.s.PreviewAssetPurgeForPrincipal(ctx, g.a.AssetID, g.proof)
				}
				want := mode == "current" || mode == "counter" || op == "summary" && (mode == "metadata_aba" || mode == "references" || mode == "facts_guard")
				if (err == nil) != want || !ran {
					t.Fatal("current actual PG projection wrong", op, mode, err, ran)
				}
				// Reads must add no lifecycle/platform history or outbox. A supported
				// external metadata ABA legitimately advances the asset nonce itself.
				if mode != "metadata_aba" && assetOwnerPGSnapshot(t, g.pool, g.a.AssetID) != before {
					t.Fatal("read wrote business metadata/audits", op, mode)
				}
			})
		}
	}
}

func TestAssetAdminReadPostgreSQLFinalAuthorityAfterWholeProjection(t *testing.T) {
	for k, op := range []string{"summary", "preview"} {
		t.Run(op, func(t *testing.T) {
			g := assetAdminPostgres(t, 167801+k*10, true, "restore")
			ctx := context.Background()
			if op == "summary" {
				if _, err := g.pool.Exec(ctx, "UPDATE admin_role_permissions SET permission_code='personal_space.asset.read_audit' WHERE role_id=$1", g.role); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			base := g.proof
			ran := false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				// Each authority read resolves twice. Call five is the final
				// authority read after both actual complete projections.
				if calls == 5 {
					ran = true
					if _, err := g.pool.Exec(ctx, "UPDATE admin_role_permissions SET created_by='changed after whole projection' WHERE role_id=$1", g.role); err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			before := assetOwnerPGSnapshot(t, g.pool, g.a.AssetID)
			var err error
			if op == "summary" {
				_, err = g.s.AssetGovernanceSummaryForPrincipal(ctx, proof)
			} else {
				_, err = g.s.PreviewAssetPurgeForPrincipal(ctx, g.a.AssetID, proof)
			}
			if err == nil || !ran {
				t.Fatal("final authority not rechecked", op, err, ran)
			}
			if assetOwnerPGSnapshot(t, g.pool, g.a.AssetID) != before {
				t.Fatal("final denied read mutated business")
			}
		})
	}
}
