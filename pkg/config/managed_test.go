package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedRuntimeFileOverridesOnlyCatalogAndPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "# deployment comment\nDATABASE_DSN=must-not-override\nAUTH_ADMIN_MFA_REQUIRED=false # retain this comment\nEMAIL_SMTP_PASSWORD='literal$secret#with\\'quote'\nUNRELATED=unchanged # comment\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMPUSOS_MANAGED_ENV_FILE", path)
	t.Setenv("DATABASE_DSN", "actual-database")
	t.Setenv("AUTH_ADMIN_MFA_REQUIRED", "true")
	cfg := Load()
	if cfg.Database.DSN != "actual-database" || !cfg.Auth.AdminMFADisabled || cfg.Email.SMTPPassword != "literal$secret#with'quote" {
		t.Fatal("managed precedence or literal decoding failed")
	}
	doc, err := ReadRuntimeDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := doc.Updated(map[string]string{"AUTH_ADMIN_MFA_REQUIRED": "true", "EMAIL_SMTP_PASSWORD": "new$credential#'value", "AI_ENABLED": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if err = ReplaceRuntimeDocument(path, doc.Revision, raw); err != nil {
		t.Fatal(err)
	}
	next, err := ReadRuntimeDocument(path)
	if err != nil || next.Values["EMAIL_SMTP_PASSWORD"] != "new$credential#'value" {
		t.Fatal("roundtrip failed", err)
	}
	if !strings.Contains(string(next.Raw), "AUTH_ADMIN_MFA_REQUIRED='true' # retain this comment\n") || !strings.Contains(string(next.Raw), "# deployment comment\nDATABASE_DSN=must-not-override\n") || !strings.Contains(string(next.Raw), "UNRELATED=unchanged # comment\n") {
		t.Fatal("unrelated configuration changed")
	}
	backup, _ := os.ReadFile(path + ".bak")
	if string(backup) != original {
		t.Fatal("backup differs")
	}
	info, _ := os.Stat(path)
	backupInfo, _ := os.Stat(path + ".bak")
	if info.Mode().Perm() != 0600 || backupInfo.Mode().Perm() != 0600 {
		t.Fatal("non-private settings")
	}
	if err = ReplaceRuntimeDocument(path, doc.Revision, raw); !errors.Is(err, ErrManagedConflict) {
		t.Fatal("stale write allowed", err)
	}
	if _, err = next.Updated(map[string]string{"DATABASE_DSN": "other"}); err == nil {
		t.Fatal("infrastructure key accepted")
	}
	if _, err = next.Updated(map[string]string{"EMAIL_SMTP_PASSWORD": "inject\nSERVER_PORT=1"}); err == nil {
		t.Fatal("newline injection accepted")
	}
}

func TestManagedRuntimeFileRejectsAmbiguityAndSymlinks(t *testing.T) {
	for _, raw := range []string{"AI_ENABLED=true\nAI_ENABLED=false\n", "EMAIL_SMTP_PASSWORD=${SHELL_SECRET}\n", "EMAIL_SMTP_PASSWORD=\"$INTERPOLATED\"\n", "EMAIL_SMTP_PASSWORD='unclosed\n"} {
		path := filepath.Join(t.TempDir(), ".env")
		_ = os.WriteFile(path, []byte(raw), 0600)
		if _, err := ReadRuntimeDocument(path); err == nil {
			t.Fatal("ambiguous file accepted")
		}
	}
	path := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(path, []byte("AI_ENABLED=false\n"), 0600)
	link := path + "-link"
	if err := os.Symlink(path, link); err == nil {
		if _, err = ReadRuntimeDocument(link); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	doc, _ := ReadRuntimeDocument(path)
	_ = os.Symlink(path, path+".bak")
	if err := ReplaceRuntimeDocument(path, doc.Revision, []byte("AI_ENABLED=true\n")); err == nil {
		t.Fatal("backup symlink accepted")
	}
}

func TestManagedValidationNoSecretDisclosure(t *testing.T) {
	t.Setenv("CAMPUSOS_MANAGED_ENV_FILE", "")
	values := Load().Managed.Values
	values["EMAIL_PROVIDER"] = "smtp"
	values["EMAIL_SMTP_HOST"] = "smtp.example.test"
	values["EMAIL_SMTP_FROM"] = "sender@example.test"
	if err := ValidateRuntimeValues(values); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"EMAIL_SMTP_PORT": "70000", "AUTH_ADMIN_MFA_REQUIRED": "maybe", "AI_TIMEOUT": "0s", "CAMPUSOS_PLUGIN_UI_PARENTS": "https://example.test/path"} {
		old := values[key]
		values[key] = value
		if err := ValidateRuntimeValues(values); err == nil || strings.Contains(err.Error(), value) {
			t.Fatal("invalid value accepted or disclosed", key)
		}
		values[key] = old
	}
}

func TestRuntimeLiteralRoundtrip(t *testing.T) {
	for _, v := range []string{"ending\\", "a$quote'ending\\", "normal$literal#value", "quote'and\\slash", "中文值", ""} {
		parsed, e := runtimeLiteral(quoteRuntimeLiteral(v))
		if e != nil || parsed != v {
			t.Fatal("literal roundtrip failed", e)
		}
	}
}

func TestCompleteRuntimeDocumentLiteralGrammar(t *testing.T) {
	for _, text := range []string{"FOO=1\nFOO=2", "echo oops", "FOO=${BAR}", "FOO=\"$BAR\"", "FOO='open", "FOO=\"line\\nnext\"", "FOO=ok\x00"} {
		if _, err := ParseRuntimeText([]byte(text)); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	values, err := ParseRuntimeText([]byte("# untouched\nexport OTHER='a$literal#value' # comment\nEMPTY=\nAI_ENABLED=true\n"))
	if err != nil || values["OTHER"] != "a$literal#value" || values["EMPTY"] != "" {
		t.Fatal("literal document failed", err)
	}
	if _, err := ParseRuntimeText(make([]byte, MaxRuntimeDocumentBytes+1)); err == nil {
		t.Fatal("oversized document accepted")
	}
}
