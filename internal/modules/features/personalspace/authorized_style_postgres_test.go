package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type spaceStylePGHook struct {
	*PgRepository
	after          func(context.Context, *Space)
	afterSnapshots func()
}

func (r *spaceStylePGHook) Upsert(ctx context.Context, space *Space) error {
	if err := r.PgRepository.Upsert(ctx, space); err != nil {
		return err
	}
	if r.after != nil {
		r.after(ctx, space)
	}
	return nil
}
func (r *spaceStylePGHook) LoadStyleSnapshotAuthority(ctx context.Context, owner string, lock bool) ([]json.RawMessage, error) {
	rows, err := r.PgRepository.LoadStyleSnapshotAuthority(ctx, owner, lock)
	if err == nil && !lock && r.afterSnapshots != nil {
		after := r.afterSnapshots
		r.afterSnapshots = nil
		after()
	}
	return rows, err
}
func seedOwnerStylePGSnapshot(t *testing.T, repo *PgRepository, owner string) {
	t.Helper()
	manifest := validStylePackage().Manifest
	if err := repo.SaveStyleSnapshot(context.Background(), &StyleSnapshot{ID: owner + "7", UserID: owner, SnapshotType: "before_apply", Theme: manifest.Name, Layout: manifest.Layout, StyleName: manifest.Name, StyleVersion: manifest.Version, StyleManifest: cloneManifest(&manifest), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil {
		t.Fatal(err)
	}
}
func spaceStylePGState(t *testing.T, pool *pgxpool.Pool, owner, session string) string {
	t.Helper()
	var raw string
	if err := pool.QueryRow(context.Background(), "SELECT to_jsonb(s)::text FROM sessions s WHERE id=$1", session).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return spaceOwnerPGState(t, pool, owner) + raw
}
func TestSpaceStyleOwnerPostgreSQLActualSpaceHistoryLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, op := range []string{"apply", "default", "rollback"} {
			failures := []string{"none", "platform_command_audits", "platform_outbox", "user_spaces"}
			if op != "rollback" {
				failures = append(failures, "user_space_style_snapshots")
			}
			for _, failure := range failures {
				t.Run(fmt.Sprint(existing)+"/"+op+"/"+failure, func(t *testing.T) {
					index++
					owner := fmt.Sprint(2420201 + index*10)
					s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
					if existing || op == "rollback" {
						seedOwnerStylePGSnapshot(t, repo, owner)
					}
					before := spaceStylePGState(t, pool, owner, claims.SessionID)
					ran := false
					s.repo = &spaceStylePGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
						ran = true
						for _, row := range []struct{ q, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", claims.SessionID}} {
							_, err := pool.Exec(ctx, row.q, row.id)
							var p *pgconn.PgError
							if !errors.As(err, &p) || p.Code != "55P03" {
								t.Fatal("actual current User/Session lock absent", row, err)
							}
						}
						if existing {
							_, err := pool.Exec(ctx, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
							var p *pgconn.PgError
							if !errors.As(err, &p) || p.Code != "55P03" {
								t.Fatal("actual Space lock absent", err)
							}
						}
						if existing || op == "rollback" {
							_, err := pool.Exec(ctx, "SELECT id FROM user_space_style_snapshots WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
							var p *pgconn.PgError
							if !errors.As(err, &p) || p.Code != "55P03" {
								t.Fatal("actual history rows not locked", err)
							}
						}
						var admitted bool
						if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&admitted); err != nil || admitted {
							t.Fatal("owner empty/history admission absent", err)
						}
					}}
					if failure != "none" {
						action := "INSERT"
						if failure == "user_spaces" {
							action = "INSERT OR UPDATE"
						}
						if _, err := pool.Exec(ctx, "CREATE FUNCTION space_style_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required space style rejected'; END $$; CREATE TRIGGER space_style_reject BEFORE "+action+" ON "+failure+" FOR EACH ROW EXECUTE FUNCTION space_style_reject()"); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS space_style_reject ON "+failure+"; DROP FUNCTION IF EXISTS space_style_reject()"); err != nil {
								t.Error(err)
							}
						})
					}
					ack, err := runOwnerStyle(ctx, s, op, validStylePackage(), proof)
					if (err == nil) != (failure == "none") || ran != (failure != "user_spaces" && failure != "user_space_style_snapshots") {
						t.Fatal("actual style command not reached", ack, err, ran)
					}
					if failure != "none" {
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "P0001" || ack != nil || before != spaceStylePGState(t, pool, owner, claims.SessionID) {
							t.Fatal("required style SQL not fully rolled back", ack, err)
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
					want := 0
					if existing || op == "rollback" {
						want++
					}
					if op != "rollback" {
						want++
					}
					if len(facts.Snapshots) != want {
						t.Fatal("wrong actual history inventory", len(facts.Snapshots), want)
					}
					if op == "default" {
						if facts.Owner.Space.StyleManifest != nil || facts.Owner.Space.Theme != "default" {
							t.Fatal(facts.Owner.Space)
						}
					} else if facts.Owner.Space.StyleName != validStylePackage().Manifest.Name {
						t.Fatal(facts.Owner.Space)
					}
					var raw []byte
					if err := pool.QueryRow(ctx, "SELECT payload FROM platform_outbox WHERE aggregate_id=$1 AND event_type='personal_space.style.updated'", owner).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["space_id"] == nil || fields["owner_id"] == nil {
						t.Fatal("private style leaked", string(raw))
					}
				})
			}
		}
	}
}
func TestSpaceStyleOwnerPostgreSQLHeldABAAndActualExtraWritesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"space_aba", "snapshot_aba", "guard", "session", "extra_profile", "extra_snapshot", "extra_actor", "extra_session"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(2430201 + index*10)
			s, repo, pool, proof, claims := spaceOwnerPG(t, owner, true)
			seedOwnerStylePGSnapshot(t, repo, owner)
			ran := false
			before := spaceStylePGState(t, pool, owner, claims.SessionID)
			mutate := func(tx context.Context) {
				ran = true
				db := transaction.ExecutorFor(tx, pool)
				var err error
				switch mode {
				case "space_aba":
					var old string
					err = db.QueryRow(tx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old)
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio='ABA' WHERE user_id=$1", owner)
					}
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old)
					}
				case "snapshot_aba":
					var old time.Time
					err = db.QueryRow(tx, "SELECT created_at FROM user_space_style_snapshots WHERE id=$1", owner+"7").Scan(&old)
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_space_style_snapshots SET created_at=created_at+INTERVAL '1 minute' WHERE id=$1", owner+"7")
					}
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_space_style_snapshots SET created_at=$2 WHERE id=$1", owner+"7", old)
					}
				case "guard":
					_, err = db.Exec(tx, "ALTER TABLE user_space_style_snapshots DISABLE TRIGGER trg_space_style_snapshots_facts_version")
				case "session", "extra_session":
					_, err = db.Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='style test' WHERE id=$1", claims.SessionID)
				case "extra_profile":
					_, err = db.Exec(tx, "UPDATE user_spaces SET bio='extra' WHERE user_id=$1", owner)
				case "extra_actor":
					_, err = db.Exec(tx, "UPDATE user_spaces SET disabled_actor_kind='admin',disabled_actor_id='opaque-admin' WHERE user_id=$1", owner)
				case "extra_snapshot":
					_, err = db.Exec(tx, "UPDATE user_space_style_snapshots SET snapshot_type='extra' WHERE id=$1", owner+"7")
				}
				if err != nil {
					t.Fatal("actual style mutation failed", mode, err)
				}
			}
			if len(mode) > 6 && mode[:6] == "extra_" {
				s.repo = &spaceStylePGHook{PgRepository: repo, after: func(tx context.Context, _ *Space) { mutate(tx) }}
			} else {
				s.repo = &spaceStylePGHook{PgRepository: repo, afterSnapshots: func() { mutate(ctx) }}
			}
			if mode == "guard" {
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE user_space_style_snapshots ENABLE TRIGGER trg_space_style_snapshots_facts_version"); err != nil {
						t.Error(err)
					}
				})
			}
			ack, err := s.ApplyOwnSpaceStylePackage(ctx, validStylePackage(), proof)
			if !ran || err == nil || ack != nil {
				t.Fatal("held/extra style facts accepted", ack, err, ran)
			}
			if len(mode) > 6 && mode[:6] == "extra_" && !reflect.DeepEqual(before, spaceStylePGState(t, pool, owner, claims.SessionID)) {
				t.Fatal("actual extra style writes escaped rollback", mode)
			}
		})
	}
}
