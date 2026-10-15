package space

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestSpaceStyleCatalogPostgreSQLActualDefaultExistingCurrentReadOnly(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "missing_root", "empty", "disabled"} {
			t.Run(fmt.Sprint(existing)+"/"+mode, func(t *testing.T) {
				index++
				owner := fmt.Sprint(2460201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
				root := t.TempDir()
				t.Setenv("RESOURCE_DIR", root)
				if mode != "missing_root" {
					dir := writeStyleVariantSource(t, root, styleVariantFiles())
					if mode == "empty" {
						if err := os.RemoveAll(dir); err != nil {
							t.Fatal(err)
						}
					}
				}
				if mode == "disabled" && existing {
					now := time.Now()
					if err := repo.SetDisabled(ctx, owner, &now, "", "held"); err != nil {
						t.Fatal(err)
					}
				}
				before := spaceStylePGState(t, pool, owner, claims.SessionID)
				result, err := s.ListOwnSpaceSourceStylePacks(ctx, proof)
				if err != nil || result == nil || result.Items == nil || spaceStylePGState(t, pool, owner, claims.SessionID) != before {
					t.Fatal("actual catalog read mutated Space/history/Session/audit/outbox", result, err)
				}
				want := 1
				if mode == "missing_root" || mode == "empty" {
					want = 0
				}
				raw, _ := json.Marshal(result)
				if len(result.Items) != want || strings.Contains(string(raw), root) {
					t.Fatal("actual catalog lost empty/current safe projection", string(raw))
				}
			})
		}
	}
}

func TestSpaceStyleCatalogPostgreSQLActualCommittedFactsSessionGuardAndEmptyChangesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"space_aba", "user_email", "session", "auth_version", "guard", "root", "body", "preview", "raw_asset", "empty_add", "empty_session"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(2470201 + index*10)
			s, _, pool, current, claims := spaceOwnerPG(t, owner, true)
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			dir := writeStyleVariantSource(t, root, styleVariantFiles())
			if mode == "empty_add" || mode == "empty_session" {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			}
			before := spaceStylePGState(t, pool, owner, claims.SessionID)
			calls, ran := 0, false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					ran = true
					var err error
					switch mode {
					case "space_aba":
						var old string
						err = pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old)
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio='catalog ABA' WHERE user_id=$1", owner)
						}
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old)
						}
					case "user_email":
						_, err = pool.Exec(ctx, "UPDATE users SET email='catalog-change@example.test' WHERE id=$1", owner)
					case "session", "empty_session":
						_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='catalog current test' WHERE id=$1", claims.SessionID)
					case "auth_version":
						_, err = pool.Exec(ctx, "UPDATE users SET auth_version=auth_version+1 WHERE id=$1", owner)
					case "guard":
						_, err = pool.Exec(ctx, "ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
						t.Cleanup(func() {
							if _, e := pool.Exec(context.Background(), "ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version"); e != nil {
								t.Error(e)
							}
						})
					case "root":
						t.Setenv("RESOURCE_DIR", t.TempDir())
					case "body":
						err = os.WriteFile(filepath.Join(dir, "templates/page.html"), []byte("<section>Changed</section>"), 0644)
					case "preview":
						err = os.WriteFile(filepath.Join(dir, "preview-mobile.png"), []byte("changed preview"), 0644)
					case "raw_asset":
						err = os.WriteFile(filepath.Join(dir, "extra.png"), []byte("changed raw bytes"), 0644)
					case "empty_add":
						writeStyleVariantSource(t, root, styleVariantFiles())
					}
					if err != nil {
						t.Fatal("actual committed change did not execute", mode, err)
					}
					before = spaceStylePGState(t, pool, owner, claims.SessionID)
				}
				return current(ctx)
			}
			result, err := s.ListOwnSpaceSourceStylePacks(ctx, proof)
			if !ran || err == nil || result != nil || spaceStylePGState(t, pool, owner, claims.SessionID) != before {
				t.Fatal("actual changed catalog accepted or read wrote state", mode, result, err, ran)
			}
		})
	}
}
