package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"testing"
	"time"
)

type spaceOwnerPGHook struct {
	*PgRepository
	after     func(context.Context, *Space)
	afterRead func()
}

func (r *spaceOwnerPGHook) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	raw, err := r.PgRepository.LoadSpaceAuthority(ctx, owner, lock)
	if err == nil && !lock && r.afterRead != nil {
		after := r.afterRead
		r.afterRead = nil
		after()
	}
	return raw, err
}

func (r *spaceOwnerPGHook) Upsert(ctx context.Context, s *Space) error {
	if err := r.PgRepository.Upsert(ctx, s); err != nil {
		return err
	}
	if r.after != nil {
		r.after(ctx, s)
	}
	return nil
}
func spaceOwnerPG(t *testing.T, owner string, existing bool) (*Service, *PgRepository, *pgxpool.Pool, identityport.SessionPrincipalResolver, *auth.JWTClaims) {
	t.Helper()
	ctx := context.Background()
	s, repo, users, pool, _, _ := filesPostgres(t, owner)
	if !existing {
		if _, err := pool.Exec(ctx, "DELETE FROM user_spaces WHERE user_id=$1", owner); err != nil {
			t.Fatal(err)
		}
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "space-profile-pg-test", Issuer: "space-profile", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	identity, err := identityservice.NewSessionService(identityrepo.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: "space-profile-ip-test"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.GetByID(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := identity.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Space profile"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, identity, identity, claims)
	}
	return s, repo, pool, proof, claims
}
func spaceOwnerPGState(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	var raw string
	if err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'spaces',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb) FROM user_spaces s WHERE user_id=$1),
 'snapshots',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb) FROM user_space_style_snapshots s WHERE user_id=$1::text),
 'audit',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM platform_command_audits a WHERE actor_id=$1::text),
 'outbox',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM platform_outbox e WHERE aggregate_id=$1::text))::text`, owner).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestSpaceOwnerPostgreSQLActualDefaultExistingLocksAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"none", "platform_command_audits", "platform_outbox"} {
			t.Run(fmt.Sprint(existing)+"/"+failure, func(t *testing.T) {
				index++
				owner := fmt.Sprint(230201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
				before := spaceOwnerPGState(t, pool, owner)
				ran := false
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
					ran = true
					for _, row := range []struct{ query, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", claims.SessionID}} {
						_, err := pool.Exec(ctx, row.query, row.id)
						var pgErr *pgconn.PgError
						if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
							t.Fatal("actual User/Session lock missing", row, err)
						}
					}
					var acquired bool
					if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&acquired); err != nil || acquired {
						t.Fatal("actual owner admission lock missing", acquired, err)
					}
					if existing {
						_, err := pool.Exec(ctx, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
						var pgErr *pgconn.PgError
						if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
							t.Fatal("actual Space row lock missing", err)
						}
					}
				}}
				if failure != "none" {
					if _, err := pool.Exec(ctx, "CREATE FUNCTION space_owner_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required space profile rejected'; END $$"); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.Exec(ctx, "CREATE TRIGGER space_owner_reject BEFORE INSERT ON "+failure+" FOR EACH ROW EXECUTE FUNCTION space_owner_reject()"); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := pool.Exec(context.Background(), "DROP TRIGGER space_owner_reject ON "+failure+"; DROP FUNCTION space_owner_reject()"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				title := "Current profile"
				ack, err := s.UpsertOwnSpaceForPrincipal(ctx, UpsertSpaceRequest{Title: &title}, proof)
				if !ran || (err == nil) != (failure == "none") {
					t.Fatal("actual profile path not exercised", existing, failure, ack, err, ran)
				}
				if failure != "none" {
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "P0001" || spaceOwnerPGState(t, pool, owner) != before {
						t.Fatal("actual required SQL rollback incomplete", failure, err)
					}
					return
				}
				space, err := s.GetOwnSpaceForPrincipal(ctx, proof)
				if err != nil || space.Space.Title != title || space.Space.ID != ack.SpaceID || ack.OwnerID != owner {
					t.Fatal(ack, space, err)
				}
				var payload []byte
				if err := pool.QueryRow(ctx, "SELECT payload FROM platform_outbox WHERE aggregate_id=$1 AND event_type='personal_space.updated'", owner).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(payload, &fields) != nil || len(fields) != 2 || fields["space_id"] == nil || fields["owner_id"] == nil {
					t.Fatal("unsafe event payload", string(payload))
				}
			})
		}
	}
}
func TestSpaceOwnerPostgreSQLCommittedABAAndActualExtraWritesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"profile_aba", "session", "strength", "guard", "disabled", "after_extra", "after_actor", "after_session"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(231201 + index*10)
			s, repo, pool, proof, claims := spaceOwnerPG(t, owner, true)
			ran := false
			mutate := func(tx context.Context, space *Space) {
				ran = true
				db := transaction.ExecutorFor(tx, pool)
				var err error
				switch mode {
				case "profile_aba":
					var old string
					err = db.QueryRow(tx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old)
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio='changed' WHERE user_id=$1", owner)
					}
					if err == nil {
						_, err = db.Exec(tx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old)
					}
				case "session", "after_session":
					_, err = db.Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='space test' WHERE id=$1", claims.SessionID)
				case "strength":
					_, err = db.Exec(tx, "UPDATE sessions SET authentication_strength='mfa',mfa_authenticated_at=NOW() WHERE id=$1", claims.SessionID)
				case "guard":
					_, err = db.Exec(tx, "ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
				case "disabled":
					at := time.Now()
					err = repo.SetDisabled(tx, owner, &at, "", "disabled current space")
				case "after_extra":
					_, err = db.Exec(tx, "UPDATE user_spaces SET cover_image='extra metadata' WHERE user_id=$1", owner)
				case "after_actor":
					_, err = db.Exec(tx, "UPDATE user_spaces SET disabled_actor_kind='admin',disabled_actor_id='admin:extra' WHERE user_id=$1", owner)
				}
				if err != nil {
					t.Fatal("actual mutation failed", mode, err)
				}
			}
			before := spaceOwnerPGState(t, pool, owner)
			if mode == "profile_aba" {
				s.repo = &spaceOwnerPGHook{PgRepository: repo, afterRead: func() { mutate(ctx, nil) }}
			} else if len(mode) > 5 && mode[:6] == "after_" {
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: mutate}
			} else {
				calls := 0
				base := proof
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := base(ctx)
					calls++
					if calls == 1 {
						mutate(ctx, nil)
					}
					return p, e
				}
			}
			if mode == "guard" {
				defer func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version"); err != nil {
						t.Fatal(err)
					}
				}()
			}
			bio := "Requested bio"
			ack, err := s.UpsertOwnSpaceForPrincipal(ctx, UpsertSpaceRequest{Bio: &bio}, proof)
			if !ran || err == nil || ack != nil {
				t.Fatal("actual stale/current state accepted", mode, ack, err, ran)
			}
			if len(mode) > 5 && mode[:6] == "after_" && !reflect.DeepEqual(before, spaceOwnerPGState(t, pool, owner)) {
				t.Fatal("extra write or session change escaped rollback", mode)
			}
		})
	}
}
