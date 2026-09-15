package richtext

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	CreateArticle(ctx context.Context, article *Article) error
	UpdateArticle(ctx context.Context, article *Article) error
	GetArticleByThreadID(ctx context.Context, threadID string) (*Article, error)
	GetArticleByContentID(ctx context.Context, articleID string) (*Article, error)
	SaveAsset(ctx context.Context, asset *Asset) error
	ListAssets(ctx context.Context, threadID string) ([]*Asset, error)
	ListAssetsByUploader(ctx context.Context, uploaderID string) ([]*Asset, error)
	CreateUserAsset(ctx context.Context, asset *UserAsset) error
	GetUserAsset(ctx context.Context, assetID string) (*UserAsset, error)
	ListUserAssetsByOwner(ctx context.Context, ownerID string, statuses []string) ([]*UserAsset, error)
	TransitionUserAsset(ctx context.Context, assetID, ownerID string, from []string, status string, audit AssetLifecycleAudit) error
	DeleteUserAsset(ctx context.Context, assetID, ownerID string) error
	CreateAttachment(ctx context.Context, attachment *ArticleAttachment) error
	GetAttachment(ctx context.Context, articleID, attachmentID string) (*ArticleAttachment, error)
	ListAttachments(ctx context.Context, articleID string) ([]ArticleAttachment, error)
	UpdateAttachmentDisplayName(ctx context.Context, articleID, attachmentID, displayName string) error
	RemoveAttachment(ctx context.Context, articleID, attachmentID string) error
	ReorderAttachments(ctx context.Context, articleID string, attachmentIDs []string) error
	AssetReferenceCount(ctx context.Context, assetID string) (int, error)
	AssetGovernanceSummary(ctx context.Context, now time.Time) (AssetGovernanceSummary, error)
	CreateInvocation(ctx context.Context, invocation *PluginUIInvocation) error
	GetInvocation(ctx context.Context, invocationID string) (*PluginUIInvocation, error)
	MarkInvocationOpened(ctx context.Context, invocationID, userID string) error
}

type PgStore struct {
	pool *pgxpool.Pool
}

// createAttachmentSQL keeps all string-form IDs explicitly typed at the SQL
// boundary. IDs are strings in the domain model so that the API does not lose
// precision in JavaScript, while PostgreSQL stores them as bigint.
//
// In particular, $2 is also used in hashtextextended. Without the casts in
// the INSERT projection PostgreSQL infers it as text there and rejects the
// insert into article_content_id (bigint) at runtime.
const attachmentAdmissionLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('richtext-attachment:' || $1::text, 0))`

const createAttachmentSQL = `WITH budget AS (
			SELECT count(*)::int AS attachment_count,
				COALESCE(sum(u.size_bytes), 0)::bigint AS total_bytes,
				COALESCE(max(a.display_order), -1)::int AS max_display_order,
				bool_or(a.asset_id=$3::bigint) AS already_bound
			FROM richtext_article_attachments a
			JOIN user_assets u ON u.id=a.asset_id
			WHERE a.article_content_id=$2::bigint
		)
		INSERT INTO richtext_article_attachments
			(id, article_content_id, asset_id, display_name, display_order, created_at, updated_at)
		SELECT $1::bigint, $2::bigint, $3::bigint, $4, budget.max_display_order+1, $5, $6
		FROM budget
		WHERE budget.attachment_count < $7
			AND budget.total_bytes + $8 <= $9
			AND NOT COALESCE(budget.already_bound, false)
		RETURNING display_order`

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
	return s.getArticle(ctx, `thread_id=$1 AND deleted_at IS NULL`, threadID)
}

func (s *PgStore) GetArticleByContentID(ctx context.Context, articleID string) (*Article, error) {
	return s.getArticle(ctx, `id=$1 AND deleted_at IS NULL`, articleID)
}

func (s *PgStore) getArticle(ctx context.Context, where string, value string) (*Article, error) {
	row := s.db(ctx).QueryRow(ctx, `SELECT id, thread_id, title, summary, cover_url, content_html, content_json,
			sanitized_html, status, created_by, COALESCE(updated_by::text, ''), published_at, created_at, updated_at
		FROM richtext_article_contents
		WHERE `+where, value)
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

func (s *PgStore) CreateUserAsset(ctx context.Context, asset *UserAsset) error {
	if asset == nil {
		return ErrAssetInvalid
	}
	_, err := s.db(ctx).Exec(ctx, `INSERT INTO user_assets
		(id, owner_user_id, kind, original_name, storage_object_id, mime_type, size_bytes, status, version, created_at, updated_at, trashed_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		asset.ID, asset.OwnerID, asset.Kind, asset.OriginalName, asset.StorageObjectID, asset.MimeType, asset.SizeBytes,
		asset.Status, asset.Version, asset.CreatedAt, asset.UpdatedAt, asset.TrashedAt, asset.DeletedAt)
	return err
}

func scanUserAsset(row pgx.Row) (*UserAsset, error) {
	item := &UserAsset{}
	err := row.Scan(&item.ID, &item.OwnerID, &item.Kind, &item.OriginalName, &item.StorageObjectID, &item.MimeType,
		&item.SizeBytes, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.TrashedAt, &item.DeletedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	return item, err
}

const userAssetColumns = `id::text, owner_user_id::text, kind, original_name, storage_object_id::text, mime_type,
	size_bytes, status, version, created_at, updated_at, trashed_at, deleted_at`

func userAssetColumnsFor(alias string) string {
	return alias + `.id::text, ` + alias + `.owner_user_id::text, ` + alias + `.kind, ` + alias + `.original_name, ` +
		alias + `.storage_object_id::text, ` + alias + `.mime_type, ` + alias + `.size_bytes, ` + alias + `.status, ` +
		alias + `.version, ` + alias + `.created_at, ` + alias + `.updated_at, ` + alias + `.trashed_at, ` + alias + `.deleted_at`
}

func (s *PgStore) GetUserAsset(ctx context.Context, assetID string) (*UserAsset, error) {
	return scanUserAsset(s.db(ctx).QueryRow(ctx, `SELECT `+userAssetColumns+` FROM user_assets WHERE id=$1`, assetID))
}

func (s *PgStore) ListUserAssetsByOwner(ctx context.Context, ownerID string, statuses []string) ([]*UserAsset, error) {
	if len(statuses) == 0 {
		return []*UserAsset{}, nil
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT `+userAssetColumns+` FROM user_assets
		WHERE owner_user_id=$1 AND status = ANY($2) ORDER BY updated_at DESC, id DESC LIMIT 200`, ownerID, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*UserAsset, 0)
	for rows.Next() {
		item, scanErr := scanUserAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PgStore) TransitionUserAsset(ctx context.Context, assetID, ownerID string, from []string, status string, audit AssetLifecycleAudit) error {
	if len(from) == 0 || strings.TrimSpace(assetID) == "" || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(status) == "" {
		return ErrAssetInvalid
	}
	if audit.ID == "" {
		audit.ID = fmt.Sprintf("%d", idgen.New())
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now().UTC()
	}
	if audit.ActorType == "" {
		audit.ActorType = "system"
	}
	tag, err := s.db(ctx).Exec(ctx, `WITH updated AS (
		UPDATE user_assets SET
			status=$1,
			version=version+1,
			updated_at=$2,
			trashed_at=CASE WHEN $1='trashed' THEN $2 WHEN $1='active' THEN NULL ELSE trashed_at END,
			deleted_at=CASE WHEN $1='deleted' THEN $2 ELSE deleted_at END
		WHERE id=$3 AND owner_user_id=$4 AND status = ANY($5)
		RETURNING id
	)
	INSERT INTO asset_lifecycle_audits (id, asset_id, actor_user_id, actor_type, action, reason, created_at)
	SELECT $6, id, NULLIF($7, '')::bigint, $8, $9, NULLIF($10, ''), $2 FROM updated`,
		status, audit.CreatedAt, assetID, ownerID, from, audit.ID, audit.ActorID, audit.ActorType, audit.Action, audit.Reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAssetNotFound
	}
	return nil
}

func (s *PgStore) DeleteUserAsset(ctx context.Context, assetID, ownerID string) error {
	tag, err := s.db(ctx).Exec(ctx, `DELETE FROM user_assets WHERE id=$1 AND owner_user_id=$2`, assetID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAssetNotFound
	}
	return nil
}

func (s *PgStore) CreateAttachment(ctx context.Context, attachment *ArticleAttachment) error {
	if attachment == nil {
		return ErrAttachmentNotFound
	}
	return transaction.NewPostgreSQL(s.pool).Within(ctx, func(ctx context.Context) error {
		// Acquire the lock in a separate statement. Under READ COMMITTED the
		// following INSERT then sees commits made by the previous lock holder;
		// putting the lock in an INSERT CTE would retain a pre-wait snapshot.
		if _, err := s.db(ctx).Exec(ctx, attachmentAdmissionLockSQL, attachment.ArticleContentID); err != nil {
			return err
		}
		return s.createAttachmentLocked(ctx, attachment)
	})
}

func (s *PgStore) createAttachmentLocked(ctx context.Context, attachment *ArticleAttachment) error {
	err := s.db(ctx).QueryRow(ctx, createAttachmentSQL, attachment.ID, attachment.ArticleContentID, attachment.AssetID, attachment.DisplayName,
		attachment.CreatedAt, attachment.UpdatedAt, MaxArticleAttachmentCount, attachment.Asset.SizeBytes, MaxArticleAttachmentTotalBytes).
		Scan(&attachment.DisplayOrder)
	if err == nil {
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	items, listErr := s.ListAttachments(ctx, attachment.ArticleContentID)
	if listErr != nil {
		return listErr
	}
	var total int64
	for _, item := range items {
		if item.AssetID == attachment.AssetID {
			return ErrAttachmentAlreadyBound
		}
		total += item.Asset.SizeBytes
	}
	if len(items) >= MaxArticleAttachmentCount {
		return ErrAttachmentLimit
	}
	if total+attachment.Asset.SizeBytes > MaxArticleAttachmentTotalBytes {
		return ErrAttachmentTotalSize
	}
	return ErrAttachmentNotFound
}

func scanAttachment(row pgx.Row) (*ArticleAttachment, error) {
	item := &ArticleAttachment{}
	err := row.Scan(&item.ID, &item.ArticleContentID, &item.AssetID, &item.DisplayName, &item.DisplayOrder,
		&item.CreatedAt, &item.UpdatedAt, &item.Asset.ID, &item.Asset.OwnerID, &item.Asset.Kind, &item.Asset.OriginalName,
		&item.Asset.StorageObjectID, &item.Asset.MimeType, &item.Asset.SizeBytes, &item.Asset.Status, &item.Asset.Version,
		&item.Asset.CreatedAt, &item.Asset.UpdatedAt, &item.Asset.TrashedAt, &item.Asset.DeletedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrAttachmentNotFound
	}
	return item, err
}

const attachmentColumns = `a.id::text, a.article_content_id::text, a.asset_id::text, a.display_name, a.display_order,
	a.created_at, a.updated_at, `

func attachmentQuery(where string) string {
	return `SELECT ` + attachmentColumns + userAssetColumnsFor("u") + ` FROM richtext_article_attachments a
		JOIN user_assets u ON u.id=a.asset_id ` + where
}

func (s *PgStore) GetAttachment(ctx context.Context, articleID, attachmentID string) (*ArticleAttachment, error) {
	return scanAttachment(s.db(ctx).QueryRow(ctx, attachmentQuery(`WHERE a.article_content_id=$1 AND a.id=$2`), articleID, attachmentID))
}

func (s *PgStore) ListAttachments(ctx context.Context, articleID string) ([]ArticleAttachment, error) {
	rows, err := s.db(ctx).Query(ctx, attachmentQuery(`WHERE a.article_content_id=$1 ORDER BY a.display_order, a.id`), articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ArticleAttachment, 0)
	for rows.Next() {
		item, scanErr := scanAttachment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *PgStore) UpdateAttachmentDisplayName(ctx context.Context, articleID, attachmentID, displayName string) error {
	tag, err := s.db(ctx).Exec(ctx, `UPDATE richtext_article_attachments
		SET display_name=$1, updated_at=NOW()
		WHERE article_content_id=$2 AND id=$3`, displayName, articleID, attachmentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

func (s *PgStore) RemoveAttachment(ctx context.Context, articleID, attachmentID string) error {
	tag, err := s.db(ctx).Exec(ctx, `DELETE FROM richtext_article_attachments WHERE article_content_id=$1 AND id=$2`, articleID, attachmentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

func (s *PgStore) ReorderAttachments(ctx context.Context, articleID string, attachmentIDs []string) error {
	if len(attachmentIDs) == 0 {
		return ErrAttachmentNotFound
	}
	// First move the selected rows away from the unique order range, then
	// write final stable positions.  Both phases share the command transaction.
	for index, id := range attachmentIDs {
		tag, err := s.db(ctx).Exec(ctx, `UPDATE richtext_article_attachments SET display_order=$1, updated_at=NOW()
			WHERE article_content_id=$2 AND id=$3`, 1000000+index, articleID, id)
		if err != nil || tag.RowsAffected() == 0 {
			if err != nil {
				return err
			}
			return ErrAttachmentNotFound
		}
	}
	for index, id := range attachmentIDs {
		if _, err := s.db(ctx).Exec(ctx, `UPDATE richtext_article_attachments SET display_order=$1, updated_at=NOW()
			WHERE article_content_id=$2 AND id=$3`, index, articleID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *PgStore) AssetReferenceCount(ctx context.Context, assetID string) (int, error) {
	var count int
	err := s.db(ctx).QueryRow(ctx, `SELECT
		(SELECT count(*) FROM richtext_article_attachments WHERE asset_id=$1) +
		(SELECT count(*) FROM richtext_article_assets WHERE asset_id=$1)`, assetID).Scan(&count)
	return count, err
}

func (s *PgStore) AssetGovernanceSummary(ctx context.Context, now time.Time) (AssetGovernanceSummary, error) {
	summary := AssetGovernanceSummary{GeneratedAt: now.UTC()}
	rows, err := s.db(ctx).Query(ctx, `SELECT status, count(*)::bigint, COALESCE(sum(size_bytes), 0)::bigint
		FROM user_assets GROUP BY status ORDER BY status`)
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AssetStatusSummary
		if err := rows.Scan(&item.Status, &item.Count, &item.SizeBytes); err != nil {
			return AssetGovernanceSummary{}, err
		}
		summary.Statuses = append(summary.Statuses, item)
	}
	if err := rows.Err(); err != nil {
		return AssetGovernanceSummary{}, err
	}
	rows, err = s.db(ctx).Query(ctx, `SELECT action, count(*)::bigint FROM asset_lifecycle_audits GROUP BY action ORDER BY action`)
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AssetActionSummary
		if err := rows.Scan(&item.Action, &item.Count); err != nil {
			return AssetGovernanceSummary{}, err
		}
		summary.Actions = append(summary.Actions, item)
	}
	if err := rows.Err(); err != nil {
		return AssetGovernanceSummary{}, err
	}
	if err := s.db(ctx).QueryRow(ctx, `SELECT count(*) FROM plugin_ui_invocations WHERE expires_at <= $1 OR revoked_at IS NOT NULL`, summary.GeneratedAt).Scan(&summary.ExpiredInvocations); err != nil {
		return AssetGovernanceSummary{}, err
	}
	return summary, nil
}

const createInvocationSQL = `INSERT INTO plugin_ui_invocations
	(id, user_id, plugin_key, surface_id, context_kind, article_content_id, asset_id, attachment_id, personal_document_id, presentation, purpose, context_digest, expires_at, opened_at, revoked_at, created_at)
	VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::bigint,NULLIF($7,'')::bigint,NULLIF($8,'')::bigint,NULLIF($9,'')::bigint,$10,$11,'',$12,$13,$14,$15)`

const getInvocationSQL = `SELECT id::text,user_id::text,plugin_key,surface_id,context_kind,COALESCE(article_content_id::text,''),COALESCE(asset_id::text,''),COALESCE(attachment_id::text,''),
	COALESCE(personal_document_id::text,''),presentation,purpose,expires_at,opened_at,revoked_at,created_at FROM plugin_ui_invocations WHERE id=$1`

func (s *PgStore) CreateInvocation(ctx context.Context, item *PluginUIInvocation) error {
	if item == nil {
		return ErrInvocationNotFound
	}
	_, err := s.db(ctx).Exec(ctx, createInvocationSQL,
		item.ID, item.UserID, item.PluginKey, item.SurfaceID, item.ContextKind, item.ArticleContentID, item.AssetID, item.AttachmentID, item.PersonalDocumentID, item.Presentation, item.Purpose,
		item.ExpiresAt, item.OpenedAt, item.RevokedAt, item.CreatedAt)
	return err
}

func (s *PgStore) GetInvocation(ctx context.Context, invocationID string) (*PluginUIInvocation, error) {
	item := &PluginUIInvocation{}
	err := s.db(ctx).QueryRow(ctx, getInvocationSQL, invocationID).
		Scan(&item.ID, &item.UserID, &item.PluginKey, &item.SurfaceID, &item.ContextKind, &item.ArticleContentID, &item.AssetID, &item.AttachmentID, &item.PersonalDocumentID, &item.Presentation, &item.Purpose, &item.ExpiresAt, &item.OpenedAt, &item.RevokedAt, &item.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrInvocationNotFound
	}
	return item, err
}

func (s *PgStore) MarkInvocationOpened(ctx context.Context, invocationID, userID string) error {
	tag, err := s.db(ctx).Exec(ctx, `UPDATE plugin_ui_invocations SET opened_at=COALESCE(opened_at,NOW())
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at > NOW()`, invocationID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvocationExpired
	}
	return nil
}

type MemoryStore struct {
	mu          sync.RWMutex
	articles    map[string]*Article
	assets      map[string]*Asset
	userAssets  map[string]*UserAsset
	attachments map[string]*ArticleAttachment
	invocations map[string]*PluginUIInvocation
	audits      []AssetLifecycleAudit
}

type memoryStoreSnapshot struct {
	Articles    map[string]*Article            `json:"articles"`
	Assets      map[string]*Asset              `json:"assets"`
	UserAssets  map[string]*UserAsset          `json:"user_assets"`
	Attachments map[string]*ArticleAttachment  `json:"attachments"`
	Invocations map[string]*PluginUIInvocation `json:"invocations"`
	Audits      []AssetLifecycleAudit          `json:"audits"`
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		articles:    make(map[string]*Article),
		assets:      make(map[string]*Asset),
		userAssets:  make(map[string]*UserAsset),
		attachments: make(map[string]*ArticleAttachment),
		invocations: make(map[string]*PluginUIInvocation),
	}
}

// Snapshot and Restore let the local profile exercise the same rollback
// contract as PostgreSQL when a RichText command fails after a Thread write.
func (s *MemoryStore) Snapshot() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	payload, err := json.Marshal(memoryStoreSnapshot{Articles: s.articles, Assets: s.assets, UserAssets: s.userAssets, Attachments: s.attachments, Invocations: s.invocations, Audits: s.audits})
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
	if snapshot.UserAssets == nil {
		snapshot.UserAssets = make(map[string]*UserAsset)
	}
	if snapshot.Attachments == nil {
		snapshot.Attachments = make(map[string]*ArticleAttachment)
	}
	if snapshot.Invocations == nil {
		snapshot.Invocations = make(map[string]*PluginUIInvocation)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.articles = snapshot.Articles
	s.assets = snapshot.Assets
	s.userAssets = snapshot.UserAssets
	s.attachments = snapshot.Attachments
	s.invocations = snapshot.Invocations
	s.audits = append([]AssetLifecycleAudit(nil), snapshot.Audits...)
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

func (s *MemoryStore) GetArticleByContentID(_ context.Context, articleID string) (*Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, article := range s.articles {
		if article.ID == articleID && article.Status != StatusDeleted {
			return cloneArticle(article), nil
		}
	}
	return nil, ErrArticleNotFound
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

func (s *MemoryStore) CreateUserAsset(_ context.Context, asset *UserAsset) error {
	if asset == nil || asset.ID == "" || asset.OwnerID == "" || asset.StorageObjectID == "" {
		return ErrAssetInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.userAssets[asset.ID]; exists {
		return ErrAssetInvalid
	}
	for _, existing := range s.userAssets {
		if existing.StorageObjectID == asset.StorageObjectID {
			return ErrAssetInvalid
		}
	}
	s.userAssets[asset.ID] = cloneUserAsset(asset)
	return nil
}

func (s *MemoryStore) GetUserAsset(_ context.Context, assetID string) (*UserAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.userAssets[assetID]
	if !ok {
		return nil, ErrAssetNotFound
	}
	return cloneUserAsset(item), nil
}

func (s *MemoryStore) ListUserAssetsByOwner(_ context.Context, ownerID string, statuses []string) ([]*UserAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*UserAsset, 0)
	for _, item := range s.userAssets {
		if item.OwnerID == ownerID && containsAssetStatus(statuses, item.Status) {
			items = append(items, cloneUserAsset(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > 200 {
		items = items[:200]
	}
	return items, nil
}

func (s *MemoryStore) TransitionUserAsset(_ context.Context, assetID, ownerID string, from []string, status string, audit AssetLifecycleAudit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.userAssets[assetID]
	if !ok || item.OwnerID != ownerID || !containsAssetStatus(from, item.Status) {
		return ErrAssetNotFound
	}
	now := time.Now().UTC()
	item.Status, item.Version, item.UpdatedAt = status, item.Version+1, now
	if status == AssetStatusTrashed {
		item.TrashedAt = &now
	}
	if status == AssetStatusActive {
		item.TrashedAt = nil
	}
	if status == AssetStatusDeleted {
		item.DeletedAt = &now
	}
	if audit.ID == "" {
		audit.ID = fmt.Sprintf("%d", idgen.New())
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = now
	}
	if audit.ActorType == "" {
		audit.ActorType = "system"
	}
	audit.AssetID = assetID
	s.audits = append(s.audits, audit)
	return nil
}

func (s *MemoryStore) DeleteUserAsset(_ context.Context, assetID, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.userAssets[assetID]
	if !ok || item.OwnerID != ownerID {
		return ErrAssetNotFound
	}
	for _, attachment := range s.attachments {
		if attachment.AssetID == assetID {
			return ErrAssetReferenced
		}
	}
	delete(s.userAssets, assetID)
	return nil
}

func (s *MemoryStore) CreateAttachment(_ context.Context, attachment *ArticleAttachment) error {
	if attachment == nil || attachment.ID == "" {
		return ErrAttachmentNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.attachments[attachment.ID]; ok {
		return ErrAttachmentNotFound
	}
	foundArticle := false
	for _, article := range s.articles {
		if article.ID == attachment.ArticleContentID {
			foundArticle = true
			break
		}
	}
	if !foundArticle {
		return ErrArticleNotFound
	}
	asset, ok := s.userAssets[attachment.AssetID]
	if !ok {
		return ErrAssetNotFound
	}
	count := 0
	var total int64
	nextOrder := 0
	for _, existing := range s.attachments {
		if existing.ArticleContentID != attachment.ArticleContentID {
			continue
		}
		if existing.AssetID == attachment.AssetID {
			return ErrAttachmentAlreadyBound
		}
		count++
		if existingAsset, exists := s.userAssets[existing.AssetID]; exists {
			total += existingAsset.SizeBytes
		}
		if existing.DisplayOrder >= nextOrder {
			nextOrder = existing.DisplayOrder + 1
		}
	}
	if count >= MaxArticleAttachmentCount {
		return ErrAttachmentLimit
	}
	if total+asset.SizeBytes > MaxArticleAttachmentTotalBytes {
		return ErrAttachmentTotalSize
	}
	attachment.DisplayOrder = nextOrder
	s.attachments[attachment.ID] = cloneAttachment(attachment)
	return nil
}

func (s *MemoryStore) attachmentLocked(articleID, attachmentID string) (*ArticleAttachment, bool) {
	item, ok := s.attachments[attachmentID]
	return item, ok && item.ArticleContentID == articleID
}

func (s *MemoryStore) GetAttachment(_ context.Context, articleID, attachmentID string) (*ArticleAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.attachmentLocked(articleID, attachmentID)
	if !ok {
		return nil, ErrAttachmentNotFound
	}
	asset, ok := s.userAssets[item.AssetID]
	if !ok {
		return nil, ErrAssetNotFound
	}
	clone := cloneAttachment(item)
	clone.Asset = *cloneUserAsset(asset)
	return clone, nil
}

func (s *MemoryStore) ListAttachments(_ context.Context, articleID string) ([]ArticleAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]ArticleAttachment, 0)
	for _, item := range s.attachments {
		if item.ArticleContentID != articleID {
			continue
		}
		asset, ok := s.userAssets[item.AssetID]
		if !ok {
			continue
		}
		clone := cloneAttachment(item)
		clone.Asset = *cloneUserAsset(asset)
		items = append(items, *clone)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DisplayOrder < items[j].DisplayOrder })
	return items, nil
}

func (s *MemoryStore) UpdateAttachmentDisplayName(_ context.Context, articleID, attachmentID, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.attachmentLocked(articleID, attachmentID)
	if !ok {
		return ErrAttachmentNotFound
	}
	item.DisplayName = displayName
	item.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryStore) RemoveAttachment(_ context.Context, articleID, attachmentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.attachmentLocked(articleID, attachmentID); !ok {
		return ErrAttachmentNotFound
	}
	delete(s.attachments, attachmentID)
	return nil
}

func (s *MemoryStore) ReorderAttachments(_ context.Context, articleID string, attachmentIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(attachmentIDs) == 0 {
		return ErrAttachmentNotFound
	}
	if len(attachmentIDs) > 10 {
		return ErrAttachmentLimit
	}
	for order, id := range attachmentIDs {
		item, ok := s.attachmentLocked(articleID, id)
		if !ok {
			return ErrAttachmentNotFound
		}
		item.DisplayOrder, item.UpdatedAt = order, time.Now().UTC()
	}
	return nil
}

func (s *MemoryStore) AssetReferenceCount(_ context.Context, assetID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, attachment := range s.attachments {
		if attachment.AssetID == assetID {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) AssetGovernanceSummary(_ context.Context, now time.Time) (AssetGovernanceSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	statusCounts := map[string]AssetStatusSummary{}
	for _, item := range s.userAssets {
		current := statusCounts[item.Status]
		current.Status = item.Status
		current.Count++
		current.SizeBytes += item.SizeBytes
		statusCounts[item.Status] = current
	}
	actionCounts := map[string]AssetActionSummary{}
	for _, item := range s.audits {
		current := actionCounts[item.Action]
		current.Action = item.Action
		current.Count++
		actionCounts[item.Action] = current
	}
	summary := AssetGovernanceSummary{GeneratedAt: now.UTC()}
	for _, item := range statusCounts {
		summary.Statuses = append(summary.Statuses, item)
	}
	for _, item := range actionCounts {
		summary.Actions = append(summary.Actions, item)
	}
	for _, item := range s.invocations {
		if !item.ExpiresAt.After(summary.GeneratedAt) || item.RevokedAt != nil {
			summary.ExpiredInvocations++
		}
	}
	sort.Slice(summary.Statuses, func(i, j int) bool { return summary.Statuses[i].Status < summary.Statuses[j].Status })
	sort.Slice(summary.Actions, func(i, j int) bool { return summary.Actions[i].Action < summary.Actions[j].Action })
	return summary, nil
}

func containsAssetStatus(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s *MemoryStore) CreateInvocation(_ context.Context, item *PluginUIInvocation) error {
	if item == nil || item.ID == "" {
		return ErrInvocationNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.invocations[item.ID]; ok {
		return ErrInvocationNotFound
	}
	s.invocations[item.ID] = cloneInvocation(item)
	return nil
}

func (s *MemoryStore) GetInvocation(_ context.Context, invocationID string) (*PluginUIInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.invocations[invocationID]
	if !ok {
		return nil, ErrInvocationNotFound
	}
	return cloneInvocation(item), nil
}

func (s *MemoryStore) MarkInvocationOpened(_ context.Context, invocationID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.invocations[invocationID]
	if !ok || item.UserID != userID || item.RevokedAt != nil || !item.ExpiresAt.After(time.Now().UTC()) {
		return ErrInvocationExpired
	}
	if item.OpenedAt == nil {
		now := time.Now().UTC()
		item.OpenedAt = &now
	}
	return nil
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
	clone.Attachments = append([]ArticleAttachment(nil), article.Attachments...)
	clone.RenderHTML = RenderArticleHTML(clone.SanitizedHTML)
	return &clone
}

func cloneUserAsset(asset *UserAsset) *UserAsset {
	if asset == nil {
		return nil
	}
	clone := *asset
	if asset.TrashedAt != nil {
		value := *asset.TrashedAt
		clone.TrashedAt = &value
	}
	if asset.DeletedAt != nil {
		value := *asset.DeletedAt
		clone.DeletedAt = &value
	}
	return &clone
}

func cloneAttachment(item *ArticleAttachment) *ArticleAttachment {
	if item == nil {
		return nil
	}
	clone := *item
	clone.Asset = *cloneUserAsset(&item.Asset)
	return &clone
}

func cloneInvocation(item *PluginUIInvocation) *PluginUIInvocation {
	if item == nil {
		return nil
	}
	clone := *item
	if item.OpenedAt != nil {
		value := *item.OpenedAt
		clone.OpenedAt = &value
	}
	if item.RevokedAt != nil {
		value := *item.RevokedAt
		clone.RevokedAt = &value
	}
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
