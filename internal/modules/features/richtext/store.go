package richtext

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	CreateArticle(ctx context.Context, article *Article) error
	UpdateArticle(ctx context.Context, article *Article) error
	GetArticleByThreadID(ctx context.Context, threadID string) (*Article, error)
	SaveAsset(ctx context.Context, asset *Asset) error
	ListAssets(ctx context.Context, threadID string) ([]*Asset, error)
	ListAssetsByUploader(ctx context.Context, uploaderID string) ([]*Asset, error)
}

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, s.pool)
}

func (s *PgStore) CreateArticle(ctx context.Context, article *Article) error {
	contentJSON := normalizeJSON(article.ContentJSON)
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO richtext_article_contents (
			id, thread_id, title, summary, cover_url, content_html, content_json, sanitized_html,
			status, created_by, updated_by, published_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, NULLIF($11, '')::bigint, $12, $13, $14)`,
		article.ID, article.ThreadID, article.Title, article.Summary, article.CoverURL,
		article.ContentHTML, string(contentJSON), article.SanitizedHTML, article.Status,
		article.CreatedBy, article.UpdatedBy, article.PublishedAt, article.CreatedAt, article.UpdatedAt)
	return err
}

func (s *PgStore) UpdateArticle(ctx context.Context, article *Article) error {
	contentJSON := normalizeJSON(article.ContentJSON)
	tag, err := s.db(ctx).Exec(ctx, `UPDATE richtext_article_contents SET
			title=$1,
			summary=$2,
			cover_url=$3,
			content_html=$4,
			content_json=$5::jsonb,
			sanitized_html=$6,
			status=$7,
			updated_by=NULLIF($8, '')::bigint,
			published_at=$9,
			updated_at=$10
		WHERE thread_id=$11 AND deleted_at IS NULL`,
		article.Title, article.Summary, article.CoverURL, article.ContentHTML,
		string(contentJSON), article.SanitizedHTML, article.Status, article.UpdatedBy,
		article.PublishedAt, article.UpdatedAt, article.ThreadID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrArticleNotFound
	}
	return nil
}

func (s *PgStore) GetArticleByThreadID(ctx context.Context, threadID string) (*Article, error) {
	row := s.db(ctx).QueryRow(ctx, `SELECT id, thread_id, title, summary, cover_url, content_html, content_json,
			sanitized_html, status, created_by, COALESCE(updated_by::text, ''), published_at, created_at, updated_at
		FROM richtext_article_contents
		WHERE thread_id=$1 AND deleted_at IS NULL`, threadID)
	article := &Article{}
	var contentJSON []byte
	err := row.Scan(&article.ID, &article.ThreadID, &article.Title, &article.Summary, &article.CoverURL,
		&article.ContentHTML, &contentJSON, &article.SanitizedHTML, &article.Status, &article.CreatedBy,
		&article.UpdatedBy, &article.PublishedAt, &article.CreatedAt, &article.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrArticleNotFound
		}
		return nil, err
	}
	article.ContentJSON = normalizeJSON(contentJSON)
	article.RenderHTML = RenderArticleHTML(article.SanitizedHTML)
	return article, nil
}

func (s *PgStore) SaveAsset(ctx context.Context, asset *Asset) error {
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO richtext_article_assets (
			id, thread_id, article_content_id, uploader_id, file_url, file_name, file_size, mime_type, width, height, created_at
		) VALUES ($1, NULLIF($2, '')::bigint, NULLIF($3, '')::bigint, $4, $5, $6, $7, $8, $9, $10, $11)`,
		asset.ID, asset.ThreadID, asset.ArticleContentID, asset.UploaderID, asset.FileURL,
		asset.FileName, asset.FileSize, asset.MimeType, asset.Width, asset.Height, asset.CreatedAt)
	return err
}

func (s *PgStore) ListAssets(ctx context.Context, threadID string) ([]*Asset, error) {
	rows, err := s.db(ctx).Query(ctx, `SELECT id, COALESCE(thread_id::text, ''), COALESCE(article_content_id::text, ''),
			uploader_id, file_url, file_name, file_size, mime_type, width, height, created_at
		FROM richtext_article_assets
		WHERE thread_id=$1
		ORDER BY created_at DESC`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*Asset{}
	for rows.Next() {
		item := &Asset{}
		if err := rows.Scan(&item.ID, &item.ThreadID, &item.ArticleContentID, &item.UploaderID,
			&item.FileURL, &item.FileName, &item.FileSize, &item.MimeType, &item.Width, &item.Height,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListAssetsByUploader is an owner-scoped media inventory. It intentionally
// returns only persisted asset metadata and public URLs, never a storage path.
func (s *PgStore) ListAssetsByUploader(ctx context.Context, uploaderID string) ([]*Asset, error) {
	rows, err := s.db(ctx).Query(ctx, `SELECT id, COALESCE(thread_id::text, ''), COALESCE(article_content_id::text, ''),
			uploader_id, file_url, file_name, file_size, mime_type, width, height, created_at
		FROM richtext_article_assets
		WHERE uploader_id=$1::bigint
		ORDER BY created_at DESC
		LIMIT 200`, uploaderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*Asset{}
	for rows.Next() {
		item := &Asset{}
		if err := rows.Scan(&item.ID, &item.ThreadID, &item.ArticleContentID, &item.UploaderID,
			&item.FileURL, &item.FileName, &item.FileSize, &item.MimeType, &item.Width, &item.Height,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type MemoryStore struct {
	mu       sync.RWMutex
	articles map[string]*Article
	assets   map[string]*Asset
}

type memoryStoreSnapshot struct {
	Articles map[string]*Article `json:"articles"`
	Assets   map[string]*Asset   `json:"assets"`
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		articles: make(map[string]*Article),
		assets:   make(map[string]*Asset),
	}
}

// Snapshot and Restore let the local profile exercise the same rollback
// contract as PostgreSQL when a RichText command fails after a Thread write.
func (s *MemoryStore) Snapshot() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	payload, err := json.Marshal(memoryStoreSnapshot{Articles: s.articles, Assets: s.assets})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (s *MemoryStore) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	snapshot := memoryStoreSnapshot{}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return
	}
	if snapshot.Articles == nil {
		snapshot.Articles = make(map[string]*Article)
	}
	if snapshot.Assets == nil {
		snapshot.Assets = make(map[string]*Asset)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.articles = snapshot.Articles
	s.assets = snapshot.Assets
}

func (s *MemoryStore) CreateArticle(_ context.Context, article *Article) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.articles[article.ThreadID] = cloneArticle(article)
	return nil
}

func (s *MemoryStore) UpdateArticle(_ context.Context, article *Article) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.articles[article.ThreadID]; !ok {
		return ErrArticleNotFound
	}
	s.articles[article.ThreadID] = cloneArticle(article)
	return nil
}

func (s *MemoryStore) GetArticleByThreadID(_ context.Context, threadID string) (*Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	article, ok := s.articles[threadID]
	if !ok || article.Status == StatusDeleted {
		return nil, ErrArticleNotFound
	}
	return cloneArticle(article), nil
}

func (s *MemoryStore) SaveAsset(_ context.Context, asset *Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[asset.ID] = cloneAsset(asset)
	return nil
}

func (s *MemoryStore) ListAssets(_ context.Context, threadID string) ([]*Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []*Asset{}
	for _, asset := range s.assets {
		if asset.ThreadID == threadID {
			items = append(items, cloneAsset(asset))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryStore) ListAssetsByUploader(_ context.Context, uploaderID string) ([]*Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []*Asset{}
	for _, asset := range s.assets {
		if asset.UploaderID == uploaderID {
			items = append(items, cloneAsset(asset))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > 200 {
		items = items[:200]
	}
	return items, nil
}

func normalizeJSON(input json.RawMessage) json.RawMessage {
	trimmed := string(input)
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`)
	}
	return input
}

func cloneArticle(article *Article) *Article {
	if article == nil {
		return nil
	}
	clone := *article
	clone.ContentJSON = append(json.RawMessage(nil), normalizeJSON(article.ContentJSON)...)
	clone.RenderHTML = RenderArticleHTML(clone.SanitizedHTML)
	return &clone
}

func cloneAsset(asset *Asset) *Asset {
	if asset == nil {
		return nil
	}
	clone := *asset
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now().UTC()
	}
	return &clone
}
