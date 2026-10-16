package richtext

import (
	"context"
	"fmt"
	"os"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
)

func TestInlineImagePostgreSQLStateMatrix(t *testing.T) {
	ctx := context.Background()
	owner := "109201"
	s, repo, _ := richReadPostgres(t, owner)
	a, core, draft := inlineImageFixture(t, s, owner, "109111")
	forum := &domain.Thread{ID: fmt.Sprint(idgen.New()), AuthorID: owner, AuthorName: "Image owner", CategoryID: "109111", ThreadType: domain.ThreadTypeDiscussion, Title: "Image source", ContentFormat: "safe_html", Content: `<p>body</p><img src="` + core.FileURL + `">`, PublicationStatus: "published", ModerationStatus: "clear", DeletionStatus: "active"}
	if err := repo.Create(ctx, forum); err != nil {
		t.Fatal(err)
	}
	query := community.NewContentQuery(communitysvc.NewThreadService(repo, nil)).(communityport.AuthenticatedThreadQuery)
	for _, publication := range []domain.PublicationStatus{"draft", "private", "published"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(string(publication)+"/"+string(moderation)+"/"+string(deletion), func(t *testing.T) {
					for _, id := range []string{draft.ThreadID, forum.ID} {
						thread, _ := repo.GetByID(ctx, id)
						thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
						if err := repo.Update(ctx, thread); err != nil {
							t.Fatal(err)
						}
					}
					allow := publication == "published" && moderation == "clear" && deletion == "active"
					for _, image := range []struct{ name, kind string }{{a.FileName, "richtext"}, {core.FileName, "content"}} {
						img, err := s.ReadInlineImageForPrincipal(ctx, owner, image.name, draft.ThreadID, image.kind, nil)
						if (err == nil) != allow || (err != nil && len(img.Data) != 0) {
							t.Fatalf("rich public %s %v", image.kind, err)
						}
						img, err = s.ReadInlineImageForPrincipal(ctx, owner, image.name, draft.ThreadID, image.kind, richReadProof(owner))
						if (err == nil) != (deletion == "active") || (err != nil && len(img.Data) != 0) {
							t.Fatalf("rich owner %s %v", image.kind, err)
						}
					}
					img, err := s.contentImages.ReadForPrincipal(ctx, owner, core.FileName, forum.ID, query, nil)
					if (err == nil) != allow || (err != nil && len(img.Data) != 0) {
						t.Fatalf("core public %v", err)
					}
					img, err = s.contentImages.ReadForPrincipal(ctx, owner, core.FileName, forum.ID, query, richReadProof(owner))
					if (err == nil) != (deletion == "active") || (err != nil && len(img.Data) != 0) {
						t.Fatalf("core owner %v", err)
					}
				})
			}
		}
	}
}

func TestInlineImagePostgreSQLCommittedRaces(t *testing.T) {
	for i, mode := range []string{"body", "cover", "publication", "trash", "owner", "metadata", "body-ABA", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(109210 + i)
			s, _, pool := richReadPostgres(t, owner)
			a, _, draft := inlineImageFixture(t, s, owner, "109111")
			if _, err := s.Publish(ctx, draft.ThreadID, owner); err != nil {
				t.Fatal(err)
			}
			wrapper := &inlineHookStore{Store: s.store}
			wrapper.after = func(n int) {
				if (mode == "bytes" && n != 2) || (mode != "bytes" && n != 1) {
					return
				}
				if mode == "bytes" {
					path, _ := s.assets.Path(owner, a.FileName)
					data := append([]byte{}, tinyPNG...)
					data[len(data)-1] ^= 1
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					return
				}
				richReadCommit(t, pool, func(tx pgx.Tx) error {
					var err error
					switch mode {
					case "body", "cover":
						_, err = tx.Exec(ctx, `UPDATE richtext_article_contents SET sanitized_html='<p>removed</p>', cover_url='' WHERE thread_id=$1`, draft.ThreadID)
					case "publication":
						_, err = tx.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=$1`, draft.ThreadID)
					case "trash":
						_, err = tx.Exec(ctx, `UPDATE threads SET deletion_status='trashed' WHERE id=$1`, draft.ThreadID)
					case "owner":
						_, err = tx.Exec(ctx, `UPDATE threads SET author_id=109201 WHERE id=$1`, draft.ThreadID)
					case "metadata":
						_, err = tx.Exec(ctx, `UPDATE richtext_article_assets SET file_size=file_size+1 WHERE id=$1`, a.ID)
					case "body-ABA":
						_, err = tx.Exec(ctx, `UPDATE threads SET content=content||'changed' WHERE id=$1`, draft.ThreadID)
						if err == nil {
							_, err = tx.Exec(ctx, `UPDATE threads SET content=left(content,length(content)-7) WHERE id=$1`, draft.ThreadID)
						}
					}
					return err
				})
			}
			s.store = wrapper
			img, err := s.ReadInlineImageForPrincipal(ctx, owner, a.FileName, draft.ThreadID, "richtext", nil)
			if err == nil || len(img.Data) != 0 {
				t.Fatalf("stale bytes=%d err=%v", len(img.Data), err)
			}
		})
	}
}
