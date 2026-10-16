package personaldocuments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAttachmentCopySourcePostgreSQLActualLocksAndCurrentChanges(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"current", "status_aba", "object_namespace", "version_sha", "session_revoke"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(161201 + i*10)
			s, pool, proof, _ := documentAuthorPostgres(t, owner)
			d, err := s.UploadForPrincipal(ctx, proof, "source.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
			if err != nil {
				t.Fatal(err)
			}
			source, err := s.ReadOnly().OpenOwnAttachmentSource(ctx, proof, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Object.Reader.Close()
			if err := source.CheckCurrent(ctx, true); err == nil {
				t.Fatal("PG locks accepted no actual transaction")
			}
			ran := false
			err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
				if err := source.CheckCurrent(tx, true); err != nil {
					return err
				}
				ran = true
				for _, row := range []struct{ q, id string }{
					{"SELECT id FROM personal_documents WHERE id=$1 FOR UPDATE NOWAIT", d.ID},
					{"SELECT id FROM personal_document_versions WHERE id=$1 FOR UPDATE NOWAIT", d.CurrentVersionID},
					{"SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", d.CurrentVersion.SourceObjectID},
				} {
					_, err := pool.Exec(ctx, row.q, row.id)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
						t.Fatal("actual source row not locked", row, err)
					}
				}
				return nil
			})
			if err != nil || !ran {
				t.Fatal("actual source lock proof not exercised", err, ran)
			}
			switch mode {
			case "status_aba":
				d, err = s.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
				d, err = s.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, StatusActive)
				if err != nil {
					t.Fatal(err)
				}
			case "object_namespace":
				_, err = pool.Exec(ctx, "UPDATE storage_objects SET namespace='changed' WHERE id=$1", d.CurrentVersion.SourceObjectID)
			case "version_sha":
				_, err = pool.Exec(ctx, "UPDATE personal_document_versions SET sha256=repeat('0',64) WHERE id=$1", d.CurrentVersionID)
			case "session_revoke":
				_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE user_id=$1", owner)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = source.CheckCurrent(ctx, false)
			if (err == nil) != (mode == "current") {
				t.Fatal("actual current source proof result wrong", mode, err)
			}
		})
	}
}
