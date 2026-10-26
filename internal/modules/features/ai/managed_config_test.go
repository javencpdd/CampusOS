package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/config"
)

func TestManagedDeploymentBoundaries(t *testing.T) {
	valid := config.AIConfig{Enabled: true, BaseURL: "https://models.example.test/v1", Model: "remote", APIKey: "synthetic-secret", LocalBaseURL: "http://127.0.0.1:11434/v1", LocalModel: "local", ActiveKeyID: "owned", EncryptionKeys: "owned:" + strings.Repeat("ab", 32), DefaultProfile: "remote-chat", BudgetRequests: 10, MaxConcurrent: 2, MaxOutputTokens: 32, Timeout: "2s"}
	if profiles, ring, e := managedConfiguration(valid); e != nil || len(profiles) != 2 || ring == nil {
		t.Fatal("valid managed deployment", e)
	}
	cases := map[string]func(*config.AIConfig){
		"remote_http":                 func(c *config.AIConfig) { c.BaseURL = "http://models.example.test/v1" },
		"remote_userinfo":             func(c *config.AIConfig) { c.BaseURL = "https://u:secret@models.example.test/v1" },
		"query":                       func(c *config.AIConfig) { c.BaseURL = "https://models.example.test/v1?x=1" },
		"local_hostname":              func(c *config.AIConfig) { c.LocalBaseURL = "http://localhost:11434/v1" },
		"local_lan":                   func(c *config.AIConfig) { c.LocalBaseURL = "http://192.168.1.2:11434/v1" },
		"local_no_port":               func(c *config.AIConfig) { c.LocalBaseURL = "http://127.0.0.1/v1" },
		"key_missing":                 func(c *config.AIConfig) { c.EncryptionKeys = "" },
		"key_bad_length":              func(c *config.AIConfig) { c.EncryptionKeys = "owned:aa" },
		"key_duplicate":               func(c *config.AIConfig) { c.EncryptionKeys += "," + c.EncryptionKeys },
		"credential_header_injection": func(c *config.AIConfig) { c.APIKey = "token\r\nInjected: true" },
		"quota":                       func(c *config.AIConfig) { c.BudgetRequests = 0 },
		"output":                      func(c *config.AIConfig) { c.MaxOutputTokens = 65537 },
		"concurrency":                 func(c *config.AIConfig) { c.MaxConcurrent = 33 },
		"timeout":                     func(c *config.AIConfig) { c.Timeout = "61s" },
		"profile":                     func(c *config.AIConfig) { c.DefaultProfile = "uncompiled" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid
			mutate(&c)
			if _, _, e := managedConfiguration(c); !errors.Is(e, ErrManagedUnavailable) || strings.Contains(e.Error(), "secret") {
				t.Fatal("invalid configuration accepted/leaked", e)
			}
		})
	}
	for _, code := range []string{"ai.ai.read", "ai.ai.manage"} {
		q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "ai.profile_inventory", ID: "all"}}
		if q.Validate() != nil || q.RequiresCurrentMFA() != (code == "ai.ai.manage") {
			t.Fatal("Admin authority contract")
		}
		q.Scope.ID = "caller-selected"
		if q.Validate() == nil {
			t.Fatal("arbitrary authority scope")
		}
	}
	// A stale resolver cannot retain the initial current User facts.
	p := identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "101"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "owned-session"}
	_, bound, e := managedPrincipal(context.Background(), func(context.Context) (identityport.PrincipalContext, error) { return p, nil })
	if e != nil {
		t.Fatal(e)
	}
	p.CredentialID = "changed-session"
	if _, e = bound(context.Background()); e == nil {
		t.Fatal("changed current User facts accepted")
	}
}
