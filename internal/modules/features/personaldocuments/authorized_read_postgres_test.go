package personaldocuments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func documentPostgresService(t *testing.T, owner string) (*Service, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_DOCUMENT_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_DOCUMENT_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_document_read" {
		t.Fatal("document tests require their owned campusos_v12_02b_document_read database")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `INSERT INTO users(id,username,nickname,email) VALUES ($1::bigint,$2,$2,$3) ON CONFLICT(id) DO NOTHING`, owner, "document-owner-"+owner, "document-"+owner+"@example.test")
	if err != nil {
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
	return s, pool
}

func documentCommittedChange(t *testing.T, pool *pgxpool.Pool, change func(pgx.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := change(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentAuthenticatedPostgreSQLFormatsAndLifecycle(t *testing.T) {
	ctx := context.Background()
	const owner = "108101"
	s, _ := documentPostgresService(t, owner)
	for _, format := range []string{FormatText, FormatMarkdown, FormatCampusDoc, FormatPDF, FormatDOCX} {
		t.Run(format, func(t *testing.T) {
			var d DocumentDetail
			var err error
			switch format {
			case FormatPDF:
				d, err = s.Upload(ctx, owner, "private.pdf", format, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
			case FormatDOCX:
				raw := validDOCX(t)
				d, err = s.Upload(ctx, owner, "private.docx", format, "application/zip", int64(len(raw)), strings.NewReader(string(raw)))
			case FormatCampusDoc:
				d, err = s.CreateText(ctx, owner, CreateRequest{Name: "private.campusdoc", Format: format, Content: `{"version":1,"blocks":[{"type":"paragraph","text":"private-body"}]}`})
			default:
				name := "private.md"
				if format == FormatText {
					name = "private.txt"
				}
				d, err = s.CreateText(ctx, owner, CreateRequest{Name: name, Format: format, Content: "private-body"})
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range []string{StatusActive, StatusTrashed} {
				if status == StatusTrashed {
					d, err = s.SetStatus(ctx, owner, d.ID, d.Version, status)
					if err != nil {
						t.Fatal(err)
					}
				}
				for _, actor := range []string{"owner", "other-user", "same-ID-admin"} {
					resolve := documentProof(owner)
					if actor == "other-user" {
						resolve = documentProof("108102")
					}
					if actor == "same-ID-admin" {
						resolve = func(ctx context.Context) (identityport.PrincipalContext, error) {
							p, _ := documentProof(owner)(ctx)
							p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
							return p, nil
						}
					}
					metadata, metadataErr := s.GetForPrincipal(ctx, resolve, d.ID)
					if (metadataErr == nil) != (actor == "owner") {
						t.Fatalf("%s/%s metadata=%v", status, actor, metadataErr)
					}
					if status == StatusTrashed && metadataErr == nil && (metadata.CurrentVersion != nil || metadata.CurrentVersionID != "") {
						t.Fatal("tombstone storage references leaked")
					}
					_, stream, readErr := s.OpenVersionForPrincipal(ctx, resolve, d.ID, "")
					allowed := status == StatusActive && actor == "owner"
					if (readErr == nil) != allowed {
						t.Fatalf("%s/%s open=%v", status, actor, readErr)
					}
					if stream.Reader != nil {
						raw, err := io.ReadAll(stream.Reader)
						_ = stream.Reader.Close()
						if err != nil || len(raw) == 0 {
							t.Fatalf("read: %v", err)
						}
					}
				}
			}
		})
	}
}

func TestDocumentAuthenticatedPostgreSQLCommittedRaces(t *testing.T) {
	for i, mode := range []string{"trash", "trash-aba", "body-version", "name", "owner", "object-delete", "object-version", "selected-version", "missing-version"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := stringOwnerID(108200 + i)
			s, pool := documentPostgresService(t, owner)
			d, err := s.CreateText(ctx, owner, CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-body"})
			if err != nil {
				t.Fatal(err)
			}
			base := s.repository
			race := &documentReadRaceRepository{Repository: base}
			race.afterGet = func(n int) {
				if n != 2 {
					return
				}
				documentCommittedChange(t, pool, func(tx pgx.Tx) error {
					var err error
					switch mode {
					case "trash":
						_, err = tx.Exec(ctx, `UPDATE personal_documents SET status='trashed',deleted_at=NOW(),version=version+1 WHERE id=$1`, d.ID)
					case "trash-aba":
						_, err = tx.Exec(ctx, `UPDATE personal_documents SET status='trashed',deleted_at=NOW(),version=version+1 WHERE id=$1`, d.ID)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE personal_documents SET status='active',deleted_at=NULL,version=version+1 WHERE id=$1`, d.ID)
						}
					case "body-version":
						_, err = tx.Exec(ctx, `UPDATE personal_documents SET version=version+1 WHERE id=$1`, d.ID)
					case "name":
						_, err = tx.Exec(ctx, `UPDATE personal_documents SET name='changed',version=version+1 WHERE id=$1`, d.ID)
					case "owner":
						_, err = tx.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(108999,'document-new-owner','other','document-new-owner@example.test') ON CONFLICT(id) DO NOTHING`)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE personal_documents SET owner_user_id=108999,version=version+1 WHERE id=$1`, d.ID)
						}
					case "object-delete":
						_, err = tx.Exec(ctx, `UPDATE storage_objects SET status='deleted',deleted_at=NOW(),version=version+1 WHERE id=$1`, d.CurrentVersion.SourceObjectID)
					case "object-version":
						_, err = tx.Exec(ctx, `UPDATE storage_objects SET version=version+1 WHERE id=$1`, d.CurrentVersion.SourceObjectID)
					case "selected-version":
						_, err = tx.Exec(ctx, `UPDATE personal_document_versions SET sha256=repeat('a',64) WHERE id=$1`, d.CurrentVersionID)
					case "missing-version":
						_, err = tx.Exec(ctx, `UPDATE personal_documents SET current_version_id=NULL,version=version+1 WHERE id=$1`, d.ID)
					}
					return err
				})
			}
			s.repository = race
			_, body, err := s.TextContentForPrincipal(ctx, documentProof(owner), d.ID, "")
			if err == nil || body != "" {
				t.Fatalf("stale private body=%q err=%v", body, err)
			}
		})
	}
}

func TestDocumentAuthenticatedPostgreSQLWholeListAndHistoricalVersion(t *testing.T) {
	ctx := context.Background()
	const owner = "108301"
	s, pool := documentPostgresService(t, owner)
	d, err := s.CreateText(ctx, owner, CreateRequest{Name: "private.txt", Format: FormatText, Content: "old-private-body"})
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := d.CurrentVersionID
	d, err = s.Save(ctx, owner, d.ID, SaveRequest{ExpectedVersion: d.Version, Content: "new-private-body"})
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := s.TextContentForPrincipal(ctx, documentProof(owner), d.ID, oldVersion)
	if err != nil || body != "old-private-body" {
		t.Fatalf("historical body=%q err=%v", body, err)
	}
	base := s.repository
	race := &documentReadRaceRepository{Repository: base}
	race.afterList = func(n int) {
		if n != 1 {
			return
		}
		documentCommittedChange(t, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE personal_documents SET status='trashed',deleted_at=NOW(),version=version+1 WHERE id=$1`, d.ID)
			return err
		})
	}
	s.repository = race
	if list, err := s.ListForPrincipal(ctx, documentProof(owner), StatusActive); err == nil || list != nil {
		t.Fatalf("mixed list: %#v %v", list, err)
	}
	if list, err := s.ListForPrincipal(ctx, documentProof(owner), StatusActive); err != nil || len(list) != 0 {
		t.Fatalf("fresh empty list: %#v %v", list, err)
	}
	if _, body, err := s.TextContentForPrincipal(ctx, documentProof(owner), d.ID, oldVersion); err == nil || body != "" {
		t.Fatal("trash historical bytes allowed")
	}
	if _, err := s.GetForPrincipal(ctx, nil, d.ID); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
}

func stringOwnerID(value int) string {
	return fmt.Sprint(value)
}
