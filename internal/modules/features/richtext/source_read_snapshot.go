package richtext

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

type PublicTextSourceSnapshot interface {
	WithPublicTextReadSnapshot(context.Context, []string, func(context.Context) error) error
}

func (r modulePublicTextSource) WithPublicTextReadSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	if r.module == nil || r.module.service == nil {
		return ErrPublicTextUnavailable
	}
	if r.module.service.ensureEnabled() != nil {
		return read(ctx)
	}
	if store, ok := r.module.service.store.(PublicTextSourceSnapshot); ok {
		return store.WithPublicTextReadSnapshot(ctx, ids, read)
	}
	return read(ctx)
}

type articleSourceReadKey struct{}
type articleSourceRead struct {
	store  *PgStore
	tx     pgx.Tx
	active atomic.Bool
	rows   map[string]*Article
}

func (s *PgStore) WithPublicTextReadSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	tx, ok := transaction.FromContext(ctx)
	if !ok || read == nil || len(ids) > 1000 {
		return errors.New("public text snapshot requires bounded checked read")
	}
	keys := []int64{}
	rowsByID := map[string]*Article{}
	for _, id := range ids {
		if !publicTextID(id) {
			return ErrPublicTextUnavailable
		}
		if _, seen := rowsByID[id]; !seen {
			n, _ := strconv.ParseInt(id, 10, 64)
			keys = append(keys, n)
			rowsByID[id] = nil
		}
	}
	rows, e := s.db(ctx).Query(ctx, `SELECT id,thread_id,title,summary,cover_url,content_html,content_json,sanitized_html,status,created_by,COALESCE(updated_by::text,''),published_at,created_at,updated_at,facts_version,updated_actor_kind,updated_actor_id FROM richtext_article_contents WHERE thread_id=ANY($1::bigint[]) AND deleted_at IS NULL ORDER BY thread_id FOR SHARE`, keys)
	if e != nil {
		return e
	}
	retainedBytes := 0
	for rows.Next() {
		a, e := scanSourceArticle(rows)
		if e != nil {
			rows.Close()
			return e
		}
		retainedBytes += len(a.Title) + len(a.ContentHTML) + len(a.ContentJSON) + len(a.SanitizedHTML) + len(a.RenderHTML) + len(a.Summary) + len(a.CoverURL)
		if retainedBytes > 4<<20 {
			rowsByID = nil
			rows.Close()
			return read(ctx)
		}
		rowsByID[a.ThreadID] = a
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	snap := &articleSourceRead{store: s, tx: tx, rows: rowsByID}
	snap.active.Store(true)
	defer snap.active.Store(false)
	return read(context.WithValue(ctx, articleSourceReadKey{}, snap))
}
func (s *PgStore) sourceReadArticle(ctx context.Context, id string) (*Article, bool) {
	snap, ok := ctx.Value(articleSourceReadKey{}).(*articleSourceRead)
	tx, active := transaction.FromContext(ctx)
	if !ok || !active || snap.store != s || snap.tx != tx || !snap.active.Load() {
		return nil, false
	}
	a, found := snap.rows[id]
	return cloneArticle(a), found
}
