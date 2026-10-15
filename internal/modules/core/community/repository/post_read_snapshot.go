package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/jackc/pgx/v5"
)

// The caller must authorize the actual parent before and after this snapshot.
// PostgreSQL additionally binds that parent and its predicate in the same SQL
// statement that selects both total and page, including an empty page.
type PostReadSnapshotRepository interface {
	ReadPublishedPostSnapshot(context.Context, string, int64, string, string, int, int) ([]*domain.Post, int64, error)
}

func postReadOffset(page, size int) (int64, error) {
	if page < 1 || size < 1 || size > 100 || int64(page-1) > math.MaxInt64/int64(size) {
		return 0, ErrThreadPaginationOverflow
	}
	return int64(page-1) * int64(size), nil
}
func (r *MemoryPostRepository) ReadPublishedPostSnapshot(ctx context.Context, threadID string, version int64, viewer, postID string, page, size int) ([]*domain.Post, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	offset, err := postReadOffset(page, size)
	if err != nil || version < 1 {
		return nil, 0, ErrPostFactsUnavailable
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	posts := []*domain.Post{}
	for _, p := range r.posts {
		if p.ThreadID == threadID && p.Status == "published" && p.DeletedAt == nil && (postID == "" || p.ID == postID) {
			posts = append(posts, clonePost(p))
		}
	}
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].FloorNumber != posts[j].FloorNumber {
			return posts[i].FloorNumber < posts[j].FloorNumber
		}
		if !posts[i].CreatedAt.Equal(posts[j].CreatedAt) {
			return posts[i].CreatedAt.Before(posts[j].CreatedAt)
		}
		return posts[i].ID < posts[j].ID
	})
	total := int64(len(posts))
	if offset >= total {
		return []*domain.Post{}, total, nil
	}
	return posts[offset:min(offset+int64(size), total)], total, nil
}
func (r *PgPostRepository) ReadPublishedPostSnapshot(ctx context.Context, threadID string, version int64, viewer, postID string, page, size int) ([]*domain.Post, int64, error) {
	offset, err := postReadOffset(page, size)
	if err != nil {
		return nil, 0, err
	}
	if version < 1 {
		return nil, 0, ErrPostFactsUnavailable
	}
	if err := r.checkPostFactsGuard(ctx); err != nil {
		return nil, 0, err
	}
	var managedParent bool
	if err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.threads'::regclass AND tgname='trg_threads_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_thread_facts_version()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.thread_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)`).Scan(&managedParent); err != nil || !managedParent {
		return nil, 0, ErrPostFactsUnavailable
	}
	var raw []byte
	var total int64
	err = r.db(ctx).QueryRow(ctx, `WITH parent AS MATERIALIZED (
	 SELECT id FROM threads WHERE id::text=$1 AND facts_version=$2 AND deletion_status='active'
	 AND publication_status IN ('draft','published','private') AND moderation_status IN ('clear','pending','rejected','taken_down')
	 AND ((publication_status='published' AND moderation_status='clear') OR ($3<>'' AND author_id::text=$3))
	), eligible AS MATERIALIZED (
	 SELECT p.* FROM posts p JOIN parent t ON t.id=p.thread_id WHERE p.deleted_at IS NULL AND p.status='published' AND ($4='' OR p.id::text=$4)
	), paged AS (
	 SELECT * FROM eligible ORDER BY floor_number,created_at,id LIMIT $5 OFFSET $6
	)
	SELECT (SELECT count(*) FROM eligible), COALESCE((SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('id',p.id::text,'thread_id',p.thread_id::text,'author_id',p.author_id::text,'parent_id',p.parent_id::text) ORDER BY floor_number,created_at,id) FROM paged p),'[]'::jsonb)
	FROM parent`, threadID, version, viewer, postID, size, offset).Scan(&total, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrThreadNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var records []struct {
		domain.Post
		Version     int64           `json:"facts_version"`
		RawMetadata json.RawMessage `json:"metadata"`
		ActorKind   string          `json:"deleted_actor_kind"`
		ActorID     string          `json:"deleted_actor_id"`
	}
	if json.Unmarshal(raw, &records) != nil {
		return nil, 0, ErrPostFactsUnavailable
	}
	posts := make([]*domain.Post, 0, len(records))
	for _, row := range records {
		p := row.Post
		p.FactsVersion, p.Metadata, p.DeletedActorKind, p.DeletedActorID = row.Version, row.RawMetadata, row.ActorKind, row.ActorID
		posts = append(posts, &p)
	}
	return posts, total, nil
}
