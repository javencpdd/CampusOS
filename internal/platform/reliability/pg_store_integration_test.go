package reliability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLClaimNeverExceedsMaxAttempts(t *testing.T) {
	databaseURL := reliabilityTestDatabaseURL()
	if databaseURL == "" {
		t.Skip("CAMPUSOS_RELIABILITY_TEST_DATABASE_URL is not configured")
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
	store := NewPostgreSQLStore(pool)
	prefix := fmt.Sprintf("claim-%d", time.Now().UTC().UnixNano())
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM platform_outbox WHERE id LIKE $1`, prefix+"%")
	}()
	now := time.Now().UTC()
	fixtures := []Event{
		{ID: prefix + "-pending", Type: "test.pg.exhausted", Status: StatusPending, Attempts: 8, MaxAttempts: 8, AvailableAt: now.Add(-time.Minute)},
		{ID: prefix + "-retry", Type: "test.pg.exhausted", Status: StatusRetry, Attempts: 8, MaxAttempts: 8, AvailableAt: now.Add(time.Hour)},
		{ID: prefix + "-processing", Type: "test.pg.exhausted", Status: StatusProcessing, Attempts: 8, MaxAttempts: 8, LeaseOwner: "worker-old", LeaseUntil: timePointer(now.Add(-time.Minute)), LeaseGeneration: 3},
		{ID: prefix + "-over", Type: "test.pg.exhausted", Status: StatusProcessing, Attempts: 103, MaxAttempts: 8, LeaseOwner: "worker-old", LeaseUntil: timePointer(now.Add(-time.Minute)), LeaseGeneration: 9},
	}
	for index := range fixtures {
		if _, err := store.Enqueue(ctx, &fixtures[index]); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := store.Claim(ctx, "worker-new", 10, time.Second)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("exhausted PostgreSQL claim=%+v err=%v", claimed, err)
	}
	for _, fixture := range fixtures {
		stored, err := store.Get(ctx, fixture.ID)
		if err != nil || stored.Status != StatusDead || stored.Attempts != fixture.Attempts || stored.DeadLetteredAt == nil {
			t.Fatalf("fixture %s did not converge: event=%+v err=%v", fixture.ID, stored, err)
		}
	}

	reclaimable := Event{
		ID: prefix + "-reclaim", Type: "test.pg.reclaim", Status: StatusProcessing,
		Attempts: 7, MaxAttempts: 8, LeaseOwner: "worker-old", LeaseUntil: timePointer(now.Add(-time.Minute)), LeaseGeneration: 4,
	}
	if _, err := store.Enqueue(ctx, &reclaimable); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.Claim(ctx, "worker-new", 1, time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 8 || claimed[0].LeaseGeneration != 5 {
		t.Fatalf("reclaimable PostgreSQL event=%+v err=%v", claimed, err)
	}
	if err := store.Complete(ctx, reclaimable.ID, "worker-old", 4); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale PostgreSQL completion error=%v", err)
	}
	if err := store.Complete(ctx, reclaimable.ID, "worker-new", 5); err != nil {
		t.Fatal(err)
	}

	transition := Event{
		ID: prefix + "-transition", Type: "test.pg.transition", Status: StatusPending,
		MaxAttempts: 3, AvailableAt: now.Add(-time.Minute),
	}
	if _, err := store.Enqueue(ctx, &transition); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.Claim(ctx, "worker-transition", 1, time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("transition claim=%+v err=%v", claimed, err)
	}
	if err := store.Retry(ctx, transition.ID, "worker-transition", claimed[0].LeaseGeneration, now.Add(-time.Second), "durable event consumer failed"); err != nil {
		t.Fatalf("PostgreSQL retry transition: %v", err)
	}
	claimed, err = store.Claim(ctx, "worker-transition", 1, time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("retry reclaim=%+v err=%v", claimed, err)
	}
	if err := store.DeadLetter(ctx, transition.ID, "worker-transition", claimed[0].LeaseGeneration, "durable event consumer failed"); err != nil {
		t.Fatalf("PostgreSQL dead-letter transition: %v", err)
	}
	storedTransition, err := store.Get(ctx, transition.ID)
	if err != nil || storedTransition.Status != StatusDead || storedTransition.DeadLetteredAt == nil {
		t.Fatalf("dead-lettered transition=%+v err=%v", storedTransition, err)
	}
}

func reliabilityTestDatabaseURL() string {
	if databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_RELIABILITY_TEST_DATABASE_URL")); databaseURL != "" {
		return databaseURL
	}
	databaseName := strings.TrimSpace(os.Getenv("CAMPUSOS_RELIABILITY_TEST_DB_NAME"))
	if databaseName == "" {
		return ""
	}
	host := strings.TrimSpace(os.Getenv("CAMPUSOS_RELIABILITY_TEST_DB_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("CAMPUSOS_RELIABILITY_TEST_DB_PORT"))
	if port == "" {
		port = "5432"
	}
	user := strings.TrimSpace(os.Getenv("CAMPUSOS_RELIABILITY_TEST_DB_USER"))
	if user == "" {
		user = "campusos"
	}
	connection := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, os.Getenv("CAMPUSOS_RELIABILITY_TEST_DB_PASSWORD")),
		Host:   net.JoinHostPort(host, port),
		Path:   databaseName,
	}
	query := connection.Query()
	query.Set("sslmode", "disable")
	connection.RawQuery = query.Encode()
	return connection.String()
}
