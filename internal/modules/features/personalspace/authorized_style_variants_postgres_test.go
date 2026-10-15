package space

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func runOwnerStyleVariant(t *testing.T, ctx context.Context, s *Service, variant string, proof identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	t.Helper()
	switch variant {
	case "html":
		return s.ApplyOwnSpaceCustomHTML(ctx, `<section>Current HTML</section>`, proof)
	case "zip":
		data := zipStylePack(t, styleVariantFiles())
		return s.ApplyOwnSpaceStylePackZip(ctx, bytes.NewReader(data), int64(len(data)), proof)
	case "source":
		return s.ApplyOwnSpaceSourceStylePack(ctx, "variant-space", proof)
	default:
		return nil, ErrSpaceOwnerInvalid
	}
}

func TestSpaceStyleVariantsPostgreSQLActualCommandsLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, variant := range []string{"html", "zip", "source"} {
			for _, failure := range []string{"none", "user_spaces", "user_space_style_snapshots", "platform_command_audits", "platform_outbox"} {
				t.Run(fmt.Sprint(existing)+"/"+variant+"/"+failure, func(t *testing.T) {
					index++
					owner := fmt.Sprint(2440201 + index*10)
					s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
					if existing {
						seedOwnerStylePGSnapshot(t, repo, owner)
					}
					root := t.TempDir()
					t.Setenv("RESOURCE_DIR", root)
					writeStyleVariantSource(t, root, styleVariantFiles())
					before := spaceStylePGState(t, pool, owner, claims.SessionID)
					ran := false
					s.repo = &spaceStylePGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
						ran = true
						for _, row := range []struct{ q, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", claims.SessionID}} {
							_, err := pool.Exec(ctx, row.q, row.id)
							var p *pgconn.PgError
							if !errors.As(err, &p) || p.Code != "55P03" {
								t.Fatal("actual variant User/Session lock absent", row, err)
							}
						}
						if existing {
							for _, q := range []string{"SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", "SELECT id FROM user_space_style_snapshots WHERE user_id=$1 FOR UPDATE NOWAIT"} {
								_, err := pool.Exec(ctx, q, owner)
								var p *pgconn.PgError
								if !errors.As(err, &p) || p.Code != "55P03" {
									t.Fatal("actual Space/history lock absent", q, err)
								}
							}
						}
						var admitted bool
						if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&admitted); err != nil || admitted {
							t.Fatal("actual owner admission absent", err)
						}
					}}
					if failure != "none" {
						action := "INSERT"
						if failure == "user_spaces" {
							action = "INSERT OR UPDATE"
						}
						if _, err := pool.Exec(ctx, "CREATE FUNCTION space_variant_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required style variant rejected'; END $$; CREATE TRIGGER space_variant_reject BEFORE "+action+" ON "+failure+" FOR EACH ROW EXECUTE FUNCTION space_variant_reject()"); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS space_variant_reject ON "+failure+"; DROP FUNCTION IF EXISTS space_variant_reject()"); err != nil {
								t.Error(err)
							}
						})
					}
					ack, err := runOwnerStyleVariant(t, ctx, s, variant, proof)
					if (err == nil) != (failure == "none") || ran != (failure != "user_spaces" && failure != "user_space_style_snapshots") {
						t.Fatal("actual variant not executed", ack, err, ran)
					}
					if failure != "none" {
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "P0001" || ack != nil || before != spaceStylePGState(t, pool, owner, claims.SessionID) {
							t.Fatal("required variant SQL did not roll back", ack, err)
						}
						return
					}
					if ack == nil || ack.OwnerID != owner {
						t.Fatal(ack)
					}
					facts, err := s.loadSpaceStyleFacts(ctx, owner, false)
					if err != nil {
						t.Fatal(err)
					}
					want := 1
					if existing {
						want++
					}
					if len(facts.Snapshots) != want || facts.Owner.Space.StyleManifest == nil || !facts.Owner.Space.StyleManifest.CustomHTMLEnabled {
						t.Fatal("variant result/history missing", facts, err)
					}
				})
			}
		}
	}
}

func TestSpaceStyleVariantsPostgreSQLActualInputSourceAndExtraFactsChangesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"zip_session", "zip_space_aba", "source_body", "source_root", "source_after_write", "source_preview", "extra_profile", "extra_snapshot", "extra_session", "guard"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(2450201 + index*10)
			s, repo, pool, proof, claims := spaceOwnerPG(t, owner, true)
			seedOwnerStylePGSnapshot(t, repo, owner)
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			dir := writeStyleVariantSource(t, root, styleVariantFiles())
			before := spaceStylePGState(t, pool, owner, claims.SessionID)
			ran := false
			mutate := func(tx context.Context) {
				ran = true
				db := transaction.ExecutorFor(tx, pool)
				var err error
				switch mode {
				case "zip_session", "extra_session":
					_, err = db.Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='variant test' WHERE id=$1", claims.SessionID)
				case "zip_space_aba":
					var old string
					err = db.QueryRow(tx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old)
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio='ABA' WHERE user_id=$1", owner)
					}
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old)
					}
				case "source_body", "source_after_write":
					err = os.WriteFile(filepath.Join(dir, "templates/page.html"), []byte(`<section>Changed source</section>`), 0644)
				case "source_root":
					t.Setenv("RESOURCE_DIR", t.TempDir())
				case "source_preview":
					err = os.WriteFile(filepath.Join(dir, "preview-mobile.png"), []byte("changed preview"), 0644)
				case "extra_profile":
					_, err = db.Exec(tx, "UPDATE user_spaces SET bio='extra' WHERE user_id=$1", owner)
				case "extra_snapshot":
					_, err = db.Exec(tx, "UPDATE user_space_style_snapshots SET snapshot_type='extra' WHERE id=$1", owner+"7")
				case "guard":
					_, err = db.Exec(tx, "ALTER TABLE user_space_style_snapshots DISABLE TRIGGER trg_space_style_snapshots_facts_version")
				}
				if err != nil {
					t.Fatal("actual variant mutation failed", mode, err)
				}
			}
			variant := "html"
			input := zipStylePack(t, styleVariantFiles())
			reader := &styleVariantReaderAt{ReaderAt: bytes.NewReader(input)}
			if strings.HasPrefix(mode, "zip_") {
				variant = "zip"
				reader.after = func() { mutate(ctx) }
			}
			if strings.HasPrefix(mode, "source_") {
				variant = "source"
				if mode == "source_after_write" || mode == "source_preview" {
					s.repo = &spaceStylePGHook{PgRepository: repo, after: func(tx context.Context, _ *Space) { mutate(tx) }}
				} else {
					base := proof
					calls := 0
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := base(ctx)
						calls++
						if calls == 2 {
							mutate(ctx)
						}
						return p, e
					}
				}
			}
			if strings.HasPrefix(mode, "extra_") {
				s.repo = &spaceStylePGHook{PgRepository: repo, after: func(tx context.Context, _ *Space) { mutate(tx) }}
			}
			if mode == "guard" {
				s.repo = &spaceStylePGHook{PgRepository: repo, afterSnapshots: func() { mutate(ctx) }}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE user_space_style_snapshots ENABLE TRIGGER trg_space_style_snapshots_facts_version"); err != nil {
						t.Error(err)
					}
				})
			}
			var ack *SpaceMutationAck
			var err error
			if variant == "zip" {
				ack, err = s.ApplyOwnSpaceStylePackZip(ctx, reader, int64(len(input)), proof)
			} else {
				ack, err = runOwnerStyleVariant(t, ctx, s, variant, proof)
			}
			if !ran || err == nil || ack != nil {
				t.Fatal("actual input/source/extra facts change accepted", mode, ack, err, ran)
			}
			if (strings.HasPrefix(mode, "extra_") || strings.HasPrefix(mode, "source_")) && before != spaceStylePGState(t, pool, owner, claims.SessionID) {
				t.Fatal("variant metadata/history/current Session changed after failure", mode)
			}
		})
	}
}
