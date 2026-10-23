package knowledge

import (
	"context"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type privateIndexFixtureReader struct {
	src     PrivateSource
	denied  bool
	fault   bool
	timeout bool
}

func (r *privateIndexFixtureReader) ReadPrivateSource(ctx context.Context, resolve identityport.SessionPrincipalResolver, k, id string, body bool) (PrivateSource, error) {
	if r.fault {
		return PrivateSource{}, ErrPrivateUnavailable
	}
	p, e := resolve(ctx)
	if e != nil {
		return PrivateSource{}, e
	}
	if p.Actor.ID != "543101" || r.denied {
		return PrivateSource{}, ErrPrivateSourceMissing
	}
	out := r.src
	if !body {
		out.Text = ""
	}
	return out, nil
}
func (r *privateIndexFixtureReader) CurrentPrivateSource(ctx context.Context, _ string, _ string, _ string, _ string) (bool, error) {
	if r.timeout {
		<-ctx.Done()
		return false, ctx.Err()
	}
	if r.fault {
		return false, ErrPrivateUnavailable
	}
	return !r.denied, nil
}

func TestPrivateIndexPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_PRIVATE_INDEX_DSN")
	if dsn == "" {
		t.Skip("owned private index PG required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || !strings.HasPrefix(cfg.ConnConfig.Database, "campusos_v12_13_") {
		t.Fatal("owned PG required")
	}
	pool, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	ctx := t.Context()
	if _, e = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(543101,'private-index-first','private','private-first@example.test'),(543102,'private-index-second','other','private-second@example.test') ON CONFLICT DO NOTHING`); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	vault, e := NewPrivateVault(dir)
	if e != nil {
		t.Fatal(e)
	}
	reader := &privateIndexFixtureReader{src: PrivateSource{Kind: "personal_document", ID: "543201", Revision: strings.Repeat("a", 64), Format: "text", Title: "PRIVATE_TITLE_UNIQUE", Text: "PRIVATE_BODY_UNIQUE 校园食堂时间"}}
	repo := NewPgRepository(pool)
	svc := &PrivateIndexService{repo: repo, vault: vault, reader: reader, reliable: reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)), enabled: func() bool { return true }}
	resolve := func(context.Context) (identityport.PrincipalContext, error) {
		p := privateTestPrincipal()
		p.Actor.ID = "543101"
		return p, nil
	}
	other := func(context.Context) (identityport.PrincipalContext, error) {
		p := privateTestPrincipal()
		p.Actor.ID = "543102"
		return p, nil
	}
	row, e := svc.Select(ctx, resolve, "personal_document", "543201", reader.src.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	if row.State != "pending" {
		t.Fatal("no durable task")
	}
	var cipher []byte
	if e = pool.QueryRow(ctx, `SELECT ciphertext FROM knowledge_private_sources WHERE id=$1`, row.ID).Scan(&cipher); e != nil || strings.Contains(string(cipher), "PRIVATE_BODY_UNIQUE") {
		t.Fatal("plaintext index", e)
	}
	if _, e = svc.Select(ctx, resolve, "personal_document", "543201", reader.src.Revision, false); !errors.Is(e, ErrInvalid) {
		t.Fatal("implicit consent")
	}
	if _, e = svc.Select(ctx, other, "personal_document", "543201", reader.src.Revision, true); !errors.Is(e, ErrPrivateSourceMissing) {
		t.Fatal("cross owner", e)
	}
	if e = svc.tick(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := svc.Search(ctx, resolve, "校园")
	if e != nil || page.Total != 1 || !strings.Contains(page.Items[0].Snippet, "食堂") {
		t.Fatalf("search %+v %v", page, e)
	}
	page, e = svc.Search(ctx, other, "校园")
	if e != nil || page.Total != 0 {
		t.Fatal("owner corpus", page, e)
	}
	if _, e = svc.Source(ctx, other, row.ID, row.Revision); !errors.Is(e, ErrPrivateSourceMissing) {
		t.Fatal("cross owner citation", e)
	}
	// Persist an old complete ciphertext row as a database backup oracle.
	old, e := repo.privateGet(ctx, "543101", row.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Withdraw(ctx, resolve, row.ID); e != nil {
		t.Fatal(e)
	}
	if vault.Has(row.ID) {
		t.Fatal("key survived withdrawal")
	}
	page, e = svc.Search(ctx, resolve, "校园")
	if e != nil || page.Total != 0 {
		t.Fatal("new request after withdrawal", page, e)
	}
	// Restoring the old database row never restores external retirement custody.
	if _, e = pool.Exec(ctx, `INSERT INTO knowledge_private_sources(id,owner_user_id,kind,source_id,revision,format,consent_version,state,nonce,ciphertext) VALUES($1,$2::bigint,$3,$4::bigint,$5,$6,$7,'ready',$8,$9)`, old.ID, old.Owner, old.Kind, old.SourceID, old.Revision, old.Format, old.Version, old.Envelope.Nonce, old.Envelope.Ciphertext); e != nil {
		t.Fatal(e)
	}
	page, e = svc.Search(ctx, resolve, "校园")
	if e != nil || page.Total != 0 {
		t.Fatal("old backup revived private result", page, e)
	}
	if _, e = svc.Source(ctx, resolve, row.ID, row.Revision); e == nil {
		t.Fatal("old backup citation decrypted")
	}
	if e = svc.Withdraw(ctx, resolve, row.ID); e != nil {
		t.Fatal(e)
	}
	// Audit rollback leaves neither consent nor a live orphan key.
	if _, e = pool.Exec(ctx, `CREATE FUNCTION fail_private_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned private audit fault'; END $$; CREATE TRIGGER fail_private_audit BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION fail_private_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Select(ctx, resolve, "personal_document", "543201", reader.src.Revision, true); e == nil {
		t.Fatal("audit failure committed")
	}
	if _, e = pool.Exec(ctx, `DROP TRIGGER fail_private_audit ON platform_command_audits;DROP FUNCTION fail_private_audit()`); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_private_sources`).Scan(&count); e != nil || count != 0 {
		t.Fatal("audit rollback", count, e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range entries {
		if filepath.Ext(x.Name()) == ".key" {
			t.Fatal("failed consent retained key")
		}
	}
	row, e = svc.Select(ctx, resolve, "personal_document", "543201", reader.src.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	var first, second privateRow
	claim := func(out *privateRow) error {
		return svc.reliable.CheckedRead(ctx, func(tx context.Context) error {
			var e error
			*out, e = repo.privateClaim(tx, time.Now().UnixMilli())
			return e
		})
	}
	if e = claim(&first); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE knowledge_private_tasks SET lease_until_ms=0 WHERE source_id=$1`, row.ID); e != nil {
		t.Fatal(e)
	}
	if e = claim(&second); e != nil || second.Lease <= first.Lease {
		t.Fatal("lease reused", e)
	}
	e = svc.reliable.CheckedRead(ctx, func(tx context.Context) error { return repo.privateFinish(tx, first, time.Now().UnixMilli(), "") })
	if !errors.Is(e, ErrConflict) {
		t.Fatal("old lease published", e)
	}
	e = svc.reliable.CheckedRead(ctx, func(tx context.Context) error { return repo.privateFinish(tx, second, time.Now().UnixMilli(), "") })
	if e != nil {
		t.Fatal(e)
	}
	reader.timeout = true
	if _, e = pool.Exec(ctx, `UPDATE knowledge_private_tasks SET available_at_ms=0 WHERE source_id=$1`, row.ID); e != nil {
		t.Fatal(e)
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	e = svc.tick(short)
	cancel()
	reader.timeout = false
	if e != nil {
		t.Fatal("timeout failure was not durably recorded", e)
	}
	reader.fault = true
	if _, e = svc.Search(ctx, resolve, "校园"); e == nil {
		t.Fatal("infrastructure fault became a successful empty search")
	}
	for attempt := 0; attempt < 5; attempt++ {
		if _, e = pool.Exec(ctx, `UPDATE knowledge_private_tasks SET available_at_ms=0 WHERE source_id=$1 AND state='retry'`, row.ID); e != nil {
			t.Fatal(e)
		}
		e = svc.tick(ctx)
		if e != nil && !errors.Is(e, ErrPrivateSourceMissing) {
			t.Fatal(e)
		}
	}
	failed, e := repo.privateGet(ctx, "543101", row.ID, false)
	if e != nil || failed.TaskState != "dead" || !vault.Has(row.ID) {
		t.Fatal("bounded transient failure erased consent/key", failed.TaskState, e)
	}
	reader.fault = false
	if _, e = pool.Exec(ctx, `UPDATE knowledge_private_tasks SET state='dead',attempts=5,error_code='source_unavailable' WHERE source_id=$1`, row.ID); e != nil {
		t.Fatal(e)
	}
	if e = svc.Retry(ctx, other, row.ID); !errors.Is(e, ErrPrivateSourceMissing) {
		t.Fatal("cross-owner retry", e)
	}
	if e = svc.Retry(ctx, resolve, row.ID); e != nil {
		t.Fatal("explicit retry", e)
	}
	if e = svc.Retry(ctx, resolve, row.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("automatic replay admitted", e)
	}
	if e = svc.tick(ctx); e != nil {
		t.Fatal("normal retry execution", e)
	}
	reader.denied = true
	page, e = svc.Search(ctx, resolve, "校园")
	if e != nil || page.Total != 0 {
		t.Fatal("stale current ACL", page, e)
	}
	if _, e = pool.Exec(ctx, `UPDATE knowledge_private_tasks SET available_at_ms=0 WHERE source_id=$1`, row.ID); e != nil {
		t.Fatal(e)
	}
	if e = svc.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if vault.Has(row.ID) {
		t.Fatal("source removal retained derivative key")
	}
	if _, e = repo.privateGet(ctx, "543101", row.ID, false); !errors.Is(e, ErrPrivateSourceMissing) {
		t.Fatal("source removal cleanup", e)
	}
	t.Log("real PG encrypted selection, default consent denial, owner isolation, native task, withdrawal, old ciphertext restore denial, audit rollback, lease fencing and source cleanup passed")
}
