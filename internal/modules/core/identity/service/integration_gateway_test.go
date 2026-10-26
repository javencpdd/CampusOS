package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func integrationTarget() port.IntegrationAccessTarget {
	return port.IntegrationAccessTarget{Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", Resource: port.PolicyResourceRef{Kind: "community.thread", ID: "123"}}
}
func TestIntegrationCurrentCredentialAndWhitelist(t *testing.T) {
	for _, mode := range []string{"allow", "empty", "tampered", "jwt", "host", "revoked", "suspended", "principal_revoked", "future", "expiry", "wrong_resource", "wrong_channel", "wrong_endpoint", "admin_mcp", "wrong_action", "suspended_rule", "expired_rule", "cross_rule"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, store := integrationEnv(t, "current")
			ctx := context.Background()
			p, err := s.CreatePrincipal(ctx, "machine", proof)
			if err != nil {
				t.Fatal(err)
			}
			req := integrationRequest(p)
			switch mode {
			case "future":
				req.NotBeforeMS += 60000
				for i := range req.Rules {
					req.Rules[i].NotBeforeMS = req.NotBeforeMS
				}
			case "suspended_rule":
				req.Rules[0].Status = "suspended"
			case "expired_rule":
				req.Rules[0].ExpiresAtMS = time.Now().UnixMilli() - 1
			case "cross_rule":
				req.Rules[0].ResourceIDs = []string{"999"}
			}
			issued, err := s.IssueCredential(ctx, p.ID, req, proof)
			if err != nil {
				t.Fatal(err)
			}
			raw := issued.Token
			target := integrationTarget()
			switch mode {
			case "empty":
				raw = ""
			case "tampered":
				raw = raw[:65] + "A"
				if raw == issued.Token {
					raw = raw[:65] + "B"
				}
			case "jwt":
				raw = "eyJhbGciOiJIUzI1NiJ9.user-or-admin.jwt"
			case "host":
				raw = "host_token_plugin"
			case "revoked":
				_, err = s.UpdateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, nil, true, proof)
			case "suspended":
				_, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "suspended", p.Version, proof)
			case "principal_revoked":
				_, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "revoked", p.Version, proof)
			case "expiry":
				s.now = func() time.Time { return time.UnixMilli(req.ExpiresAtMS) }
			case "wrong_resource":
				target.Resource.ID = "999"
			case "wrong_channel":
				target.Channel = "admin_http"
			case "wrong_endpoint":
				target.Endpoint = "unlisted"
			case "admin_mcp":
				target.Channel = "integration_mcp"
				target.Endpoint = "admin_accounts.list"
			case "wrong_action":
				target.Action = "community.thread.purge"
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			data, err := s.ReadForIntegration(ctx, raw, target, func(context.Context) (any, error) { calls++; return "public-body", nil })
			if mode == "allow" {
				if err != nil || data != "public-body" || calls != 1 {
					t.Fatal(data, calls, err)
				}
				audits, _, _ := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
				found := false
				for _, a := range audits {
					if a.ActorType == "integration" && a.ActorID == p.ID {
						found = true
					}
				}
				if !found {
					t.Fatal("machine read missing typed audit")
				}
			} else if err == nil || data != nil || calls != 0 {
				t.Fatalf("denial failed: data=%v calls=%d err=%v", data, calls, err)
			}
		})
	}
}
func TestIntegrationReadFinalProofAndResourceFailure(t *testing.T) {
	for _, mode := range []string{"source_denied", "revoke_during_read", "scope_change", "principal_aba", "expiry", "cancel", "audit_failure", "outbox_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, store := integrationEnv(t, "current")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, err := s.CreatePrincipal(ctx, "final proof", proof)
			if err != nil {
				t.Fatal(err)
			}
			issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(mode, "failure") {
				s.reliable = newIntegrationFailReliability(s, store, mode == "audit_failure")
			}
			data, err := s.ReadForIntegration(ctx, issued.Token, integrationTarget(), func(tx context.Context) (any, error) {
				switch mode {
				case "source_denied":
					return nil, errors.New("owner denies private content")
				case "revoke_during_read":
					c, e := s.repo.GetIntegrationCredential(tx, issued.Credential.ID, true)
					if e != nil {
						return nil, e
					}
					c.Status = "revoked"
					_, e = s.repo.UpdateIntegrationCredential(tx, &c, c.Version)
					if e != nil {
						return nil, e
					}
				case "scope_change":
					c, e := s.repo.GetIntegrationCredential(tx, issued.Credential.ID, true)
					if e != nil {
						return nil, e
					}
					c.Rules[0].ResourceIDs = []string{"999"}
					_, e = s.repo.UpdateIntegrationCredential(tx, &c, c.Version)
					if e != nil {
						return nil, e
					}
				case "principal_aba":
					cur, e := s.repo.GetIntegrationPrincipal(tx, p.ID, true)
					if e != nil {
						return nil, e
					}
					cur.Status = "suspended"
					_, e = s.repo.UpdateIntegrationPrincipal(tx, &cur, cur.Version)
					if e != nil {
						return nil, e
					}
					cur.Status = "active"
					_, e = s.repo.UpdateIntegrationPrincipal(tx, &cur, cur.Version)
					if e != nil {
						return nil, e
					}
				case "expiry":
					s.now = func() time.Time { return time.UnixMilli(issued.Credential.ExpiresAtMS) }
				case "cancel":
					cancel()
				}
				return "never-return-this-body", nil
			})
			if err == nil || data != nil {
				t.Fatal("failed proof returned bytes", data, err)
			}
		})
	}
}

func newIntegrationFailReliability(s *IntegrationService, store *reliability.MemoryStore, audit bool) *reliability.Service {
	return reliability.NewService(transaction.NewMemory(s.repo.(transaction.Snapshotter), store), integrationFailStore{Store: store, audit: audit, event: !audit})
}
