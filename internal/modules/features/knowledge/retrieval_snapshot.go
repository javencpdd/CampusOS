package knowledge

import (
	"context"
	"encoding/json"
	"sync/atomic"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"

	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// Index pages may write Knowledge's own members/projections, while treating
// Community/RichText source rows as read-only. Source facts remain domain-owned
// and G1 is evaluated on every use, just as for the retrieval callback.
func (s *IndexService) withPageSourceRead(ctx context.Context, ids []string, read func(context.Context) error) error {
	articles := func(c context.Context) error {
		if source, ok := s.collections.articles.(*PublicArticleSourceService); ok {
			return source.withPublicTextSnapshot(c, ids, read)
		}
		return read(c)
	}
	if source, ok := s.collections.query.(communityport.SourceReadSnapshot); ok {
		return source.WithSourceReadSnapshot(ctx, ids, articles)
	}
	return articles(ctx)
}

// Recheck all current relationship IDs/epochs, including phantoms, without
// decoding a second copy of every member DTO. Existing rows are SHARE-locked;
// repository-owned non-reused facts_version binds every authority field.
func (p *PgRepository) matchesRetrievalState(ctx context.Context, state CollectionState) (bool, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return false, ErrSourceUnavailable
	}
	expected := map[string]int64{"c:" + state.Collection.ID: state.Collection.Version}
	for _, r := range state.Selections {
		expected["s:"+r.ID] = r.Version
	}
	for _, m := range state.Members {
		expected["m:"+m.ID] = m.Version
	}
	rows, e := transaction.ExecutorFor(ctx, p.pool).Query(ctx, `SELECT 'c:'||id,facts_version FROM knowledge_collections WHERE id=$1 UNION ALL SELECT 's:'||id,facts_version FROM knowledge_source_selections WHERE collection_id=$1 UNION ALL SELECT 'm:'||id,facts_version FROM knowledge_collection_sources WHERE collection_id=$1`, state.Collection.ID)
	if e != nil {
		return false, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var epoch int64
		if e = rows.Scan(&id, &epoch); e != nil {
			return false, e
		}
		if expected[id] != epoch || epoch < 1 {
			return false, nil
		}
		count++
	}
	return count == len(expected), rows.Err()
}

func searchDocumentBytes(d IndexDocument) int {
	n := len(d.Title)
	for _, c := range d.Chunks {
		n += len(c)
	}
	return n
}

type officialReadKey struct{}
type officialRead struct {
	repo   *PgRepository
	tx     pgx.Tx
	active atomic.Bool
	rows   map[string]record
}

func (s *PublicArticleSourceService) withPublicTextSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	if source, ok := s.reader.(richtext.PublicTextSourceSnapshot); ok {
		return source.WithPublicTextReadSnapshot(ctx, ids, read)
	}
	return read(ctx)
}
func (p *PgRepository) withOfficialReadSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	tx, ok := transaction.FromContext(ctx)
	if !ok || read == nil || len(ids) > MaxMembers {
		return ErrSourceUnavailable
	}
	for _, id := range ids {
		if !opaque("ko_", id) {
			return ErrInvalid
		}
	}
	rows, e := transaction.ExecutorFor(ctx, p.pool).Query(ctx, `SELECT id,data,facts_version FROM knowledge_official_sources WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, ids)
	if e != nil {
		return e
	}
	snap := &officialRead{repo: p, tx: tx, rows: map[string]record{}}
	for rows.Next() {
		var r record
		if e = rows.Scan(&r.ID, &r.Data, &r.Version); e != nil {
			rows.Close()
			return e
		}
		snap.rows[r.ID] = r
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	snap.active.Store(true)
	defer snap.active.Store(false)
	return read(context.WithValue(ctx, officialReadKey{}, snap))
}
func (p *PgRepository) sourceReadOfficial(ctx context.Context, id string) (record, bool) {
	s, ok := ctx.Value(officialReadKey{}).(*officialRead)
	tx, active := transaction.FromContext(ctx)
	if !ok || !active || s.repo != p || s.tx != tx || !s.active.Load() {
		return record{}, false
	}
	r, found := s.rows[id]
	if found {
		r.Data = append(json.RawMessage(nil), r.Data...)
	}
	return r, found
}
