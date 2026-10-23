package repository

import (
	"context"
	"sort"
	"strconv"
)

type SourceReferenceInventory interface {
	SourceThreadIDs(context.Context, string, string, int) ([]string, error)
}

func sourceInventoryInput(board, after string, limit int) (int64, int64, error) {
	b, e := strconv.ParseInt(board, 10, 64)
	if e != nil || b < 1 || strconv.FormatInt(b, 10) != board || limit < 1 || limit > 33 {
		return 0, 0, ErrThreadOwnerListInvalid
	}
	a := int64(0)
	if after != "" {
		a, e = strconv.ParseInt(after, 10, 64)
		if e != nil || a < 1 || strconv.FormatInt(a, 10) != after {
			return 0, 0, ErrThreadOwnerListInvalid
		}
	}
	return b, a, nil
}
func (r *PgThreadRepository) SourceThreadIDs(ctx context.Context, board, after string, limit int) ([]string, error) {
	b, a, e := sourceInventoryInput(board, after, limit)
	if e != nil {
		return nil, e
	}
	rows, e := r.db(ctx).Query(ctx, `SELECT id::text FROM threads WHERE category_id=$1 AND id>$2 ORDER BY id LIMIT $3`, b, a, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *MemoryThreadRepository) SourceThreadIDs(ctx context.Context, board, after string, limit int) ([]string, error) {
	_, a, e := sourceInventoryInput(board, after, limit)
	if e != nil {
		return nil, e
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	numbers := []int64{}
	for id, row := range r.threads {
		if row.CategoryID != board {
			continue
		}
		n, e := strconv.ParseInt(id, 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != id {
			return nil, ErrThreadOwnerListInvalid
		}
		if n > a {
			numbers = append(numbers, n)
		}
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	if len(numbers) > limit {
		numbers = numbers[:limit]
	}
	ids := []string{}
	for _, id := range numbers {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	return ids, ctx.Err()
}
