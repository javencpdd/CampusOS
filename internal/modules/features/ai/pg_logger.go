package ai

import (
	"context"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgCallLogger struct {
	pool *pgxpool.Pool
}

func NewPgCallLogger(pool *pgxpool.Pool) *PgCallLogger {
	return &PgCallLogger{pool: pool}
}

func (l *PgCallLogger) Log(ctx context.Context, entry CallLog) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	query := `INSERT INTO ai_call_logs (
		id, provider, model, source, status, duration_ms,
		prompt_tokens, completion_tokens, total_tokens,
		error_message, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := l.pool.Exec(ctx, query,
		idgen.New(),
		entry.Provider,
		entry.Model,
		entry.Source,
		string(entry.Status),
		entry.Duration.Milliseconds(),
		entry.Usage.PromptTokens,
		entry.Usage.CompletionTokens,
		entry.Usage.TotalTokens,
		entry.Error,
		entry.Timestamp,
	)
	return err
}

func (l *PgCallLogger) List(ctx context.Context, limit int) ([]CallLog, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT provider, model, source, status, duration_ms,
		prompt_tokens, completion_tokens, total_tokens,
		error_message, created_at
		FROM ai_call_logs
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1`
	rows, err := l.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]CallLog, 0, limit)
	for rows.Next() {
		var entry CallLog
		var status string
		var durationMS int64
		if err := rows.Scan(
			&entry.Provider,
			&entry.Model,
			&entry.Source,
			&status,
			&durationMS,
			&entry.Usage.PromptTokens,
			&entry.Usage.CompletionTokens,
			&entry.Usage.TotalTokens,
			&entry.Error,
			&entry.Timestamp,
		); err != nil {
			return nil, err
		}
		entry.Status = CallStatus(status)
		entry.Duration = time.Duration(durationMS) * time.Millisecond
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}
