package repository

import (
	"context"
	"os"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationPostgreSQLStore(t *testing.T) {
	url := os.Getenv("CAMPUSOS_INTEGRATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("owned Integration PG target not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `DELETE FROM integration_credentials WHERE principal_id='ip_store'; DELETE FROM integration_principals WHERE id='ip_store'`); err != nil {
		t.Fatal(err)
	}
	integrationStoreContract(t, NewPgIntegrationRepository(pool), transaction.NewPostgreSQL(pool))
	for _, sql := range []string{`UPDATE integration_principals SET facts_version=1 WHERE id='ip_store'`, `UPDATE integration_principals SET facts_version=1 WHERE id='ip_store'`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	var version int64
	if err := pool.QueryRow(ctx, `SELECT facts_version FROM integration_principals WHERE id='ip_store'`).Scan(&version); err != nil || version <= 1 {
		t.Fatalf("SQL rewound facts: %v", err)
	}
}
