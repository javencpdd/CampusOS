package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthorizationAuditActorMemory(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRoleRepository()
	cases := []AuthorizationAudit{
		{ActorKind: "user", ActorID: "100", Outcome: "allow"},
		{ActorKind: "admin", ActorID: "100", Outcome: "deny"},
		{ActorKind: "integration", ActorID: "partner.alpha:1", Outcome: "allow"},
		{ActorKind: "anonymous", Outcome: "deny"},
		{ActorKind: "system", ActorID: "local-cli", Outcome: "allow"},
	}
	for _, audit := range cases {
		if err := repo.RecordAuthorizationAudit(ctx, audit); err != nil {
			t.Fatalf("record %s/%s: %v", audit.ActorKind, audit.ActorID, err)
		}
	}
	invalid := []AuthorizationAudit{
		{ActorKind: "", ActorID: "100", Outcome: "allow"},
		{ActorKind: "anonymous", ActorID: "100", Outcome: "allow"},
		{ActorKind: "admin", Outcome: "allow"},
		{ActorKind: "legacy_unknown", Outcome: "allow"},
		{ActorKind: "integration", ActorID: "bad id", Outcome: "allow"},
		{ActorKind: "worker", ActorID: strings.Repeat("a", 129), Outcome: "allow"},
	}
	for _, audit := range invalid {
		if err := repo.RecordAuthorizationAudit(ctx, audit); !errors.Is(err, ErrInvalidAuthorizationAuditActor) {
			t.Fatalf("invalid %s/%s: got %v", audit.ActorKind, audit.ActorID, err)
		}
	}
	items, err := repo.ListAuthorizationAudits(ctx, 20)
	if err != nil || len(items) != len(cases) {
		t.Fatalf("list: len=%d err=%v", len(items), err)
	}
	seen := make(map[string]bool)
	for _, audit := range items {
		seen[audit.ActorKind+":"+audit.ActorID] = true
	}
	if !seen["user:100"] || !seen["admin:100"] || !seen["anonymous:"] {
		t.Fatalf("actor domains merged: %v", seen)
	}
	var group sync.WaitGroup
	writeErrors := make(chan error, 32)
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			kind := "user"
			if index%2 == 1 {
				kind = "admin"
			}
			writeErrors <- repo.RecordAuthorizationAudit(ctx, AuthorizationAudit{ActorKind: kind, ActorID: "shared-42", Outcome: "allow"})
		}(i)
	}
	group.Wait()
	close(writeErrors)
	for err := range writeErrors {
		if err != nil {
			t.Fatalf("concurrent memory audit write: %v", err)
		}
	}
	items, err = repo.ListAuthorizationAudits(ctx, 100)
	if err != nil || len(items) != len(cases)+32 {
		t.Fatalf("concurrent memory audits: len=%d err=%v", len(items), err)
	}
	counts := map[string]int{}
	for _, audit := range items {
		if audit.ActorID == "shared-42" {
			counts[audit.ActorKind]++
		}
	}
	if counts["user"] != 16 || counts["admin"] != 16 {
		t.Fatalf("concurrent actor domains merged or lost: %v", counts)
	}
	restored := NewMemoryRoleRepository()
	restored.Restore(repo.Snapshot())
	items, err = restored.ListAuthorizationAudits(ctx, 100)
	if err != nil || len(items) != len(cases)+32 {
		t.Fatalf("restored list: len=%d err=%v", len(items), err)
	}
}

func TestAuthorizationAuditActorPostgreSQL(t *testing.T) {
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
	repo := NewPgRoleRepository(pool)
	requestID := "audit-actor-test-" + time.Now().UTC().Format("20060102150405.000000000")
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM authorization_audits WHERE request_id=$1`, requestID) }()
	for _, audit := range []AuthorizationAudit{
		{ActorKind: "user", ActorID: "100", Outcome: "allow", RequestID: requestID},
		{ActorKind: "admin", ActorID: "100", Outcome: "deny", RequestID: requestID},
		{ActorKind: "integration", ActorID: "partner.alpha:1", Outcome: "allow", RequestID: requestID},
		{ActorKind: "anonymous", Outcome: "deny", RequestID: requestID},
	} {
		if err := repo.RecordAuthorizationAudit(ctx, audit); err != nil {
			t.Fatalf("record %s/%s: %v", audit.ActorKind, audit.ActorID, err)
		}
	}
	if err := repo.RecordAuthorizationAudit(ctx, AuthorizationAudit{ActorKind: "user", ActorID: "bad id", Outcome: "allow"}); !errors.Is(err, ErrInvalidAuthorizationAuditActor) {
		t.Fatalf("invalid actor accepted: %v", err)
	}
	var group sync.WaitGroup
	writeErrors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			kind := "user"
			if index%2 == 1 {
				kind = "admin"
			}
			writeErrors <- repo.RecordAuthorizationAudit(ctx, AuthorizationAudit{ActorKind: kind, ActorID: "shared-42", Outcome: "allow", RequestID: requestID})
		}(i)
	}
	group.Wait()
	close(writeErrors)
	for err := range writeErrors {
		if err != nil {
			t.Fatalf("concurrent PostgreSQL audit write: %v", err)
		}
	}
	var userCount, adminCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE actor_kind='user'), count(*) FILTER (WHERE actor_kind='admin') FROM authorization_audits WHERE request_id=$1 AND actor_id='shared-42'`, requestID).Scan(&userCount, &adminCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 8 || adminCount != 8 {
		t.Fatalf("concurrent PostgreSQL actor domains merged or lost: user=%d admin=%d", userCount, adminCount)
	}
	items, err := repo.ListAuthorizationAudits(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, audit := range items {
		if audit.RequestID == requestID {
			seen[audit.ActorKind+":"+audit.ActorID] = true
		}
	}
	for _, key := range []string{"user:100", "admin:100", "integration:partner.alpha:1", "anonymous:"} {
		if !seen[key] {
			t.Fatalf("missing PostgreSQL audit actor %s in %v", key, seen)
		}
	}
}
