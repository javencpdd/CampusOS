package richtext

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run inside the API container with CAMPUSOS_V11_PG_TEST=1. This creates and
// removes only a unique isolated database; DB_NAME is deliberately not used.
func TestPostgresV11AttachmentContracts(t *testing.T) {
	if os.Getenv("CAMPUSOS_V11_PG_TEST") != "1" {
		t.Skip("requires opt-in isolated PostgreSQL drill")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	host, port := os.Getenv("DB_HOST"), os.Getenv("DB_PORT")
	if host == "" || port == "" {
		t.Fatal("explicit DB_HOST/DB_PORT required")
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")), Host: net.JoinHostPort(host, port), Path: "/postgres", RawQuery: "sslmode=disable"}
	admin, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := fmt.Sprintf("campusos_v11_contract_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("isolated database cleanup failed: %v", err)
		}
	}()
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	baseline, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "000001_v1_1_schema_baseline.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(baseline)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `SET search_path=public;
INSERT INTO users(id,username,nickname,email) VALUES(1,'fixture','fixture','fixture@example.invalid');
INSERT INTO categories(id,name,slug) VALUES(1,'fixture','fixture');
INSERT INTO threads(id,title,content,author_id,category_id) SELECT id,'fixture','fixture',1,1 FROM generate_series(1,3) id;
INSERT INTO richtext_article_contents(id,thread_id,title,created_by) SELECT id,id,'fixture',1 FROM generate_series(1,3) id;
INSERT INTO storage_objects(id,owner_user_id,namespace,purpose,storage_key,original_name,mime_type,status,sha256,size_bytes)
 SELECT id,1,'richtext','article_attachment','fixture-'||id,'fixture.pdf','application/pdf','ready','hash',1 FROM generate_series(1,240) id;
INSERT INTO user_assets(id,owner_user_id,kind,original_name,storage_object_id,mime_type,size_bytes)
 SELECT id,1,'article_attachment','fixture.pdf',id,'application/pdf',1 FROM generate_series(1,240) id;`); err != nil {
		t.Fatal(err)
	}
	store := NewPgStore(pool)
	for _, scenario := range []struct {
		article string
		count   int
		bytes   int64
		same    bool
		want    int32
	}{{"1", 25, 1, false, 10}, {"2", 4, 20 * 1024 * 1024, false, 2}, {"3", 12, 1, true, 1}} {
		if _, err := pool.Exec(ctx, "UPDATE user_assets SET size_bytes=$1", scenario.bytes); err != nil {
			t.Fatal(err)
		}
		var admitted atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < scenario.count; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				asset := fmt.Sprint(i + 1)
				if scenario.same {
					asset = "1"
				}
				now := time.Now().UTC()
				err := store.CreateAttachment(ctx, &ArticleAttachment{ID: fmt.Sprint(1000 + i), ArticleContentID: scenario.article, AssetID: asset, DisplayName: "fixture.pdf", CreatedAt: now, UpdatedAt: now, Asset: UserAsset{SizeBytes: scenario.bytes}})
				if err == nil {
					admitted.Add(1)
				} else if err != ErrAttachmentLimit && err != ErrAttachmentTotalSize && err != ErrAttachmentAlreadyBound {
					t.Errorf("unexpected admission error: %v", err)
				}
			}(i)
		}
		close(start)
		wg.Wait()
		if admitted.Load() != scenario.want {
			t.Fatalf("article %s admitted %d, want %d", scenario.article, admitted.Load(), scenario.want)
		}
		// Exercise real FK behavior: an existing preview must not prevent the
		// author removing the binding. Invalidation and removal are atomic.
		items, err := store.ListAttachments(ctx, scenario.article)
		if err != nil {
			t.Fatal(err)
		}
		binding := items[0]
		now := time.Now().UTC()
		v := &PluginUIInvocation{ID: "9000", UserID: "1", PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextArticleAttachment, ArticleContentID: scenario.article, AssetID: binding.AssetID, AttachmentID: binding.ID, Presentation: "modal", Purpose: "test", ContextDigest: "pin-test", ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now}
		if err := store.CreateInvocation(ctx, v); err != nil {
			t.Fatal(err)
		}
		read, err := store.GetInvocation(ctx, v.ID)
		if err != nil || read.ContextDigest != "pin-test" {
			t.Fatalf("context digest persistence failed: %v", err)
		}
		if err := store.RemoveAttachment(ctx, scenario.article, binding.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetInvocation(ctx, v.ID); err != ErrInvocationNotFound {
			t.Fatalf("removed binding kept invocation: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM richtext_article_attachments"); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{store: store}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := svc.ListMyUserAssetPage(ctx, "1", "active", cursor, 37)
		if err != nil {
			t.Fatal(err)
		}
		for _, asset := range page.Items {
			if seen[asset.ID] {
				t.Fatal("duplicate asset")
			}
			seen[asset.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 240 {
		t.Fatalf("pagination truncated at %d", len(seen))
	}
	now := time.Now().UTC()
	for i, age := range []time.Duration{48 * time.Hour, time.Hour} {
		v := &PluginUIInvocation{ID: fmt.Sprint(9100 + i), UserID: "1", PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextPersonalAsset, AssetID: "1", Presentation: "modal", Purpose: "test", ContextDigest: "retention", CreatedAt: now.Add(-72 * time.Hour), ExpiresAt: now.Add(-age)}
		if err := store.CreateInvocation(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := store.PruneInvocations(ctx, now.Add(-24*time.Hour), 1); err != nil || count != 1 {
		t.Fatalf("postgres retention: count=%d error=%v", count, err)
	}
	if _, err := store.GetInvocation(ctx, "9100"); err != ErrInvocationNotFound {
		t.Fatalf("old context retained: %v", err)
	}
	if _, err := store.GetInvocation(ctx, "9101"); err != nil {
		t.Fatalf("recent fallback removed: %v", err)
	}
}
