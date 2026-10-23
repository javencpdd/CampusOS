package repository

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// These snapshots are private repository facts for a compiled-in, read-only
// callback. G1 still runs on every public read. They never survive the callback
// or transaction, and cannot serve a different repository/transaction.
type sourceThreadSnapshotKey struct{}
type sourceCategorySnapshotKey struct{}
type sourceThreadSnapshot struct {
	repo   *PgThreadRepository
	tx     pgx.Tx
	active atomic.Bool
	rows   map[string]*domain.Thread
}
type sourceCategorySnapshot struct {
	repo   *PgCategoryRepository
	tx     pgx.Tx
	active atomic.Bool
	rows   []*domain.Category
}

func sourceReadIDs(ids []string) ([]int64, error) {
	if len(ids) > 1000 {
		return nil, ErrThreadOwnerListInvalid
	}
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		n, e := strconv.ParseInt(id, 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != id {
			return nil, ErrThreadOwnerListInvalid
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
func (r *PgThreadRepository) WithSourceThreadReadSnapshot(ctx context.Context, ids []string, read func(context.Context) error) error {
	tx, ok := transaction.FromContext(ctx)
	if !ok || read == nil {
		return errors.New("source snapshot requires checked transaction")
	}
	keys, e := sourceReadIDs(ids)
	if e != nil {
		return e
	}
	// Sort before locking, and keep all rows pinned until the enclosing checked
	// read commits. A concurrent takedown cannot commit behind these facts.
	rows, e := r.db(ctx).Query(ctx, selectThreadSQL("id = ANY($1::bigint[])")+" ORDER BY id FOR SHARE", keys)
	if e != nil {
		return e
	}
	s := &sourceThreadSnapshot{repo: r, tx: tx, rows: map[string]*domain.Thread{}}
	retainedBytes := 0
	for _, id := range ids {
		s.rows[id] = nil
	}
	for rows.Next() {
		v, e := scanThread(rows)
		if e != nil {
			rows.Close()
			return e
		}
		retainedBytes += len(v.Title) + len(v.Content) + len(v.ModerationReason) + len(v.AuthorName)
		for _, tag := range v.Tags {
			retainedBytes += len(tag)
		}
		if retainedBytes > 4<<20 {
			s.rows = nil
			rows.Close()
			return read(ctx)
		}
		s.rows[v.ID] = v
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	s.active.Store(true)
	defer s.active.Store(false)
	return read(context.WithValue(ctx, sourceThreadSnapshotKey{}, s))
}
func (r *PgThreadRepository) SourceReadPinned(ctx context.Context, id string) bool {
	s, ok := ctx.Value(sourceThreadSnapshotKey{}).(*sourceThreadSnapshot)
	tx, active := transaction.FromContext(ctx)
	if !ok || !active || s.repo != r || s.tx != tx || !s.active.Load() {
		return false
	}
	_, found := s.rows[id]
	return found
}
func (r *PgThreadRepository) sourceReadThread(ctx context.Context, id string) (*domain.Thread, bool) {
	s, ok := ctx.Value(sourceThreadSnapshotKey{}).(*sourceThreadSnapshot)
	tx, inTx := transaction.FromContext(ctx)
	if !ok || !inTx || s.repo != r || s.tx != tx || !s.active.Load() {
		return nil, false
	}
	v, found := s.rows[id]
	return cloneThread(v), found
}
func (r *PgCategoryRepository) WithSourceCategoryReadSnapshot(ctx context.Context, read func(context.Context) error) error {
	tx, ok := transaction.FromContext(ctx)
	if !ok || read == nil {
		return errors.New("source category snapshot requires checked transaction")
	}
	rows, e := r.ListForSource(ctx)
	if e != nil {
		return e
	}
	s := &sourceCategorySnapshot{repo: r, tx: tx, rows: rows}
	s.active.Store(true)
	defer s.active.Store(false)
	return read(context.WithValue(ctx, sourceCategorySnapshotKey{}, s))
}
func (r *PgCategoryRepository) sourceReadCategories(ctx context.Context) ([]*domain.Category, bool) {
	s, ok := ctx.Value(sourceCategorySnapshotKey{}).(*sourceCategorySnapshot)
	tx, inTx := transaction.FromContext(ctx)
	if !ok || !inTx || s.repo != r || s.tx != tx || !s.active.Load() {
		return nil, false
	}
	out := make([]*domain.Category, 0, len(s.rows))
	for _, v := range s.rows {
		out = append(out, cloneCategory(v))
	}
	return out, true
}

func (r *PgCategoryRepository) ReadSourceBoardFacts(ctx context.Context, id string) (domain.SourceCategoryFact, domain.SourceCategoryFact, error) {
	var board, parent domain.SourceCategoryFact
	if err := ctx.Err(); err != nil {
		return board, parent, err
	}
	s, ok := ctx.Value(sourceCategorySnapshotKey{}).(*sourceCategorySnapshot)
	tx, active := transaction.FromContext(ctx)
	var rows []*domain.Category
	if ok && active && s.repo == r && s.tx == tx && s.active.Load() {
		rows = s.rows
	} else {
		var err error
		rows, err = r.ListForSource(ctx)
		if err != nil {
			return board, parent, err
		}
	}
	project := func(c *domain.Category) domain.SourceCategoryFact {
		v := domain.SourceCategoryFact{ID: c.ID, NodeKind: c.NodeKind, LifecycleStatus: c.LifecycleStatus, Version: c.Version}
		if c.ParentID != nil {
			v.ParentID = *c.ParentID
		}
		return v
	}
	for _, row := range rows {
		if row != nil && row.ID == id {
			board = project(row)
			break
		}
	}
	for _, row := range rows {
		if row != nil && row.ID == board.ParentID {
			parent = project(row)
			break
		}
	}
	return board, parent, nil
}
