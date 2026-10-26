package service

import (
	"context"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestIntegrationKnowledgeEndpointExactCollectionAndCurrentKey(t *testing.T) {
	for _, mode := range []string{"allow", "other_collection", "community_rule", "empty_rules", "scope_removed", "revoked", "mcp_channel", "private_kind"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, _ := integrationEnv(t, "current")
			ctx := t.Context()
			principal, err := s.CreatePrincipal(ctx, "knowledge consumer", proof)
			if err != nil {
				t.Fatal(err)
			}
			collection := "kc_" + strings.Repeat("a", 32)
			req := integrationRequest(principal)
			req.Rules = req.Rules[:1]
			req.Rules[0].Endpoint = "knowledge.source.read"
			req.Rules[0].Action = "knowledge.source.read"
			req.Rules[0].ResourceKind = "public_collection"
			req.Rules[0].ResourceIDs = []string{collection}
			if mode == "community_rule" {
				req = integrationRequest(principal)
			}
			if mode == "empty_rules" {
				req.Rules = []port.IntegrationAccessRule{}
			}
			issued, err := s.IssueCredential(ctx, principal.ID, req, proof)
			if err != nil {
				t.Fatal(err)
			}
			target := port.IntegrationAccessTarget{Channel: "integration_http", Endpoint: "knowledge.source.read", Action: "knowledge.source.read", Resource: port.PolicyResourceRef{Kind: "public_collection", ID: collection}}
			switch mode {
			case "other_collection":
				target.Resource.ID = "kc_" + strings.Repeat("b", 32)
			case "mcp_channel":
				target.Channel = "integration_mcp"
			case "private_kind":
				target.Resource.Kind = "personal.document"
			case "scope_removed", "revoked":
				_, err = s.UpdateCredential(ctx, principal.ID, issued.Credential.ID, issued.Credential.Version, []port.IntegrationAccessRule{}, mode == "revoked", proof)
				if err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			data, err := s.ReadForIntegration(ctx, issued.Token, target, func(context.Context) (any, error) {
				calls++
				return "current-public-source", nil
			})
			if mode == "allow" {
				if err != nil || data != "current-public-source" || calls != 1 {
					t.Fatal("registered Knowledge endpoint failed", err)
				}
			} else if err == nil || data != nil || calls != 0 {
				t.Fatal("scope or revoked key reached the source owner", calls, err)
			}
		})
	}
}

func TestIntegrationSearchAndCitationRulesRemainIndependent(t *testing.T) {
	for _, endpoint := range []string{"knowledge.source.read", "knowledge.search", "knowledge.public.read"} {
		t.Run(endpoint, func(t *testing.T) {
			s, proof, _ := integrationEnv(t, "current")
			ctx := t.Context()
			p, err := s.CreatePrincipal(ctx, "scoped search", proof)
			if err != nil {
				t.Fatal(err)
			}
			req := integrationRequest(p)
			req.Rules = req.Rules[:1]
			req.Rules[0].Endpoint = endpoint
			if endpoint == "knowledge.public.read" {
				req.Rules[0].Channel = "integration_mcp"
			}
			req.Rules[0].Action = "knowledge.source.read"
			req.Rules[0].ResourceKind = "public_collection"
			req.Rules[0].ResourceIDs = []string{"kc_" + strings.Repeat("a", 32)}
			issued, err := s.IssueCredential(ctx, p.ID, req, proof)
			if err != nil {
				t.Fatal(err)
			}
			for _, targetEndpoint := range []string{"knowledge.source.read", "knowledge.search", "knowledge.public.read"} {
				target := port.IntegrationAccessTarget{Channel: "integration_http", Endpoint: targetEndpoint, Action: "knowledge.source.read", Resource: port.PolicyResourceRef{Kind: "public_collection", ID: req.Rules[0].ResourceIDs[0]}}
				if targetEndpoint == "knowledge.public.read" {
					target.Channel = "integration_mcp"
				}
				controlErr := s.CheckForIntegration(ctx, issued.Token, target)
				if (controlErr == nil) != (targetEndpoint == endpoint) {
					t.Fatal("control scope bypass", controlErr)
				}
				calls := 0
				data, err := s.ReadForIntegration(ctx, issued.Token, target, func(context.Context) (any, error) { calls++; return "current-public", nil })
				if targetEndpoint == endpoint {
					if err != nil || data != "current-public" || calls != 1 {
						t.Fatal("own exact endpoint denied", err)
					}
				} else if err == nil || data != nil || calls != 0 {
					t.Fatal("endpoint scope borrowed", calls, err)
				}
			}
		})
	}
}
