package personaldocuments

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

type documentAuthorLocker interface {
	LockDocumentForAuthorCommand(context.Context, string, string, string) (DocumentDetail, error)
}

func (r *MemoryRepository) LockDocumentForAuthorCommand(ctx context.Context, owner, id, selected string) (DocumentDetail, error) {
	if !transaction.Active(ctx) {
		return DocumentDetail{}, ErrDocumentCommandUnavailable
	}
	return r.Get(ctx, owner, id)
}

func (r *PgRepository) LockDocumentForAuthorCommand(ctx context.Context, owner, id, selected string) (DocumentDetail, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return DocumentDetail{}, ErrDocumentCommandUnavailable
	}
	d, err := scanDetail(r.db(ctx).QueryRow(ctx, detailSQL+` WHERE d.id=$1::bigint AND d.owner_user_id=$2::bigint FOR UPDATE OF d`, id, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentDetail{}, ErrNotFound
	}
	if err != nil {
		return DocumentDetail{}, err
	}
	// The current and selected immutable version rows are separate facts. Lock
	// them in stable order, including the source of a historical restore.
	rows, err := r.db(ctx).Query(ctx, `SELECT id FROM personal_document_versions WHERE document_id=$1::bigint AND (id=$2::bigint OR id=NULLIF($3,'')::bigint) ORDER BY id FOR UPDATE`, id, d.CurrentVersionID, selected)
	if err != nil {
		return DocumentDetail{}, err
	}
	for rows.Next() {
		var versionID int64
		if err = rows.Scan(&versionID); err != nil {
			break
		}
	}
	rows.Close()
	if err != nil {
		return DocumentDetail{}, err
	}
	if err = rows.Err(); err != nil {
		return DocumentDetail{}, err
	}
	return r.Get(ctx, owner, id)
}
