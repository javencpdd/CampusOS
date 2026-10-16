package reliability

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

var ErrOperationFactsUnavailable = errors.New("reliable operation facts unavailable")

// ScopedOperationReader is a first-party persistence seam, never authentication.
// Its private Details must not be returned by an HTTP read model.
type ScopedOperationReader interface {
	ListSubjectOperations(context.Context, string, string, string, int) ([]Operation, error)
	GetSubjectOperation(context.Context, string, string, string, string, bool) (*Operation, error)
}

func cloneOperation(op Operation) Operation {
	op.Details = append([]byte(nil), op.Details...)
	op.CreatedAt, op.UpdatedAt = op.CreatedAt.UTC(), op.UpdatedAt.UTC()
	return op
}

func validOperationScope(kind, subject, owner string) bool {
	return strings.TrimSpace(kind) != "" && strings.TrimSpace(subject) != "" && strings.TrimSpace(owner) != ""
}

func (s *MemoryStore) ListSubjectOperations(ctx context.Context, kind, subject, owner string, limit int) ([]Operation, error) {
	if !validOperationScope(kind, subject, owner) || limit < 1 || limit > 101 {
		return nil, ErrOperationFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []Operation{}
	for _, op := range s.operations {
		if op.Kind == kind && op.SubjectType == subject && op.SubjectID == owner && op.Status != OperationSucceeded {
			items = append(items, cloneOperation(op))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryStore) GetSubjectOperation(ctx context.Context, id, kind, subject, owner string, lock bool) (*Operation, error) {
	if !validOperationScope(kind, subject, owner) || id == "" || lock && !transaction.Active(ctx) {
		return nil, ErrOperationFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.operations[id]
	if !ok || op.Kind != kind || op.SubjectType != subject || op.SubjectID != owner {
		return nil, ErrOperationNotFound
	}
	op = cloneOperation(op)
	return &op, nil
}

const operationFactColumns = `id,kind,subject_type,subject_id,status,COALESCE(actor_id,''),COALESCE(idempotency_key,''),details,COALESCE(error_message,''),created_at,updated_at`

func scanOperation(row pgx.Row) (*Operation, error) {
	var op Operation
	err := row.Scan(&op.ID, &op.Kind, &op.SubjectType, &op.SubjectID, &op.Status, &op.ActorID, &op.IdempotencyKey, &op.Details, &op.Error, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	op = cloneOperation(op)
	return &op, nil
}

func (s *PostgreSQLStore) ListSubjectOperations(ctx context.Context, kind, subject, owner string, limit int) ([]Operation, error) {
	if !validOperationScope(kind, subject, owner) || limit < 1 || limit > 101 {
		return nil, ErrOperationFactsUnavailable
	}
	rows, err := s.db(ctx).Query(ctx, `SELECT `+operationFactColumns+` FROM platform_operation_runs WHERE kind=$1 AND subject_type=$2 AND subject_id=$3 AND status<>'succeeded' ORDER BY created_at,id LIMIT $4`, kind, subject, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Operation{}
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *op)
	}
	return items, rows.Err()
}

func (s *PostgreSQLStore) GetSubjectOperation(ctx context.Context, id, kind, subject, owner string, lock bool) (*Operation, error) {
	if !validOperationScope(kind, subject, owner) || id == "" {
		return nil, ErrOperationFactsUnavailable
	}
	q := `SELECT ` + operationFactColumns + ` FROM platform_operation_runs WHERE id=$1 AND kind=$2 AND subject_type=$3 AND subject_id=$4`
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrOperationFactsUnavailable
		}
		q += ` FOR UPDATE`
	}
	return scanOperation(s.db(ctx).QueryRow(ctx, q, id, kind, subject, owner))
}
