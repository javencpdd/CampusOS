package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSecretRewrapOptionsBoundedAndApplyRequiresOperatorIntent(t *testing.T) {
	valid := []struct {
		name string
		args []string
	}{
		{"preview defaults", []string{"--plugin-id", "7"}},
		{"preview upper batch bound", []string{"--plugin-id", "7", "--batch-size", "500"}},
		{"apply empty set", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", "admin-7", "--reason", "planned rotation"}},
		{"apply bounded batch", []string{"--plugin-id", "7", "--batch-size", "1", "--apply", "--expected-count", "3", "--actor", "admin-7", "--reason", "planned rotation"}},
		{"actor byte boundary", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", strings.Repeat("a", 64), "--reason", "rotation"}},
	}
	for _, item := range valid {
		t.Run(item.name, func(t *testing.T) {
			got, err := parseSecretRewrapOptions(item.args)
			if err != nil || got.PluginID != 7 || got.BatchSize < 1 || got.BatchSize > 500 {
				t.Fatalf("options = %#v, err = %v", got, err)
			}
		})
	}

	invalid := []struct {
		name string
		args []string
	}{
		{"missing plugin ID", nil},
		{"zero plugin ID", []string{"--plugin-id", "0"}},
		{"negative plugin ID", []string{"--plugin-id", "-4"}},
		{"batch below bound", []string{"--plugin-id", "7", "--batch-size", "0"}},
		{"batch above bound", []string{"--plugin-id", "7", "--batch-size", "501"}},
		{"missing expected count", []string{"--plugin-id", "7", "--apply", "--actor", "admin", "--reason", "rotation"}},
		{"negative expected count", []string{"--plugin-id", "7", "--apply", "--expected-count", "-1", "--actor", "admin", "--reason", "rotation"}},
		{"missing actor", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--reason", "rotation"}},
		{"missing reason", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", "admin"}},
		{"reason too long", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", "admin", "--reason", strings.Repeat("x", 501)}},
		{"actor too long", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", strings.Repeat("x", 65), "--reason", "rotation"}},
		{"actor multibyte exceeds database limit", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", strings.Repeat("中", 22), "--reason", "rotation"}},
		{"reason control", []string{"--plugin-id", "7", "--apply", "--expected-count", "0", "--actor", "admin", "--reason", "rotation\nsecret"}},
		{"apply arguments in preview", []string{"--plugin-id", "7", "--expected-count", "0"}},
		{"explicit default expected count in preview", []string{"--plugin-id", "7", "--expected-count", "-1"}},
		{"empty actor in preview", []string{"--plugin-id", "7", "--actor", ""}},
		{"DSN flag forbidden", []string{"--plugin-id", "7", "--dsn", "postgres://user:password@host/db"}},
		{"key flag forbidden", []string{"--plugin-id", "7", "--key", "private-material"}},
		{"unexpected positional argument", []string{"--plugin-id", "7", "extra"}},
	}
	for _, item := range invalid {
		t.Run(item.name, func(t *testing.T) {
			if _, err := parseSecretRewrapOptions(item.args); err == nil {
				t.Fatal("expected argument rejection")
			}
		})
	}
}

func TestSecretRewrapRequiresExplicitEnvironmentWithoutEchoingValues(t *testing.T) {
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID", "new")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "new:private-material")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"secret", "rewrap", "--plugin-id", "7"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "DATABASE_DSN is required") {
		t.Fatalf("missing DSN should fail before connecting: code=%d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "private-material") {
		t.Fatal("secret material appeared in command output")
	}

	t.Setenv("DATABASE_DSN", "postgres://user:password@invalid-host.invalid/db")
	t.Setenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS", "")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"secret", "rewrap", "--plugin-id", "7"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "CAMPUSOS_SECRET_ACTIVE_KEY_ID and CAMPUSOS_SECRET_ENCRYPTION_KEYS are required") {
		t.Fatalf("incomplete keyring should fail before connecting: code=%d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "password") {
		t.Fatal("database address appeared in command output")
	}
}

func TestSecretRewrapOutputContainsOnlyAggregateFields(t *testing.T) {
	var output bytes.Buffer
	result := secretRewrapResult{PluginID: 7, ActiveKeyID: "new", DryRun: false, Eligible: 3, Selected: 2, Rewrapped: 2, Remaining: 1, AuditID: "operation-7"}
	if err := writeSecretRewrapResult(&output, result); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	want := []string{"plugin_id", "active_key_id", "dry_run", "eligible", "selected", "rewrapped", "remaining", "complete", "audit_id"}
	got := make([]string, 0, len(decoded))
	for key := range decoded {
		got = append(got, key)
	}
	for _, key := range want {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing result field %q", key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected result fields: %v", got)
	}
	if !reflect.DeepEqual(decoded["selected"], float64(2)) || decoded["complete"] != false {
		t.Fatalf("incorrect aggregate result: %s", output.String())
	}
}
