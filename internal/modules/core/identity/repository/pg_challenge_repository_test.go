package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChallengeRateLockKeyIsPostgreSQLTextSafe(t *testing.T) {
	window := domain.ChallengeRateWindow{Scope: "email_window", SubjectDigest: strings.Repeat("a", 64)}
	key := challengeRateLockKey(window)
	if strings.ContainsRune(key, '\x00') {
		t.Fatalf("advisory lock key contains a PostgreSQL-invalid NUL: %q", key)
	}
	if len(key) != 64 {
		t.Fatalf("advisory lock key length=%d, want SHA-256 hex length 64", len(key))
	}
	other := challengeRateLockKey(domain.ChallengeRateWindow{Scope: "ip_window", SubjectDigest: window.SubjectDigest})
	if key == other {
		t.Fatal("different rate scopes produced the same advisory lock key")
	}
}

func TestPgChallengeRepositoryTryConsumeRate(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_IDENTITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_IDENTITY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	digest := fmt.Sprintf("challenge-rate-integration-%d", time.Now().UTC().UnixNano())
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM identity_challenge_rate_limits WHERE subject_digest=$1`, digest)
	}()
	repository := NewPgChallengeRepository(pool)
	observedAt := time.Now().UTC().Truncate(time.Second)
	windows := []domain.ChallengeRateWindow{{
		Scope: "email_window", SubjectDigest: digest, ObservedAt: observedAt,
		Duration: 10 * time.Minute, Limit: 1,
	}}
	consumed, err := repository.TryConsumeRate(ctx, windows)
	if err != nil || !consumed {
		t.Fatalf("first PostgreSQL rate consumption consumed=%v err=%v", consumed, err)
	}
	consumed, err = repository.TryConsumeRate(ctx, windows)
	if err != nil || consumed {
		t.Fatalf("limited PostgreSQL rate consumption consumed=%v err=%v", consumed, err)
	}
}
