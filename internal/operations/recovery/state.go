package recovery

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrQuarantined = errors.New("recovery.quarantined")

const LockID int64 = 0x435245434f564552
const markerName = ".campusos-recovery.json"

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type State struct {
	Epoch        string `json:"epoch"`
	ArchiveSHA   string `json:"archive_sha256"`
	Phase        string `json:"phase"`
	InventorySHA string `json:"inventory_sha256"`
	EgressPaused bool   `json:"egress_paused"`
}

func NewEpoch() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func DeriveSecret(secret, epoch string) string {
	if epoch == "" {
		return secret
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("campusos/recovery/jwt/v1/" + epoch))
	return hex.EncodeToString(h.Sum(nil))
}
func ReadMarker(root string) (*State, error) {
	name := filepath.Join(root, markerName)
	info, e := os.Lstat(name)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, ErrQuarantined
	}
	raw, e := os.ReadFile(name)
	if e != nil {
		return nil, ErrQuarantined
	}
	var s State
	if json.Unmarshal(raw, &s) != nil || !shaPattern.MatchString(s.Epoch) || !shaPattern.MatchString(s.ArchiveSHA) || (s.Phase != "quarantined" && s.Phase != "reviewed") {
		return nil, ErrQuarantined
	}
	return &s, nil
}
func WriteMarker(root string, s State) error {
	if !shaPattern.MatchString(s.Epoch) || !shaPattern.MatchString(s.ArchiveSHA) {
		return ErrQuarantined
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(root, ".recovery-marker-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(raw, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), filepath.Join(root, markerName)); e != nil {
		return e
	}
	d, e := os.Open(root)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readState(ctx context.Context, q queryer) (*State, error) {
	var s State
	e := q.QueryRow(ctx, `SELECT epoch,archive_sha256,phase,inventory_sha256,egress_paused FROM instance_recovery_epochs WHERE singleton`).Scan(&s.Epoch, &s.ArchiveSHA, &s.Phase, &s.InventorySHA, &s.EgressPaused)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, ErrQuarantined
	}
	return &s, nil
}

// Startup holds a shared database maintenance lock for the process lifetime.
// The marker survives failed SQL/file application; either half alone blocks.
func Startup(ctx context.Context, pool *pgxpool.Pool, root string) (*State, func(), error) {
	marker, e := ReadMarker(root)
	if e != nil {
		return nil, nil, e
	}
	if pool == nil {
		if marker != nil {
			return nil, nil, ErrQuarantined
		}
		return nil, func() {}, nil
	}
	conn, e := pool.Acquire(ctx)
	if e != nil {
		return nil, nil, ErrQuarantined
	}
	release := func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock_shared($1)`, LockID)
		conn.Release()
	}
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock_shared($1)`, LockID).Scan(&locked); e != nil || !locked {
		conn.Release()
		return nil, nil, ErrQuarantined
	}
	state, e := readState(ctx, conn)
	if e != nil {
		release()
		return nil, nil, e
	}
	if marker == nil && state == nil {
		return nil, release, nil
	}
	if marker == nil || state == nil || marker.Epoch != state.Epoch || marker.ArchiveSHA != state.ArchiveSHA || state.Phase != "reviewed" || marker.Phase != "reviewed" || marker.InventorySHA != state.InventorySHA || marker.EgressPaused != state.EgressPaused {
		release()
		return nil, nil, ErrQuarantined
	}
	return state, release, nil
}

func exclusive(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, func(), error) {
	c, e := pool.Acquire(ctx)
	if e != nil {
		return nil, nil, e
	}
	var ok bool
	if e = c.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, LockID).Scan(&ok); e != nil || !ok {
		c.Release()
		return nil, nil, errors.New("recovery.stop_all_api_processes")
	}
	return c, func() { _, _ = c.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, LockID); c.Release() }, nil
}

// Quarantine runs after a complete SQL restore, while the exclusive lock and
// the prewritten marker keep every normal Server from starting. It preserves
// rows/audits and invalidates authority rather than deleting security history.
func Quarantine(ctx context.Context, c *pgxpool.Conn, state State) error {
	if !shaPattern.MatchString(state.Epoch) || !shaPattern.MatchString(state.ArchiveSHA) {
		return ErrQuarantined
	}
	tx, e := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	statements := []string{
		`UPDATE users SET auth_version=auth_version+1`,
		`UPDATE admin_accounts SET version=version+1,updated_at=now()`,
		`UPDATE sessions SET revoked_at=now(),revoke_reason='whole_restore',updated_at=now() WHERE revoked_at IS NULL`,
		`UPDATE admin_sessions SET revoked_at=now(),revoke_reason='whole_restore',version=version+1,updated_at=now() WHERE revoked_at IS NULL`,
		`UPDATE integration_credentials SET status='revoked' WHERE status='active'`,
		`UPDATE integration_principals SET status='suspended' WHERE status='active'`,
		`UPDATE api_keys SET is_active=false WHERE is_active`,
		`UPDATE identity_delegations SET status='revoked',revoked_at=now() WHERE status<>'revoked'`,
		`UPDATE identity_email_challenges SET consumed_at=now() WHERE consumed_at IS NULL`,
		`UPDATE identity_mfa_tickets SET consumed_at=now() WHERE consumed_at IS NULL`,
		`UPDATE identity_mfa_recovery_codes SET used_at=now() WHERE used_at IS NULL`,
		`UPDATE admin_mfa_tickets SET consumed_at=now(),updated_at=now() WHERE consumed_at IS NULL`,
		`UPDATE admin_recovery_tickets SET consumed_at=now(),updated_at=now() WHERE consumed_at IS NULL`,
		`UPDATE identity_mfa_totp_methods SET last_accepted_step=GREATEST(last_accepted_step,floor(extract(epoch from now())/30)::bigint),updated_at=now() WHERE status='active'`,
		`UPDATE admin_mfa_totp SET last_accepted_step=GREATEST(last_accepted_step,floor(extract(epoch from now())/30)::bigint),version=version+1,updated_at=now() WHERE status='active'`,
		`UPDATE identity_account_recovery_cases SET status='cancelled',cancelled_at=now(),updated_at=now() WHERE status='pending'`,
		`UPDATE webhook_endpoints SET enabled=false,updated_at=now() WHERE enabled`,
		`UPDATE webhook_deliveries SET status='failed',error_message='whole_restore',dead_lettered_at=now(),updated_at=now() WHERE status IN ('pending','retrying','sending','processing')`,
		`UPDATE message_bindings SET deleted_at=now(),updated_at=now() WHERE deleted_at IS NULL`,
		`UPDATE plugin_v5_admin_grants SET state=state||jsonb_build_object('approved',false,'revision',((state->>'revision')::bigint+1)::text)`,
		`UPDATE plugin_v5_user_consents SET state=state||jsonb_build_object('accepted',false,'revision',((state->>'revision')::bigint+1)::text)`,
		`UPDATE plugin_operations SET state=state||jsonb_build_object('status','superseded','error_code','whole_restore','lease_until_ms',0,'revision',((state->>'revision')::bigint+1)::text) WHERE state->>'status' IN ('queued','working','waiting')`,
	}
	for _, table := range []string{"plugin_installations", "plugin_user_installations"} {
		statements = append(statements, `UPDATE `+table+` SET state=state||jsonb_build_object('desired',CASE WHEN (state->>'installed')::boolean THEN 'disabled' ELSE 'uninstalled' END,'observed','stopped','active_generation','0','observed_generation','0','generation',nextval('plugin_v5_generations')::text,'revision',((state->>'revision')::bigint+1)::text,'error_code','whole_restore')`)
	}
	for _, statement := range statements {
		if _, e = tx.Exec(ctx, statement); e != nil {
			return e
		}
	}
	audit, _ := json.Marshal(map[string]any{"action": "whole_restore.quarantine", "epoch": state.Epoch, "archive_sha256": state.ArchiveSHA, "at": time.Now().UTC()})
	_, e = tx.Exec(ctx, `INSERT INTO instance_recovery_epochs(singleton,epoch,archive_sha256,phase,egress_paused,audit) VALUES(true,$1,$2,'quarantined',true,jsonb_build_array($3::jsonb)) ON CONFLICT(singleton) DO UPDATE SET epoch=excluded.epoch,archive_sha256=excluded.archive_sha256,phase='quarantined',inventory_sha256='',reviewed_by='',review_reason='',rotated_at=now(),reviewed_at=NULL,egress_paused=true,audit=instance_recovery_epochs.audit||excluded.audit`, state.Epoch, state.ArchiveSHA, audit)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Inventory deliberately excludes passwords, secret values, tokens and email.
// Its private local result identifies the exact restored authorization facts a
// trusted operator must review; a stale digest cannot release the barrier.
func inventory(ctx context.Context, q queryer) (json.RawMessage, string, error) {
	var raw []byte
	e := q.QueryRow(ctx, `SELECT jsonb_build_object(
 'users',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'status',status,'auth_version',auth_version) ORDER BY id),'[]') FROM users),
 'admins',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'status',status,'version',version) ORDER BY id),'[]') FROM admin_accounts),
 'user_roles',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY user_id,role_id,id),'[]') FROM user_roles r),
 'role_permissions',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY role_id,permission_id,id),'[]') FROM role_permissions r),
 'admin_roles',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM admin_role_bindings r),
 'admin_permissions',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY role_id,permission_code),'[]') FROM admin_role_permissions r),
 'grants',(SELECT COALESCE(jsonb_agg(state ORDER BY record_id),'[]') FROM plugin_v5_admin_grants),
 'consents',(SELECT COALESCE(jsonb_agg(state ORDER BY record_id),'[]') FROM plugin_v5_user_consents),
 'integrations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'status',status,'facts_version',facts_version) ORDER BY id),'[]') FROM integration_principals)
 )::text`).Scan(&raw)
	if e != nil {
		return nil, "", e
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func Review(ctx context.Context, pool *pgxpool.Pool, root, epoch, expected, actor, reason string) (*State, error) {
	if !shaPattern.MatchString(epoch) || !shaPattern.MatchString(expected) || len(actor) > 128 || strings.TrimSpace(actor) == "" || len(reason) > 500 || strings.TrimSpace(reason) == "" {
		return nil, ErrQuarantined
	}
	c, unlock, e := exclusive(ctx, pool)
	if e != nil {
		return nil, e
	}
	defer unlock()
	marker, e := ReadMarker(root)
	if e != nil || marker == nil || marker.Epoch != epoch {
		return nil, ErrQuarantined
	}
	tx, e := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	s, e := readState(ctx, tx)
	if e != nil || s == nil || s.Epoch != epoch || (s.Phase != "quarantined" && s.Phase != "reviewed") || s.ArchiveSHA != marker.ArchiveSHA {
		return nil, ErrQuarantined
	}
	_, actual, e := inventory(ctx, tx)
	if e != nil {
		return nil, e
	}
	if actual != expected {
		return nil, errors.New("recovery.inventory_changed")
	}
	if s.Phase == "reviewed" && s.InventorySHA == actual {
		// The DB commit can precede a failed marker fsync/rename. Repair that
		// half without rotating again or duplicating the review evidence.
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
		if e = WriteMarker(root, *s); e != nil {
			return nil, e
		}
		return s, nil
	}
	audit, _ := json.Marshal(map[string]any{"action": "whole_restore.review", "epoch": epoch, "inventory_sha256": actual, "operator": actor, "reason": reason, "at": time.Now().UTC()})
	_, e = tx.Exec(ctx, `UPDATE instance_recovery_epochs SET phase='reviewed',inventory_sha256=$1,reviewed_by=$2,review_reason=$3,reviewed_at=now(),audit=audit||jsonb_build_array($4::jsonb) WHERE singleton`, actual, actor, reason, audit)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	s.Phase = "reviewed"
	s.InventorySHA = actual
	if e = WriteMarker(root, *s); e != nil {
		return nil, e
	}
	return s, nil
}
