package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSecretInspectKeyOptionsRejectUnsafeInput(t *testing.T) {
	if got, err := parseSecretInspectKeyOptions([]string{"--key-id", "old-v1"}); err != nil || got != "old-v1" {
		t.Fatalf("valid key ID: got %q, err %v", got, err)
	}
	for _, args := range [][]string{
		nil,
		{"--key-id", ""},
		{"--key-id", " old-v1"},
		{"--key-id", "old-v1 "},
		{"--key-id", strings.Repeat("x", 65)},
		{"--key-id", "old\nv1"},
		{"--key-id", "old\x1bv1"},
		{"--key-id", "old\u202ev1"},
		{"--key-id", "old-v1", "extra"},
		{"--key-id", "old-v1", "--apply"},
	} {
		if _, err := parseSecretInspectKeyOptions(args); err == nil {
			t.Fatalf("accepted unsafe inspect-key arguments: %q", args)
		}
	}
}

func TestSecretInspectKeyNeedsDatabaseButNoKeyring(t *testing.T) {
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"secret", "inspect-key", "--key-id", "old-v1"}, &stdout, &stderr); code != 1 {
		t.Fatalf("missing database exit = %d", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "DATABASE_DSN is required") ||
		strings.Contains(stderr.String(), "CAMPUSOS_SECRET_") {
		t.Fatalf("unexpected inspect-key output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSecretInspectKeyResultOnlyAggregates(t *testing.T) {
	result := secretInspectKeyResult{
		Table: "plugin_secret_values", KeyID: "old-v1",
		SnapshotAt:      time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC),
		TotalReferences: 6, ActiveReferences: 2, RotatedReferences: 2,
		RevokedReferences: 2, OtherReferences: 0, PluginCount: 2,
		SnapshotHasReferences: true,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"table", "key_id", "snapshot_at", "total_references", "active_references",
		"rotated_references", "revoked_references", "other_references", "plugin_count", "snapshot_has_references"} {
		if !bytes.Contains(raw, []byte("\""+field+"\"")) {
			t.Fatalf("missing aggregate field %s: %s", field, raw)
		}
	}
	for _, unsafe := range []string{"secret_name", "ciphertext", "owner_user_id", "DATABASE_DSN"} {
		if bytes.Contains(raw, []byte(unsafe)) {
			t.Fatalf("unsafe field %s in output: %s", unsafe, raw)
		}
	}
}
