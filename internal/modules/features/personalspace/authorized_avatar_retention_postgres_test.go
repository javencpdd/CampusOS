package space

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type avatarRetentionPGStoreHook struct {
	*reliability.PostgreSQLStore
	before func(context.Context, string)
	after  func(context.Context, string)
}

func (s *avatarRetentionPGStoreHook) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	phase := strings.TrimPrefix(a.CommandCode, avatarRetentionKind+".")
	if s.before != nil {
		s.before(ctx, phase)
	}
	if err := s.PostgreSQLStore.RecordCommandAudit(ctx, a); err != nil {
		return err
	}
	if s.after != nil {
		s.after(ctx, phase)
	}
	return nil
}

func avatarRetentionPG(t *testing.T, owner string, protect bool) (*Service, *PgRepository, *pgxpool.Pool, identityport.SessionPrincipalResolver, string, *corestorage.ObjectService, []avatarFile) {
	t.Helper()
	ctx := context.Background()
	s, repo, pool, proof, claims := spaceOwnerPG(t, owner, true)
	s.fileStore.cfg.AvatarKeepLimit = 10
	for i := 0; i < 3; i++ {
		if _, _, err := s.fileStore.SaveAvatar(owner, "legacy.png", bytes.NewReader(tinyPNG)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := s.fileStore.avatarFiles(owner)
	if err != nil || len(files) != 5 {
		t.Fatal(files, err)
	}
	space, err := repo.GetByUserID(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	space.Avatar = s.fileStore.AvatarURL(owner, files[0].name)
	if err := repo.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	if protect {
		if _, err := pool.Exec(ctx, "UPDATE users SET avatar=$2 WHERE id=$1", owner, s.fileStore.AvatarURL(owner, files[4].name)); err != nil {
			t.Fatal(err)
		}
	}
	s.fileStore.cfg.AvatarKeepLimit = 3
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(s.fileStore.cfg.RootDir, 1024*1024, corestorage.NewPgQuotaRepository(pool))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAvatarStorage(adapter, objects)
	if _, err := corestorage.NewPgQuotaRepository(pool).Upsert(ctx, corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Usage(ctx, owner); err != nil {
		t.Fatal(err)
	}
	return s, repo, pool, proof, claims.SessionID, objects, files
}
func avatarRetentionLedger(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	var raw string
	if err := pool.QueryRow(context.Background(), "SELECT to_jsonb(a)::text FROM user_storage_accounts a WHERE user_id=$1", owner).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSpaceAvatarRetentionPostgreSQLActualLocksRequiredSQLAndRestartRecovery(t *testing.T) {
	ctx := context.Background()
	type scene struct {
		phase, table string
		protect      bool
	}
	scenes := []scene{{"none", "", false}, {"none", "", true}, {"prepare", "platform_command_audits", false}, {"prepare", "platform_outbox", false}, {"prepare", "platform_operation_runs", false}, {"bytes", "platform_command_audits", false}, {"bytes", "platform_outbox", false}, {"finalize", "platform_command_audits", false}, {"finalize", "platform_outbox", false}, {"finalize", "platform_operation_runs", false}, {"finalize", "user_storage_accounts", false}}
	for index, scene := range scenes {
		t.Run(fmt.Sprintf("%s/%s/protect=%t", scene.phase, scene.table, scene.protect), func(t *testing.T) {
			owner := fmt.Sprint(2400201 + index*10)
			s, repo, pool, proof, session, objects, files := avatarRetentionPG(t, owner, scene.protect)
			beforeSpace, err := repo.LoadSpaceAuthority(ctx, owner, false)
			if err != nil {
				t.Fatal(err)
			}
			beforeLedger := avatarRetentionLedger(t, pool, owner)
			lockChecks := 0
			hook := &avatarRetentionPGStoreHook{PostgreSQLStore: reliability.NewPostgreSQLStore(pool)}
			hook.before = func(tx context.Context, phase string) {
				if phase != "bytes" && phase != "finalize" {
					return
				}
				lockChecks++
				rows := []struct{ query, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", session}, {"SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", owner}}
				if phase == "finalize" {
					rows = append(rows, struct{ query, id string }{"SELECT user_id FROM user_storage_accounts WHERE user_id=$1 FOR UPDATE NOWAIT", owner})
				}
				for _, row := range rows {
					_, err := pool.Exec(ctx, row.query, row.id)
					var p *pgconn.PgError
					if !errors.As(err, &p) || p.Code != "55P03" {
						t.Fatal("actual cleanup lock absent", phase, row, err)
					}
				}
				var admitted bool
				if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&admitted); err != nil || admitted {
					t.Fatal("actual owner admission absent", err)
				}
				var opID string
				if err := pool.QueryRow(ctx, "SELECT id FROM platform_operation_runs WHERE kind=$1 AND subject_id=$2 AND status<>'succeeded' ORDER BY created_at DESC,id DESC LIMIT 1", avatarRetentionKind, owner).Scan(&opID); err != nil {
					t.Fatal(err)
				}
				_, err := pool.Exec(ctx, "SELECT id FROM platform_operation_runs WHERE id=$1 FOR UPDATE NOWAIT", opID)
				var p *pgconn.PgError
				if !errors.As(err, &p) || p.Code != "55P03" {
					t.Fatal("actual operation row lock absent", phase, err)
				}
			}
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), hook))
			if scene.phase != "none" {
				action, condition := "INSERT", "NEW.command_code='"+avatarRetentionKind+"."+scene.phase+"'"
				switch scene.table {
				case "platform_outbox":
					condition = "NEW.event_type='personal_space.avatar_retention." + scene.phase + "'"
				case "platform_operation_runs":
					condition = "NEW.kind='" + avatarRetentionKind + "'"
					if scene.phase == "finalize" {
						action = "UPDATE"
						condition += " AND NEW.status='succeeded'"
					}
				case "user_storage_accounts":
					action = "UPDATE"
					condition = "TRUE"
				}
				query := "CREATE SEQUENCE avatar_retention_reject_hits; CREATE FUNCTION avatar_retention_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('avatar_retention_reject_hits'); RAISE EXCEPTION 'required avatar retention rejected'; END $$; CREATE TRIGGER avatar_retention_reject BEFORE " + action + " ON " + scene.table + " FOR EACH ROW WHEN (" + condition + ") EXECUTE FUNCTION avatar_retention_reject()"
				if _, err := pool.Exec(ctx, query); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS avatar_retention_reject ON "+scene.table+"; DROP FUNCTION IF EXISTS avatar_retention_reject(); DROP SEQUENCE IF EXISTS avatar_retention_reject_hits"); err != nil {
						t.Error(err)
					}
				})
			}
			ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, proof)
			if err != nil || ack == nil || ack.CleanupPending != (scene.phase != "none") {
				t.Fatal("wrong actual cleanup result", ack, err)
			}
			current := avatarUploadFileBytes(t, s, owner)
			if current[files[0].name] == nil || scene.protect && current[files[4].name] == nil {
				t.Fatal("active avatar deleted")
			}
			afterSpace, err := repo.LoadSpaceAuthority(ctx, owner, false)
			if err != nil || !bytes.Equal(beforeSpace, afterSpace) {
				t.Fatal("cleanup changed Space", err)
			}
			if scene.phase != "none" {
				var hit bool
				if err := pool.QueryRow(ctx, "SELECT is_called FROM avatar_retention_reject_hits").Scan(&hit); err != nil || !hit {
					t.Fatal("actual P0001 trigger did not execute", hit, err)
				}
				if beforeLedger != avatarRetentionLedger(t, pool, owner) {
					t.Fatal("failed cleanup released capacity")
				}
				if scene.phase == "prepare" && len(current) != 5 || scene.phase != "prepare" && len(current) != 3 {
					t.Fatal("wrong pending physical state", scene, len(current))
				}
				if _, err := pool.Exec(ctx, "DROP TRIGGER avatar_retention_reject ON "+scene.table+"; DROP FUNCTION avatar_retention_reject(); DROP SEQUENCE avatar_retention_reject_hits"); err != nil {
					t.Fatal(err)
				}
				// New service, file store, account repository and reliability adapter:
				// no in-memory receipt survives this restart.
				freshStore, err := NewLocalFileStoreWithStorage(s.fileStore.cfg, s.avatarStorage)
				if err != nil {
					t.Fatal(err)
				}
				restarted := NewService(repo, s.users)
				restarted.SetFileStore(freshStore)
				freshObjects, err := corestorage.NewObjectService(s.avatarStorage, s.avatarStorage.(corestorage.Quota), corestorage.NewPgObjectRepository(pool), 1024*1024)
				if err != nil {
					t.Fatal(err)
				}
				restarted.SetAvatarStorage(s.avatarStorage, freshObjects)
				restarted.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
				ack, err = restarted.CleanupAvatarHistoryForPrincipal(ctx, proof)
				if err != nil || ack.CleanupPending {
					t.Fatal("persistent retry after restart failed", ack, err)
				}
				s = restarted
			}
			current = avatarUploadFileBytes(t, s, owner)
			want := 3
			if scene.protect {
				want = 4
			}
			if len(current) != want {
				t.Fatal(len(current), want)
			}
			var actual int64
			for _, data := range current {
				actual += int64(len(data))
			}
			usage, err := objects.Usage(ctx, owner)
			if err != nil || usage.UsedBytes != actual {
				t.Fatal("capacity not exact", usage, actual, err)
			}
			if scene.phase == "none" && lockChecks == 0 {
				t.Fatal("actual lock hooks did not run")
			}
			ledger := avatarRetentionLedger(t, pool, owner)
			ack, err = s.CleanupAvatarHistoryForPrincipal(ctx, proof)
			if err != nil || ack.CleanupPending || ledger != avatarRetentionLedger(t, pool, owner) {
				t.Fatal("terminal retry released twice", ack, err)
			}
			var payloads [][]byte
			rows, err := pool.Query(ctx, "SELECT payload FROM platform_outbox WHERE aggregate_id=$1 AND event_type LIKE 'personal_space.avatar_retention.%'", owner)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var raw []byte
				if err := rows.Scan(&raw); err != nil {
					t.Fatal(err)
				}
				payloads = append(payloads, raw)
			}
			rows.Close()
			for _, raw := range payloads {
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["owner_id"] == nil || fields["operation_id"] == nil {
					t.Fatal("private receipt leaked to event", string(raw))
				}
			}
		})
	}
}

func TestSpaceAvatarRetentionPostgreSQLHeldFactsReceiptExtraWritesAndCancellation(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"space_aba", "empty_job_session", "inode", "after_session", "after_space", "after_operation", "cancel_after_bytes", "subject_filter"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(2410201 + index*10)
			s, _, pool, proof, session, objects, files := avatarRetentionPG(t, owner, false)
			p, err := proof(ctx)
			if err != nil {
				t.Fatal(err)
			}
			reader := reliability.NewPostgreSQLStore(pool)
			r := &avatarRetentionRun{s: s, store: s.fileStore, cfg: s.fileStore.cfg, reader: reader, account: s.avatarAccount.(corestorage.AvatarRetentionAccounting), storage: s.avatarStorage, accountPort: s.avatarAccount, p: p, resolve: proof}
			scope, err := r.scope(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := readAvatarRetentionReceipt(ctx, s.fileStore, owner, files[3].name)
			if err != nil {
				t.Fatal(err)
			}
			ran := false
			if mode == "space_aba" {
				var old string
				if err := pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "UPDATE user_spaces SET bio='ABA' WHERE user_id=$1", owner); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old); err != nil {
					t.Fatal(err)
				}
				ran = true
				job, err := r.prepare(ctx, scope, *receipt)
				if !ran || err == nil || job != nil || len(avatarUploadFileBytes(t, s, owner)) != 5 {
					t.Fatal("held Space ABA accepted", job, err)
				}
				return
			}
			if mode == "empty_job_session" {
				base := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					current, e := base(ctx)
					calls++
					if calls == 1 {
						ran = true
						if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='cleanup test' WHERE id=$1", session); err != nil {
							t.Fatal(err)
						}
					}
					return current, e
				}
				ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, proof)
				if !ran || err == nil || ack != nil || len(avatarUploadFileBytes(t, s, owner)) != 5 {
					t.Fatal("empty current session skipped", ack, err)
				}
				return
			}
			job, err := r.prepare(ctx, scope, *receipt)
			if err != nil {
				t.Fatal(err)
			}
			beforeLedger := avatarRetentionLedger(t, pool, owner)
			if mode == "subject_filter" {
				for i := 0; i < 110; i++ {
					if _, err := reader.StartOperation(ctx, reliability.Operation{ID: fmt.Sprintf("foreign-%s-%d", owner, i), Kind: avatarRetentionKind, SubjectType: "personal_space", SubjectID: "other-owner", ActorID: "other-owner", Details: job.Details}); err != nil {
						t.Fatal(err)
					}
				}
				items, err := reader.ListSubjectOperations(ctx, avatarRetentionKind, "personal_space", owner, 1)
				if err != nil || len(items) != 1 || items[0].ID != job.ID {
					t.Fatal("subject filter after limit", items, err)
				}
				if _, err := reader.GetSubjectOperation(ctx, job.ID, avatarRetentionKind, "personal_space", "other-owner", false); !errors.Is(err, reliability.ErrOperationNotFound) {
					t.Fatal("other owner operation read", err)
				}
				return
			}
			if mode == "inode" {
				ran = true
				mutateSelectedAvatar(t, s, owner, files[3].name, "inode")
				err = r.finish(ctx, *job)
				if !ran || err == nil || beforeLedger != avatarRetentionLedger(t, pool, owner) || avatarUploadFileBytes(t, s, owner)[files[3].name] == nil {
					t.Fatal("replacement inode deleted", err)
				}
				return
			}
			commandCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			hook := &avatarRetentionPGStoreHook{PostgreSQLStore: reader}
			hook.after = func(tx context.Context, phase string) {
				if ran {
					return
				}
				if mode == "cancel_after_bytes" && phase == "bytes" {
					ran = true
					cancel()
					return
				}
				if phase != "finalize" {
					return
				}
				ran = true
				db := transaction.ExecutorFor(tx, pool)
				var e error
				switch mode {
				case "after_session":
					_, e = db.Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='cleanup test' WHERE id=$1", session)
				case "after_space":
					_, e = db.Exec(tx, "UPDATE user_spaces SET cover_image='extra' WHERE user_id=$1", owner)
				case "after_operation":
					_, e = db.Exec(tx, "UPDATE platform_operation_runs SET actor_id='wrong' WHERE id=$1", job.ID)
				}
				if e != nil {
					t.Fatal("actual extra SQL failed", e)
				}
			}
			// Mutations happen in UpdateOperation, before the command's final checks.
			mutating := &avatarRetentionPGUpdateHook{avatarRetentionPGStoreHook: hook, after: hook.after}
			if mode != "cancel_after_bytes" {
				hook.after = nil
			}
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), mutating))
			r.reader = mutating
			err = r.finish(commandCtx, *job)
			if !ran {
				t.Fatal("actual post-write hook not reached", mode, err)
			}
			if mode == "cancel_after_bytes" {
				usage, e := objects.Usage(ctx, owner)
				if e != nil || err != nil || usage.UsedBytes >= sumAvatarSizes(files) {
					t.Fatal("bounded cancelled finalize failed", usage, err, e)
				}
				return
			}
			if err == nil || beforeLedger != avatarRetentionLedger(t, pool, owner) {
				t.Fatal("extra SQL escaped final rollback", mode, err)
			}
			stored, e := reader.GetSubjectOperation(ctx, job.ID, avatarRetentionKind, "personal_space", owner, false)
			if e != nil || !reflect.DeepEqual(*job, *stored) {
				t.Fatal("operation final state escaped rollback", stored, e)
			}
		})
	}
}

type avatarRetentionPGUpdateHook struct {
	*avatarRetentionPGStoreHook
	after func(context.Context, string)
}

func (s *avatarRetentionPGUpdateHook) UpdateOperation(ctx context.Context, op reliability.Operation) error {
	if err := s.PostgreSQLStore.UpdateOperation(ctx, op); err != nil {
		return err
	}
	if s.after != nil {
		s.after(ctx, "finalize")
	}
	return nil
}
func sumAvatarSizes(files []avatarFile) int64 {
	var n int64
	for _, file := range files {
		n += file.size
	}
	return n
}
