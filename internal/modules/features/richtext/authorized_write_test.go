package richtext

import (
	"context"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestRichTextAuthorWriteUpdate(t *testing.T)  { runArticleAuthorWrite(t, "update") }
func TestRichTextAuthorWritePublish(t *testing.T) { runArticleAuthorWrite(t, "publish") }
func TestRichTextAuthorWriteOffline(t *testing.T) { runArticleAuthorWrite(t, "offline") }
func TestRichTextAuthorWriteTrash(t *testing.T)   { runArticleAuthorWrite(t, "trash") }
func runArticleAuthorWrite(t *testing.T, operation string) {
	for _, mode := range []string{"current", "other", "admin", "nil-proof", "missing-command", "trashed", "article-drift", "credential", "audit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, cats, _, events, core := articleCreateFixture(t)
			created, err := s.CreateDraft(ctx, "1001", "Owner", articleCreateRequest())
			if err != nil {
				t.Fatal(err)
			}
			var proof identityport.SessionPrincipalResolver = articleCreateProof
			switch mode {
			case "other":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := articleCreateProof(ctx)
					p.Actor.ID = "1002"
					return p, nil
				}
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := articleCreateProof(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "nil-proof":
				proof = nil
			case "missing-command":
				s.community = articleRawGateway{s.community}
			case "trashed":
				row, _ := threads.GetByID(ctx, created.ThreadID)
				row.DeletionStatus = "trashed"
				if err := threads.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			persisted := s.store.(transaction.Snapshotter)
			beforeThread, beforeArticle, beforeEvent := threads.Snapshot(), persisted.Snapshot(), events.Snapshot()
			if mode == "audit" {
				d := reliability.NewService(transaction.NewMemory(cats, persisted, events), articleCreateFailAudit{events})
				core.SetReliability(d)
				s.SetReliability(d)
			}
			if mode == "article-drift" || mode == "credential" {
				base := proof
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					p, _ := base(ctx)
					if calls == 2 {
						if mode == "credential" {
							p.CredentialID = "changed"
						} else {
							a, _ := s.store.GetArticleByThreadID(ctx, created.ThreadID)
							a.Summary = "external"
							if err := s.store.UpdateArticle(ctx, a); err != nil {
								t.Fatal(err)
							}
						}
					}
					return p, nil
				}
			}
			switch operation {
			case "update":
				_, err = s.UpdateDraftForPrincipal(ctx, created.ThreadID, SaveArticleRequest{Title: "Changed", ContentHTML: "<p>new body</p>"}, proof)
			case "publish":
				_, err = s.PublishForPrincipal(ctx, created.ThreadID, proof)
			case "offline":
				_, err = s.OfflineForPrincipal(ctx, created.ThreadID, proof)
			case "trash":
				err = s.DeleteForPrincipal(ctx, created.ThreadID, proof)
			}
			if (err == nil) != (mode == "current") {
				t.Fatal(err)
			}
			if mode == "audit" || mode == "credential" {
				if !reflect.DeepEqual(beforeThread, threads.Snapshot()) || !reflect.DeepEqual(beforeArticle, persisted.Snapshot()) || !reflect.DeepEqual(beforeEvent, events.Snapshot()) {
					t.Fatal("partial article/audit/outbox survived")
				}
			}
			if err == nil {
				a, _ := s.store.GetArticleByThreadID(ctx, created.ThreadID)
				row, _ := threads.GetByID(ctx, created.ThreadID)
				if a.CreatedBy != "1001" || a.UpdatedBy != "1001" || a.Title != row.Title {
					t.Fatal(a, row)
				}
				if operation == "trash" && (a.Status != StatusTrashed || row.DeletionStatus != "trashed") {
					t.Fatal(a, row)
				}
			}
		})
	}
}
