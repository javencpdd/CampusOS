package space

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestSpaceStyleReadPostgreSQLActualAllPathsDefaultExistingDisabledAndNoWrites(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, op := range styleReadOperations {
		for _, mode := range []string{"default", "existing", "disabled"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				index++
				owner := fmt.Sprint(2480201 + index*10)
				s, repo, pool, proof, claims := spaceOwnerPG(t, owner, mode != "default")
				if mode == "disabled" {
					now := time.Now()
					if err := repo.SetDisabled(ctx, owner, &now, "", "held"); err != nil {
						t.Fatal(err)
					}
				}
				before := spaceStylePGState(t, pool, owner, claims.SessionID)
				result, err := runStyleReadOperation(t, ctx, s, op, proof)
				if err != nil || len(result) == 0 || spaceStylePGState(t, pool, owner, claims.SessionID) != before {
					t.Fatal("actual read wrote state or failed", op, mode, err)
				}
			})
		}
	}
}

func TestSpaceStyleReadPostgreSQLActualCurrentFactsABAEmptyAndInputChangesClose(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, op := range styleReadOperations {
		for _, mode := range []string{"space_aba", "user_email", "session", "auth_version", "guard", "default_created", "operational"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				index++
				owner := fmt.Sprint(2490201 + index*10)
				s, repo, pool, current, claims := spaceOwnerPG(t, owner, mode != "default_created")
				before := spaceStylePGState(t, pool, owner, claims.SessionID)
				calls, ran := 0, false
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						ran = true
						var err error
						switch mode {
						case "space_aba":
							var bio string
							err = pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&bio)
							if err == nil {
								_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio='style read ABA' WHERE user_id=$1", owner)
							}
							if err == nil {
								_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, bio)
							}
						case "user_email":
							_, err = pool.Exec(ctx, "UPDATE users SET email=$2 WHERE id=$1", owner, owner+"-changed@example.test")
						case "session":
							_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='style read' WHERE id=$1", claims.SessionID)
						case "auth_version":
							_, err = pool.Exec(ctx, "UPDATE users SET auth_version=auth_version+1 WHERE id=$1", owner)
						case "guard":
							_, err = pool.Exec(ctx, "ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
							t.Cleanup(func() {
								if _, err := pool.Exec(context.Background(), "ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version"); err != nil {
									t.Error(err)
								}
							})
						case "default_created":
							user, e := s.users.GetByID(ctx, owner)
							if e != nil {
								t.Fatal(e)
							}
							space := defaultSpace(user)
							space.ID = owner + "1"
							space.IsDefault = false
							err = repo.Upsert(ctx, space)
						case "operational":
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET last_sync_error='changed operational field' WHERE user_id=$1", owner)
						}
						if err != nil {
							t.Fatal("actual committed mutation did not execute", mode, err)
						}
						before = spaceStylePGState(t, pool, owner, claims.SessionID)
					}
					return current(ctx)
				}
				result, err := runStyleReadOperation(t, ctx, s, op, proof)
				if !ran || err == nil || result != nil || spaceStylePGState(t, pool, owner, claims.SessionID) != before {
					t.Fatal("actual changed read accepted or wrote state", op, mode, err, ran)
				}
			})
		}
	}
	for i, mode := range []string{"session", "space_aba", "email"} {
		t.Run("zip_input/"+mode, func(t *testing.T) {
			owner := fmt.Sprint(2500201 + i*10)
			s, _, pool, proof, claims := spaceOwnerPG(t, owner, true)
			data := zipStylePack(t, styleVariantFiles())
			reader := &styleVariantReaderAt{ReaderAt: bytes.NewReader(data)}
			before := spaceStylePGState(t, pool, owner, claims.SessionID)
			ran := false
			reader.after = func() {
				ran = true
				var err error
				switch mode {
				case "session":
					_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='zip input' WHERE id=$1", claims.SessionID)
				case "email":
					_, err = pool.Exec(ctx, "UPDATE users SET email=$2 WHERE id=$1", owner, owner+"-input@example.test")
				case "space_aba":
					var bio string
					err = pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&bio)
					if err == nil {
						_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio='input ABA' WHERE user_id=$1", owner)
					}
					if err == nil {
						_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, bio)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				before = spaceStylePGState(t, pool, owner, claims.SessionID)
			}
			result, err := s.ValidateOwnSpaceStylePackZip(ctx, reader, int64(len(data)), proof)
			if !ran || err == nil || result != nil || spaceStylePGState(t, pool, owner, claims.SessionID) != before {
				t.Fatal("actual input-time change accepted", mode, result, err, ran)
			}
		})
	}
}
