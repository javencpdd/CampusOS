package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/gin-gonic/gin"
)

type authorityFixture struct {
	denied   bool
	stale    bool
	calls    int
	revokeAt int
	optional bool
}

func (f *authorityFixture) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	f.calls++
	if q.Validate() != nil || lock && !transaction.Active(ctx) {
		return identityport.AdminExecutionAuthority{}, ErrDenied
	}
	p, e := resolve(ctx)
	if e != nil {
		return identityport.AdminExecutionAuthority{}, e
	}
	expires := time.Now().Truncate(time.Hour).Add(2 * time.Hour).UnixMilli()
	if f.stale {
		expires = time.Now().Add(-time.Hour).UnixMilli()
	}
	return identityport.AdminExecutionAuthority{Principal: p, Query: q, Allowed: !f.denied && (f.revokeAt == 0 || f.calls < f.revokeAt), ExpiresAtMS: expires, Binding: "fixture-current-authority"}, nil
}

func (f *authorityFixture) LoadRuntimeConfigPasswordAuthority(ctx context.Context, resolve identityport.SessionPrincipalResolver, password string, lock bool) (identityport.AdminExecutionAuthority, error) {
	if password != "fixture-current-admin-password" {
		return identityport.AdminExecutionAuthority{}, identityport.ErrAdminConfirmationPassword
	}
	return f.LoadAdminExecutionAuthority(ctx, identityport.AdminExecutionQuery{PermissionCode: "platform.runtime_config.manage", Scope: identityport.PolicyResourceRef{Kind: "platform.runtime_config", ID: "current"}}, resolve, lock)
}

func TestRuntimeConfigPasswordAlternativeIsRequestScopedAndAudited(t *testing.T) {
	s, resolve, reader, store := fixture(t)
	ctx := context.Background()
	reader.optional = false
	before, _ := config.ReadRuntimeDocument(s.cfg.Path)
	input := Update{Revision: before.Revision, Changes: map[string]string{"AI_ENABLED": "true"}, VerificationMethod: "password", CurrentPassword: "wrong-password"}
	if _, err := s.Apply(ctx, resolve, input); !errors.Is(err, identityport.ErrAdminConfirmationPassword) {
		t.Fatal("wrong password accepted", err)
	}
	current, _ := config.ReadRuntimeDocument(s.cfg.Path)
	if current.Revision != before.Revision {
		t.Fatal("failed verification changed the file")
	}
	input.CurrentPassword = "fixture-current-admin-password"
	result, err := s.Apply(ctx, resolve, input)
	if err != nil || !result.Pending {
		t.Fatal("password alternative failed", err)
	}
	audits, total, err := store.ListCommandAudits(ctx, reliability.PageRequest{})
	if err != nil || total != 1 {
		t.Fatal("incorrect audit count", err, total)
	}
	raw, _ := json.Marshal(audits)
	if strings.Contains(string(raw), input.CurrentPassword) {
		t.Fatal("password leaked into audit")
	}
	input.Revision = result.Revision
	input.CurrentPassword = ""
	input.VerificationMethod = "mfa"
	if _, err = s.Apply(ctx, resolve, input); !errors.Is(err, ErrMFA) {
		t.Fatal("password alternative upgraded MFA", err)
	}
}

func TestRuntimeConfigVerificationInputCannotBypassAuthority(t *testing.T) {
	for _, mode := range []string{"empty-password", "unknown-method", "password-without-method", "mfa-with-password", "revoked", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, resolve, reader, store := fixture(t)
			reader.optional = false
			ctx := context.Background()
			doc, _ := config.ReadRuntimeDocument(s.cfg.Path)
			input := Update{Revision: doc.Revision, Changes: map[string]string{"AI_ENABLED": "true"}, VerificationMethod: "password", CurrentPassword: "fixture-current-admin-password", Restart: true}
			switch mode {
			case "empty-password":
				input.CurrentPassword = ""
			case "unknown-method":
				input.VerificationMethod = "none"
			case "password-without-method":
				input.VerificationMethod = ""
			case "mfa-with-password":
				input.VerificationMethod = "mfa"
			case "revoked":
				reader.revokeAt = 3
			case "audit-failure":
				s.reliable = reliability.NewService(transaction.NewMemory(), auditFailure{store})
			}
			if _, err := s.Apply(ctx, resolve, input); err == nil {
				t.Fatal("invalid alternative verification accepted")
			}
			current, _ := config.ReadRuntimeDocument(s.cfg.Path)
			if current.Revision != doc.Revision || s.restarting {
				t.Fatal("failed confirmation wrote or restarted")
			}
		})
	}
}

type auditFailure struct{ *reliability.MemoryStore }

func (f auditFailure) RecordCommandAudit(context.Context, reliability.CommandAudit) error {
	return errors.New("private internal details")
}

func fixture(t *testing.T) (*Service, identityport.SessionPrincipalResolver, *authorityFixture, *reliability.MemoryStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if e := os.WriteFile(path, []byte("# preserve this\nAUTH_ADMIN_MFA_REQUIRED=false\nEMAIL_SMTP_PASSWORD='fixture-hidden-password'\nAI_ENABLED=false\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CAMPUSOS_MANAGED_ENV_FILE", path)
	cfg := config.Load()
	store := reliability.NewMemoryStore()
	reader := &authorityFixture{optional: true}
	resolve := func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "admin", ID: "22001"}, Audience: "admin", AuthenticationStrength: "password", CredentialID: "33001", AdminMFAOptional: reader.optional}, nil
	}
	s := New(cfg.Managed, reader, reliability.NewService(transaction.NewMemory(), store), func() {})
	return s, resolve, reader, store
}
func TestRuntimeConfigSaveRedactionAndRestart(t *testing.T) {
	s, resolve, _, store := fixture(t)
	ctx := context.Background()
	before, e := s.Read(ctx, resolve)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(before)
	if strings.Contains(string(raw), "fixture-hidden-password") || strings.Contains(string(raw), s.cfg.Path) {
		t.Fatal("secret or path leaked")
	}
	result, e := s.Apply(ctx, resolve, Update{Revision: before.Revision, Changes: map[string]string{"AI_ENABLED": "true"}})
	if e != nil || !result.Pending || result.Restarting {
		t.Fatal("save failed", e)
	}
	file, _ := config.ReadRuntimeDocument(s.cfg.Path)
	if file.Values["EMAIL_SMTP_PASSWORD"] != "fixture-hidden-password" {
		t.Fatal("blank UI secret was lost")
	}
	if _, e = s.Apply(ctx, resolve, Update{Revision: before.Revision, Changes: map[string]string{"AI_ENABLED": "false"}}); !errors.Is(e, config.ErrManagedConflict) {
		t.Fatal("stale write accepted")
	}
	result, e = s.Apply(ctx, resolve, Update{Revision: result.Revision, Changes: map[string]string{}, Restart: true})
	if e != nil || !result.Restarting {
		t.Fatal("restart not accepted", e)
	}
	if _, e = s.Apply(ctx, resolve, Update{Revision: result.Revision, Restart: true}); !errors.Is(e, ErrPending) {
		t.Fatal("duplicate restart accepted")
	}
	audits, total, e := store.ListCommandAudits(ctx, reliability.PageRequest{})
	if e != nil || total != 2 || len(audits) != 2 {
		t.Fatal("intent not audited", e, total)
	}
	marshalled, _ := json.Marshal(audits)
	if strings.Contains(string(marshalled), "fixture-hidden-password") {
		t.Fatal("audit leaked secret")
	}
	// A new API generation re-reads the actual file rather than the inherited
	// environment. The saved setting is active and no longer pending.
	newConfig := config.Load()
	next := New(newConfig.Managed, s.admins, s.reliable, func() {})
	active, e := next.Read(ctx, resolve)
	if e != nil || active.Pending || active.Generation == before.Generation || !newConfig.AI.Enabled {
		t.Fatal("reload failed", e)
	}
}
func TestRuntimeConfigFailuresLeaveFileUntouched(t *testing.T) {
	for _, mode := range []string{"no-permission", "mfa-required", "stale-mfa", "audit-failure", "revoked-after-audit", "unknown-key", "invalid-value"} {
		t.Run(mode, func(t *testing.T) {
			s, resolve, reader, store := fixture(t)
			ctx := context.Background()
			before, _ := config.ReadRuntimeDocument(s.cfg.Path)
			input := Update{Revision: before.Revision, Changes: map[string]string{"AI_ENABLED": "true"}, Restart: true}
			switch mode {
			case "no-permission":
				reader.denied = true
			case "mfa-required":
				reader.optional = false
			case "stale-mfa":
				reader.stale = true
			case "audit-failure":
				s.reliable = reliability.NewService(transaction.NewMemory(), auditFailure{store})
			case "revoked-after-audit":
				reader.revokeAt = 3
			case "unknown-key":
				input.Changes = map[string]string{"JWT_SECRET": "must-not-be-writable"}
			case "invalid-value":
				input.Changes = map[string]string{"AI_ENABLED": "malformed"}
			}
			if _, err := s.Apply(ctx, resolve, input); err == nil {
				t.Fatal("invalid apply accepted")
			}
			after, _ := config.ReadRuntimeDocument(s.cfg.Path)
			if after.Revision != before.Revision || s.restarting {
				t.Fatal("failed apply changed file or restarted")
			}
		})
	}
}

type sessionFixture struct{}

func (sessionFixture) CurrentAdminSession(context.Context, string, string) (identityport.AdminSessionAuthority, error) {
	return identityport.AdminSessionAuthority{AccountID: "22001", SessionID: "33001", AuthenticationStrength: "password", AccountVersion: 1, SessionVersion: 1, ExpiresAt: time.Now().Truncate(time.Hour).Add(2 * time.Hour), AdminMFAOptional: true}, nil
}
func TestRuntimeConfigHTTPClosedBodyAndCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"missing-csrf", "extra-field", "valid"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _ := fixture(t)
			doc, _ := config.ReadRuntimeDocument(s.cfg.Path)
			h := NewHandler(s, sessionFixture{})
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("admin_account_id", "22001")
				c.Set("session_id", "33001")
				c.Set("auth_version", int64(1))
			})
			r.PUT("/config", h.Update)
			body := `{"revision":"` + doc.Revision + `","changes":{"AI_ENABLED":"true"},"restart":false}`
			if mode == "extra-field" {
				body = strings.TrimSuffix(body, "}") + `,"path":"/etc/other"}`
			}
			req := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if mode != "missing-csrf" {
				req.AddCookie(&http.Cookie{Name: "campusos_admin_csrf", Value: "fixture-csrf-token-123456"})
				req.Header.Set("X-Admin-CSRF-Token", "fixture-csrf-token-123456")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := 200
			if mode == "missing-csrf" {
				want = 403
			}
			if mode == "extra-field" {
				want = 400
			}
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "fixture-hidden-password") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestCompleteDocumentRequiresConfirmationAndSynchronizesCatalog(t *testing.T) {
	s, resolve, reader, store := fixture(t)
	reader.optional = false
	ctx := context.Background()
	before, _ := config.ReadRuntimeDocument(s.cfg.Path)
	request := DocumentConfirmation{Revision: before.Revision, VerificationMethod: "password", CurrentPassword: "wrong"}
	if _, err := s.ReadDocument(ctx, resolve, request); err == nil {
		t.Fatal("document disclosed without confirmation")
	}
	request.CurrentPassword = "fixture-current-admin-password"
	document, err := s.ReadDocument(ctx, resolve, request)
	if err != nil || document.Content != string(before.Raw) {
		t.Fatal("confirmed document read failed", err)
	}
	raw := document.Content + "# new section\nOTHER_DEPLOYMENT_SETTING='new$literal'\nAI_TIMEOUT=17s\n"
	result, err := s.Apply(ctx, resolve, Update{Revision: before.Revision, Document: &raw, VerificationMethod: "password", CurrentPassword: request.CurrentPassword})
	if err != nil || !result.Pending || len(result.DeploymentKeys) != 1 || result.DeploymentKeys[0] != "OTHER_DEPLOYMENT_SETTING" {
		t.Fatal("document save failed", err)
	}
	for _, field := range result.Fields {
		if field.Key == "AI_TIMEOUT" && (field.Value != "17s" || field.ActiveValue != "30s") {
			t.Fatal("catalog did not synchronize")
		}
		if field.Kind == "secret" && (field.Value != "" || field.ActiveValue != "") {
			t.Fatal("snapshot leaked credentials")
		}
	}
	after, _ := config.ReadRuntimeDocument(s.cfg.Path)
	backup, _ := os.ReadFile(s.cfg.Path + ".bak")
	if string(after.Raw) != raw || string(backup) != string(before.Raw) {
		t.Fatal("full file or backup differs")
	}
	if _, err = s.Apply(ctx, resolve, Update{Revision: before.Revision, Document: &raw, VerificationMethod: "password", CurrentPassword: request.CurrentPassword}); !errors.Is(err, config.ErrManagedConflict) {
		t.Fatal("stale document accepted", err)
	}
	audits, _, _ := store.ListCommandAudits(ctx, reliability.PageRequest{})
	auditJSON, _ := json.Marshal(audits)
	for _, secret := range []string{"fixture-hidden-password", request.CurrentPassword, "new$literal"} {
		if strings.Contains(string(auditJSON), secret) {
			t.Fatal("document content leaked into audit")
		}
	}
}
func TestCompleteDocumentFailuresPreserveFile(t *testing.T) {
	for _, mode := range []string{"duplicate", "syntax", "invalid-catalog", "mixed", "denied", "audit", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			s, resolve, reader, store := fixture(t)
			before, _ := config.ReadRuntimeDocument(s.cfg.Path)
			raw := string(before.Raw) + "OTHER=new\n"
			input := Update{Revision: before.Revision, Document: &raw, Restart: true, VerificationMethod: "password", CurrentPassword: "fixture-current-admin-password"}
			switch mode {
			case "duplicate":
				raw += "OTHER=second\n"
			case "syntax":
				raw += "OTHER2=${SHELL}\n"
			case "invalid-catalog":
				raw += "AI_TIMEOUT=0s\n"
			case "mixed":
				input.Changes = map[string]string{"AI_ENABLED": "true"}
			case "denied":
				reader.denied = true
			case "audit":
				s.reliable = reliability.NewService(transaction.NewMemory(), auditFailure{store})
			case "revoked":
				reader.revokeAt = 3
			}
			if _, err := s.Apply(context.Background(), resolve, input); err == nil {
				t.Fatal("invalid full save accepted")
			}
			after, _ := config.ReadRuntimeDocument(s.cfg.Path)
			if after.Revision != before.Revision || s.restarting {
				t.Fatal("failed save modified file or scheduled restart")
			}
		})
	}
}

func TestDocumentDisclosureFailsClosed(t *testing.T) {
	for _, mode := range []string{"denied", "audit", "revoked", "no-confirmation", "stale"} {
		t.Run(mode, func(t *testing.T) {
			s, resolve, reader, store := fixture(t)
			before, _ := config.ReadRuntimeDocument(s.cfg.Path)
			input := DocumentConfirmation{Revision: before.Revision, VerificationMethod: "password", CurrentPassword: "fixture-current-admin-password"}
			switch mode {
			case "denied":
				reader.denied = true
			case "audit":
				s.reliable = reliability.NewService(transaction.NewMemory(), auditFailure{store})
			case "revoked":
				reader.revokeAt = 3
			case "no-confirmation":
				input.VerificationMethod = ""
				input.CurrentPassword = ""
			case "stale":
				input.Revision = strings.Repeat("0", 64)
			}
			out, err := s.ReadDocument(context.Background(), resolve, input)
			if err == nil || out.Content != "" {
				t.Fatal("sensitive document disclosed after failed authorization")
			}
		})
	}
}
func TestDocumentHTTPRequiresCSRFAndClosedConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"missing-csrf", "extra-path", "valid"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _ := fixture(t)
			before, _ := config.ReadRuntimeDocument(s.cfg.Path)
			h := NewHandler(s, sessionFixture{})
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("admin_account_id", "22001")
				c.Set("session_id", "33001")
				c.Set("auth_version", int64(1))
			})
			r.POST("/document", h.Document)
			body := `{"revision":"` + before.Revision + `","verification_method":"password","current_password":"fixture-current-admin-password"}`
			if mode == "extra-path" {
				body = strings.TrimSuffix(body, "}") + `,"path":"/etc/passwd"}`
			}
			req := httptest.NewRequest(http.MethodPost, "/document", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if mode != "missing-csrf" {
				req.AddCookie(&http.Cookie{Name: "campusos_admin_csrf", Value: "fixture-csrf-token-123456"})
				req.Header.Set("X-Admin-CSRF-Token", "fixture-csrf-token-123456")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			expected := 200
			if mode == "missing-csrf" {
				expected = 403
			}
			if mode == "extra-path" {
				expected = 400
			}
			if rec.Code != expected {
				t.Fatal("unexpected document HTTP status", rec.Code, expected)
			}
			if rec.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("document response permits caching")
			}
			if expected != 200 && strings.Contains(rec.Body.String(), "fixture-hidden-password") {
				t.Fatal("failed read disclosed a secret")
			}
		})
	}
}
