package richtext

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func richReadPostgres(t *testing.T, owner string) (*Service, *communityrepo.PgThreadRepository, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_RICHTEXT_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_RICHTEXT_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_richtext_read" {
		t.Fatal("richtext read tests require their owned campusos_v12_02b_richtext_read database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES($1::bigint,$2,$2,$3) ON CONFLICT(id) DO NOTHING`, owner, "rich-read-"+owner, "rich-read-"+owner+"@example.test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(109110,'Rich read group','rich-read-group','group','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(109111,'Rich read board','rich-read-board',109110,'board','active') ON CONFLICT(id) DO NOTHING;
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(109111,'article',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	repo := communityrepo.NewPgThreadRepository(pool)
	threads := communityservice.NewThreadService(repo, nil)
	durable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	threads.SetReliability(durable)
	threads.SetGovernanceRepository(communityrepo.NewPgContentGovernanceRepository(pool))
	threads.SetCategoryRepository(communityrepo.NewPgCategoryRepository(pool))
	threads.SetThreadTypePolicyRepository(communityrepo.NewPgThreadTypePolicyRepository(pool))
	s := NewService(NewPgStore(pool), community.NewContentGateway(repo, threads))
	s.SetReliability(durable)
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), MaxArticleAttachmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	s.SetObjectPort(objects)
	s.SetPDFViewerAuthorizer(func(context.Context, PDFViewerAuthorizationInput) error { return nil })
	return s, repo, pool
}

func richReadDraft(t *testing.T, s *Service, owner string) *ArticleResult {
	t.Helper()
	draft, err := s.CreateDraft(context.Background(), owner, "Rich Reader", SaveArticleRequest{Title: "Private target article", CategoryID: "109111", ContentHTML: "<p>target-private-body</p>"})
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func richReadCommit(t *testing.T, pool *pgxpool.Pool, action func(pgx.Tx) error) {
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

func TestRichTextAuthorizedPostgreSQLStateMatrix(t *testing.T) {
	ctx := context.Background()
	owner := "109101"
	s, repo, _ := richReadPostgres(t, owner)
	for _, publication := range []domain.PublicationStatus{domain.PublicationStatusPublished, domain.PublicationStatusPrivate, domain.PublicationStatusDraft} {
		for _, moderation := range []domain.ModerationStatus{domain.ModerationStatusClear, domain.ModerationStatusPending, domain.ModerationStatusRejected, domain.ModerationStatusTakenDown} {
			for _, deletion := range []domain.DeletionStatus{domain.DeletionStatusActive, domain.DeletionStatusTrashed, domain.DeletionStatusPurged} {
				t.Run(string(publication)+"/"+string(moderation)+"/"+string(deletion), func(t *testing.T) {
					draft := richReadDraft(t, s, owner)
					thread, err := repo.GetByID(ctx, draft.ThreadID)
					if err != nil {
						t.Fatal(err)
					}
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					article, err := s.GetArticleForPrincipal(ctx, draft.ThreadID, richReadProof(owner))
					if (err == nil) != (deletion == domain.DeletionStatusActive) || (err != nil && article != nil) {
						t.Fatalf("owner article=%v err=%v", article, err)
					}
					article, err = s.GetArticleForPrincipal(ctx, draft.ThreadID, nil)
					public := publication == domain.PublicationStatusPublished && moderation == domain.ModerationStatusClear && deletion == domain.DeletionStatusActive
					if (err == nil) != public || (err != nil && article != nil) {
						t.Fatalf("public article=%v err=%v", article, err)
					}
					stored, err := repo.GetByID(ctx, draft.ThreadID)
					if err != nil || stored.ViewCount != 0 {
						t.Fatalf("view counter: %v", err)
					}
				})
			}
		}
	}
}

func TestRichTextAuthorizedPostgreSQLCommittedArticleRaces(t *testing.T) {
	for i, mode := range []string{"body", "publication", "moderation", "trash", "trash-aba", "owner", "owner-aba"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(109200 + i)
			s, _, pool := richReadPostgres(t, owner)
			draft := richReadDraft(t, s, owner)
			store := &richReadStore{Store: s.store}
			store.afterGet = func(n int) {
				if n != 1 {
					return
				}
				richReadCommit(t, pool, func(tx pgx.Tx) error {
					var err error
					switch mode {
					case "body":
						_, err = tx.Exec(ctx, `UPDATE richtext_article_contents SET sanitized_html='<p>changed-body</p>',updated_at=NOW() WHERE thread_id=$1`, draft.ThreadID)
					case "publication":
						_, err = tx.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=$1`, draft.ThreadID)
					case "moderation":
						_, err = tx.Exec(ctx, `UPDATE threads SET moderation_status='taken_down' WHERE id=$1`, draft.ThreadID)
					case "trash", "trash-aba":
						_, err = tx.Exec(ctx, `UPDATE threads SET deletion_status='trashed' WHERE id=$1`, draft.ThreadID)
						if mode == "trash-aba" && err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET deletion_status='active' WHERE id=$1`, draft.ThreadID)
						}
					case "owner", "owner-aba":
						_, err = tx.Exec(ctx, `INSERT INTO users(id,username,nickname,email) VALUES(109999,'rich-other-owner','Other','rich-other-owner@example.test') ON CONFLICT(id) DO NOTHING`)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET author_id=109999 WHERE id=$1`, draft.ThreadID)
						}
						if mode == "owner-aba" && err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET author_id=$2::bigint WHERE id=$1`, draft.ThreadID, owner)
						}
					}
					return err
				})
			}
			s.store = store
			if article, err := s.GetArticleForPrincipal(ctx, draft.ThreadID, richReadProof(owner)); err == nil || article != nil {
				t.Fatalf("stale article=%v err=%v", article, err)
			}
		})
	}
}

func TestRichTextAuthorizedPostgreSQLAttachmentsAndInvocation(t *testing.T) {
	ctx := context.Background()
	owner := "109301"
	s, _, pool := richReadPostgres(t, owner)
	draft := richReadDraft(t, s, owner)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	attachment, err := s.UploadAttachment(ctx, owner, draft.ThreadID, "private.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	proof := richReadProof(owner)
	invocation, err := s.CreateInvocationForPrincipal(ctx, PluginUIInvocationRequest{PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ResourceType: InvocationContextPersonalAsset, ResourceID: attachment.AssetID, Presentation: "modal"}, proof)
	if err != nil {
		t.Fatal(err)
	}
	for _, download := range []bool{false, true} {
		opened, _, err := s.OpenInvocationForPrincipal(ctx, invocation.ID, download, proof)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(opened.Reader)
		_ = opened.Reader.Close()
		if err != nil || !bytes.Equal(raw, pdf) {
			t.Fatal("wrong invocation bytes")
		}
	}
	objects := &richReadObjects{ObjectPort: s.objects}
	objects.afterOpen = func() {
		richReadCommit(t, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE user_assets SET status='quarantined',version=version+1,updated_at=NOW() WHERE id=$1`, attachment.AssetID)
			return err
		})
	}
	s.objects = objects
	if opened, err := s.OpenAttachmentForPrincipal(ctx, draft.ThreadID, attachment.ID, proof); err == nil || opened.Reader != nil || objects.closes != 1 {
		t.Fatalf("quarantine race err=%v closes=%d", err, objects.closes)
	}
	if opened, _, err := s.OpenInvocationForPrincipal(ctx, invocation.ID, true, proof); err == nil || opened.Reader != nil {
		t.Fatal("quarantined invocation read")
	}
}
