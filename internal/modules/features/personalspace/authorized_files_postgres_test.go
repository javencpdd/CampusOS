package space

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func filesPostgres(t *testing.T, owner string) (*Service, *PgRepository, *identityrepo.PgUserRepository, *pgxpool.Pool, string, string) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL is not configured")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "campusos_v12_02b_space_files" {
		t.Fatal("space files tests require owned campusos_v12_02b_space_files")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	users := identityrepo.NewPgUserRepository(pool)
	user := &identitydomain.User{ID: owner, Username: "files-" + owner, Email: "files-" + owner + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo := NewPgRepository(pool)
	s := NewService(repo, identityUserReader{reader: portadapt.NewRepositoryUserReader(users)})
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), corestorage.DefaultQuotaBytes, corestorage.NewPgQuotaRepository(pool))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStoreWithStorage(FileStorageConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	s.SetQuotaManager(adapter)
	old, _, err := store.SaveAvatar(owner, "old.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := store.SaveAvatar(owner, "current.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	space := defaultSpace(user)
	space.ID = owner + "1"
	space.Avatar = store.AvatarURL(owner, current)
	if err := repo.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	return s, repo, users, pool, current, old
}

func TestSpaceFilesPostgreSQLCurrentBindings(t *testing.T) {
	ctx := context.Background()
	owner := "112201"
	s, repo, users, _, current, old := filesPostgres(t, owner)
	for _, binding := range []string{"user-profile", "space-profile", "history"} {
		for _, visibility := range []Visibility{VisibilityPublic, VisibilityUnlisted, VisibilityPrivate} {
			for _, disabled := range []bool{false, true} {
				for _, status := range []identitydomain.UserStatus{"active", "suspended", "deactivated"} {
					t.Run(binding+"/"+string(visibility)+"/"+string(status)+"/"+fmt.Sprint(disabled), func(t *testing.T) {
						user, _ := users.GetByID(ctx, owner)
						user.Status = status
						user.Avatar = ""
						if binding == "user-profile" {
							user.Avatar = s.fileStore.AvatarURL(owner, current)
						}
						if err := users.Update(ctx, user); err != nil {
							t.Fatal(err)
						}
						space, _ := repo.GetByUserID(ctx, owner)
						space.Visibility = visibility
						space.DisabledAt = nil
						if disabled {
							now := time.Now()
							space.DisabledAt = &now
						}
						if err := repo.Upsert(ctx, space); err != nil {
							t.Fatal(err)
						}
						name := current
						if binding == "history" {
							name = old
						}
						public := status == "active" && (binding == "user-profile" || (binding == "space-profile" && visibility == VisibilityPublic && !disabled))
						for _, actor := range []struct {
							resolve identityport.SessionPrincipalResolver
							allow   bool
						}{{nil, public}, {filesProof(owner), status == "active"}} {
							img, err := s.ReadAvatarForPrincipal(ctx, owner, name, actor.resolve)
							if (err == nil) != actor.allow || (err != nil && len(img.Data) != 0) {
								t.Fatalf("bytes=%d err=%v", len(img.Data), err)
							}
						}
					})
				}
			}
		}
	}
}

func filesCommit(t *testing.T, pool *pgxpool.Pool, action func(pgx.Tx) error) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := action(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestSpaceFilesPostgreSQLCommittedRevocation(t *testing.T) {
	for i, mode := range []string{"avatar", "visibility", "disabled", "status", "bytes", "history", "quota"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(112210 + i)
			s, repo, _, pool, current, _ := filesPostgres(t, owner)
			hook := &filesRepoHook{Repository: repo}
			s.repo = hook
			threshold := 1
			if mode == "bytes" || mode == "quota" {
				threshold = 2
			}
			if mode == "history" {
				threshold = 3
			}
			hook.after = func(n int) {
				if n != threshold {
					return
				}
				if mode == "bytes" {
					path, _ := s.fileStore.AvatarPath(owner, current)
					data := append([]byte{}, tinyPNG...)
					data[len(data)-1] ^= 1
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					return
				}
				if mode == "history" {
					if _, _, err := s.fileStore.SaveAvatar(owner, "later.png", bytes.NewReader(tinyPNG)); err != nil {
						t.Fatal(err)
					}
					return
				}
				if mode == "quota" {
					if _, err := corestorage.NewPgQuotaRepository(pool).Upsert(ctx, corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1024 * 1024, UpdatedBy: owner}); err != nil {
						t.Fatal(err)
					}
					return
				}
				filesCommit(t, pool, func(tx pgx.Tx) error {
					var err error
					switch mode {
					case "avatar":
						_, err = tx.Exec(ctx, `UPDATE user_spaces SET avatar='',updated_at=now() WHERE user_id=$1`, owner)
					case "visibility":
						_, err = tx.Exec(ctx, `UPDATE user_spaces SET visibility='private',updated_at=now() WHERE user_id=$1`, owner)
					case "disabled":
						_, err = tx.Exec(ctx, `UPDATE user_spaces SET disabled_at=now(),updated_at=now() WHERE user_id=$1`, owner)
					case "status":
						_, err = tx.Exec(ctx, `UPDATE users SET status='suspended',updated_at=now() WHERE id=$1`, owner)
					}
					return err
				})
			}
			if mode == "history" {
				history, err := s.ListAvatarsForPrincipal(ctx, filesProof(owner))
				if err == nil || history != nil {
					t.Fatal("stale history", err)
				}
			} else if mode == "quota" {
				status, err := s.StorageStatusForPrincipal(ctx, filesProof(owner))
				if err == nil || status != nil {
					t.Fatal("stale quota", err)
				}
			} else {
				img, err := s.ReadAvatarForPrincipal(ctx, owner, current, nil)
				if err == nil || len(img.Data) != 0 {
					t.Fatalf("stale bytes=%d err=%v", len(img.Data), err)
				}
			}
		})
	}
}
