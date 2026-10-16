package personaldocuments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func documentAuthorPostgres(t *testing.T, owner string) (*Service, *pgxpool.Pool, DocumentPrincipalResolver, *auth.JWTClaims) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("document writes require owned campusos_v12_02b_author_write database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	users := identityrepo.NewPgUserRepository(pool)
	u := &identitydomain.User{ID: owner, Username: "doc-writer-" + owner, Email: owner + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), maxBinaryBytes)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewService(NewPgRepository(pool), objects)
	if err != nil {
		t.Fatal(err)
	}
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "document-write-pg-test-secret", Issuer: "document-write-pg", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	session, err := identityservice.NewSessionService(identityrepo.NewPgSessionRepository(pool), users, jwt, identityservice.SessionConfig{IPHashSecret: "document-write-pg-test-ip"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := session.Issue(ctx, u, identityservice.SessionMetadata{DeviceName: "Document writer"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, session, session, claims)
	}
	return s, pool, proof, claims
}

func documentWriterFixture(t *testing.T, s *Service, owner, operation string) (DocumentDetail, string) {
	t.Helper()
	if operation == "create" || operation == "upload" {
		return DocumentDetail{}, ""
	}
	d, err := s.CreateText(context.Background(), owner, CreateRequest{Name: "private.txt", Format: FormatText, Content: "original body"})
	if err != nil {
		t.Fatal(err)
	}
	selected := d.CurrentVersionID
	if operation == "restore_version" {
		d, err = s.Save(context.Background(), owner, d.ID, SaveRequest{ExpectedVersion: d.Version, Content: "second body"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if operation == "restore" {
		d, err = s.SetStatus(context.Background(), owner, d.ID, d.Version, StatusTrashed)
		if err != nil {
			t.Fatal(err)
		}
	}
	return d, selected
}
func runDocumentPGAuthorWrite(s *Service, proof DocumentPrincipalResolver, d DocumentDetail, selected, operation string) (DocumentDetail, error) {
	ctx := context.Background()
	if operation == "create" {
		return s.CreateTextForPrincipal(ctx, proof, CreateRequest{Name: "private.txt", Format: FormatText, Content: "body"})
	}
	if operation == "upload" {
		return s.UploadForPrincipal(ctx, proof, "private.pdf", FormatPDF, "forged", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
	}
	return runDocumentAuthorWrite(s, ctx, proof, d, selected, operation)
}

func documentAuthorCounts(t *testing.T, pool *pgxpool.Pool, owner string) []int {
	t.Helper()
	out := make([]int, 6)
	for i, q := range []string{`SELECT count(*) FROM personal_documents WHERE owner_user_id=$1`, `SELECT count(*) FROM personal_document_versions WHERE created_by=$1`, `SELECT count(*) FROM personal_document_previews WHERE document_version_id IN(SELECT id FROM personal_document_versions WHERE created_by=$1)`, `SELECT count(*) FROM platform_command_audits WHERE actor_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_id IN(SELECT id::text FROM personal_document_versions WHERE created_by=$1)`, `SELECT count(*) FROM storage_objects WHERE owner_user_id=$1 AND status='ready'`} {
		if err := pool.QueryRow(context.Background(), q, owner).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

type documentObjectLockProbe struct {
	corestorage.ObjectPort
	locker corestorage.OwnedObjectCommandLocker
	after  func(context.Context, corestorage.Object)
}

func (p *documentObjectLockProbe) LockOwnedObjectForCommand(ctx context.Context, owner, id string) (corestorage.Object, error) {
	o, err := p.locker.LockOwnedObjectForCommand(ctx, owner, id)
	if err == nil && p.after != nil {
		p.after(ctx, o)
	}
	return o, err
}

func TestDocumentAuthorPostgreSQLAtomicLocks(t *testing.T) {
	for i, operation := range []string{"create", "upload", "save", "restore_version", "trash", "restore"} {
		t.Run(operation, func(t *testing.T) {
			owner := fmt.Sprint(116501 + i)
			s, pool, proof, claims := documentAuthorPostgres(t, owner)
			d, selected := documentWriterFixture(t, s, owner, operation)
			checked := false
			probe := func(ctx context.Context, object *corestorage.Object) {
				checked = true
				queries := []struct{ sql, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, owner}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}}
				if d.ID != "" {
					queries = append(queries, struct{ sql, id string }{`SELECT id FROM personal_documents WHERE id=$1 FOR UPDATE NOWAIT`, d.ID}, struct{ sql, id string }{`SELECT id FROM personal_document_versions WHERE id=$1 FOR UPDATE NOWAIT`, d.CurrentVersionID})
				}
				if operation == "restore_version" {
					queries = append(queries, struct{ sql, id string }{`SELECT id FROM personal_document_versions WHERE id=$1 FOR UPDATE NOWAIT`, selected})
				}
				if object != nil {
					queries = append(queries, struct{ sql, id string }{`SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT`, object.ID})
				}
				for _, q := range queries {
					_, err := pool.Exec(context.Background(), q.sql, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("document facts not actually locked", q.sql, err)
					}
				}
			}
			if operation == "trash" || operation == "restore" {
				s.repository = &documentRepositoryLockProbe{Repository: s.repository, locker: s.repository.(documentAuthorLocker), after: func(ctx context.Context, _ DocumentDetail) { probe(ctx, nil) }}
			} else {
				s.objects = &documentObjectLockProbe{ObjectPort: s.objects, locker: s.objects.(corestorage.OwnedObjectCommandLocker), after: func(ctx context.Context, o corestorage.Object) { probe(ctx, &o) }}
			}
			result, err := runDocumentPGAuthorWrite(s, proof, d, selected, operation)
			if err != nil || !checked {
				t.Fatal(result, err, checked)
			}
			if operation == "trash" && (result.CurrentVersionID != "" || result.CurrentVersion != nil) {
				t.Fatal("tombstone source leak")
			}
			if operation == "restore_version" {
				_, body, e := s.TextContent(context.Background(), owner, d.ID)
				if e != nil || body != "original body" {
					t.Fatal(body, e)
				}
			}
		})
	}
}

type documentRepositoryLockProbe struct {
	Repository
	locker documentAuthorLocker
	after  func(context.Context, DocumentDetail)
}

func (p *documentRepositoryLockProbe) LockDocumentForAuthorCommand(ctx context.Context, owner, id, selected string) (DocumentDetail, error) {
	d, err := p.locker.LockDocumentForAuthorCommand(ctx, owner, id, selected)
	if err == nil && p.after != nil {
		p.after(ctx, d)
	}
	return d, err
}

func TestDocumentAuthorPostgreSQLAuditRollback(t *testing.T) {
	for i, operation := range []string{"create", "upload", "save", "restore_version", "trash", "restore"} {
		t.Run(operation, func(t *testing.T) {
			owner := fmt.Sprint(116601 + i)
			s, pool, proof, _ := documentAuthorPostgres(t, owner)
			d, selected := documentWriterFixture(t, s, owner, operation)
			counts := documentAuthorCounts(t, pool, owner)
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), documentAuditFailure{reliability.NewPostgreSQLStore(pool)}))
			result, err := runDocumentPGAuthorWrite(s, proof, d, selected, operation)
			if err == nil || result.ID != "" {
				t.Fatal("required audit accepted", result, err)
			}
			if got := documentAuthorCounts(t, pool, owner); !reflect.DeepEqual(counts, got) {
				t.Fatal("partial document/preview/outbox/ready object", counts, got)
			}
			if d.ID != "" {
				actual, e := s.repository.Get(context.Background(), owner, d.ID)
				if e != nil || !sameDocumentFacts(d, actual) {
					t.Fatal("partial aggregate", actual, e)
				}
			}
		})
	}
}

func TestDocumentAuthorPostgreSQLCommittedChanges(t *testing.T) {
	operations := []string{"create", "upload", "save", "restore_version", "trash", "restore"}
	for i, operation := range operations {
		modes := []string{"session", "auth_version"}
		if operation == "create" || operation == "upload" || operation == "save" || operation == "restore_version" {
			modes = append(modes, "object")
		}
		if operation != "create" && operation != "upload" {
			modes = append(modes, "document_aba", "current_version")
		}
		if operation == "restore_version" {
			modes = append(modes, "selected_version", "source_object")
		}
		for j, mode := range modes {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				owner := fmt.Sprint(116701 + 20*i + j)
				s, pool, proof, claims := documentAuthorPostgres(t, owner)
				d, selected := documentWriterFixture(t, s, owner, operation)
				var historical DocumentVersion
				if d.ID != "" {
					var err error
					historical, err = s.repository.Version(context.Background(), owner, d.ID, selected)
					if err != nil {
						t.Fatal(err)
					}
				}
				counts := documentAuthorCounts(t, pool, owner)
				changed := false
				var changedObjectID string
				var changedObjectVersion int64
				resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
					if transaction.Active(ctx) && !changed {
						changed = true
						documentCommittedChange(t, pool, func(tx pgx.Tx) error {
							var err error
							switch mode {
							case "session":
								_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=NOW() WHERE id=$1`, claims.SessionID)
							case "auth_version":
								_, err = tx.Exec(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, owner)
							case "object":
								err = tx.QueryRow(ctx, `UPDATE storage_objects SET version=version+1 WHERE id=(SELECT id FROM storage_objects WHERE owner_user_id=$1 AND status='ready' ORDER BY id DESC LIMIT 1) RETURNING id::text,version`, owner).Scan(&changedObjectID, &changedObjectVersion)
							case "document_aba":
								_, err = tx.Exec(ctx, `UPDATE personal_documents SET name='changed.txt',version=version+1 WHERE id=$1`, d.ID)
								if err == nil {
									_, err = tx.Exec(ctx, `UPDATE personal_documents SET name=$2,version=version+1 WHERE id=$1`, d.ID, d.Name)
								}
							case "current_version":
								_, err = tx.Exec(ctx, `UPDATE personal_document_versions SET sha256=repeat('a',64) WHERE id=$1`, d.CurrentVersionID)
							case "selected_version":
								_, err = tx.Exec(ctx, `UPDATE personal_document_versions SET size_bytes=size_bytes+1 WHERE id=$1`, selected)
							case "source_object":
								_, err = tx.Exec(ctx, `UPDATE storage_objects SET version=version+1 WHERE id=$1`, historical.SourceObjectID)
							}
							return err
						})
					}
					return proof(ctx)
				}
				result, err := runDocumentPGAuthorWrite(s, resolve, d, selected, operation)
				if err == nil || result.ID != "" || !changed {
					t.Fatal("committed change accepted", result, err, changed)
				}
				if mode == "object" {
					if !errors.Is(err, ErrDocumentCompensationPending) {
						t.Fatal("changed receipt cleanup was hidden", err)
					}
					// The independently changed new Object cannot be deleted with
					// the original receipt. It remains owned, with no document
					// reference; no business row, audit or outbox may persist.
					counts[5]++
					var unreferenced int
					if e := pool.QueryRow(context.Background(), `SELECT count(*) FROM storage_objects o WHERE o.owner_user_id=$1 AND o.id=$2 AND o.status='ready' AND o.version=$3 AND NOT EXISTS(SELECT 1 FROM personal_document_versions v WHERE v.source_object_id=o.id)`, owner, changedObjectID, changedObjectVersion).Scan(&unreferenced); e != nil || unreferenced != 1 {
						t.Fatal("changed staging receipt was deleted or adopted", unreferenced, e)
					}
				}
				if got := documentAuthorCounts(t, pool, owner); !reflect.DeepEqual(counts, got) {
					t.Fatal("failed race persisted command", counts, got)
				}
				// These adversarial direct-SQL fixtures intentionally violate the
				// immutable source receipt. Restore that fixture after proving denial
				// so the subsequent common data audit checks a valid owned database.
				if mode == "current_version" {
					if _, e := pool.Exec(context.Background(), `UPDATE personal_document_versions SET sha256=$2 WHERE id=$1`, d.CurrentVersionID, d.CurrentVersion.SHA256); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "selected_version" {
					if _, e := pool.Exec(context.Background(), `UPDATE personal_document_versions SET size_bytes=$2 WHERE id=$1`, selected, historical.SizeBytes); e != nil {
						t.Fatal(e)
					}
				}
			})
		}
	}
}
