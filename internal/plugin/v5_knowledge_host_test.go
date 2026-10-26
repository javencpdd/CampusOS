package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/campusos/CampusOS/sdk/go"
)

func TestKnowledgeManagedHostDeclaredGrantCurrentFactsAndBoundedOutput(t *testing.T) {
	for _, mode := range []string{"allow", "wrong_collection", "unknown_field", "missing_grant", "revoked", "changed", "aba", "disable", "forged", "deadline", "budget"} {
		t.Run(mode, func(t *testing.T) {
			v, priv := newV5ContributionTestPlatform(t)
			id := "kc_" + strings.Repeat("a", 32)
			v.manifest.Consumes = []PluginV5Consume{{Capability: V5KnowledgeSearch, Scope: PluginV5ConsumeScope{Kind: "collection", CollectionIDs: []string{id}}, Purpose: "Search one public collection", Required: mode != "missing_grant"}}
			if e := os.WriteFile(filepath.Join(v.dir, "plugin.json"), mustV5JSON(v.manifest), 0600); e != nil {
				t.Fatal(e)
			}
			raw, p, e := BuildPluginV5Package(v.dir)
			if e != nil {
				t.Fatal(e)
			}
			env, e := SignPluginV5Package(raw, "test-key", priv)
			if e != nil {
				t.Fatal(e)
			}
			v.raw, v.envelope, v.manifest = raw, env, p.Manifest
			v.installed(t)
			consumes := v.manifest.Consumes
			if mode == "missing_grant" {
				consumes = []PluginV5Consume{}
			}
			v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "grant", Approved: true, Consumes: consumes, Resources: v.manifest.HostResources}, true, v.admin)
			i, _ := v.s.installation(t.Context(), v.c, "")
			v.ready(t, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, v.admin))
			i, _ = v.s.installation(t.Context(), v.c, "")
			h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
			defer h.Close(t.Context())
			lease, e := h.Publish(t.Context(), v.c, "", i.Generation, time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply})
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			features := v5KnowledgeFeature{}
			endpoint := V5PublicKnowledgeEndpoint(V5KnowledgeSearch, func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
				calls++
				wire := `{"public":"one"}`
				if (mode == "changed" && calls > 1) || (mode == "aba" && calls == 2) {
					wire = `{"public":"two"}`
				}
				return json.RawMessage(wire), ctx.Err()
			}, features)
			if mode == "disable" {
				endpoint.Feature = v5NeverFeature{}
			}
			if e = h.BindHost(V5KnowledgeSearch, endpoint); e != nil {
				t.Fatal(e)
			}
			q := `{"collection_id":"` + id + `","query":"public","page":1,"page_size":2}`
			if mode == "wrong_collection" {
				q = strings.ReplaceAll(q, id, "kc_"+strings.Repeat("b", 32))
			}
			if mode == "unknown_field" {
				q = strings.TrimSuffix(q, "}") + `,"owner":"admin"}`
			}
			token := lease.Token
			if mode == "forged" {
				token = "fake"
			}
			if mode == "revoked" {
				g, _ := v.s.grant(t.Context(), v.c)
				v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "revoke"}, true, v.admin)
			}
			limit := int64(4096)
			if mode == "budget" {
				limit = 1
			}
			deadline := time.Now().Add(time.Second)
			if mode == "deadline" {
				deadline = time.Now().Add(-time.Second)
			}
			call := sdk.V5DispatchCall{Contract: "campusos.plugin-dispatch/v1", RequestID: "knowledge-host", Target: sdk.V5DispatchTarget{Kind: "host_api", ID: V5KnowledgeSearch}, InstanceGeneration: i.Generation, DeadlineMS: deadline.UnixMilli(), RequestBytes: int64(len(q)), ResponseLimitBytes: limit}
			out, e := h.HostCall(t.Context(), token, mustV5JSON(call), []byte(q))
			if mode == "allow" {
				if e != nil || !strings.Contains(string(out), "one") {
					t.Fatal(e, string(out))
				}
			} else if e == nil || len(out) > 0 {
				t.Fatal("denied output escaped", mode, e, string(out))
			}
		})
	}
}

type v5NeverFeature struct{}

func (v5NeverFeature) Enabled(string) bool { return false }
func TestKnowledgeHostCatalogAvailabilityIsNotInvented(t *testing.T) {
	v, _ := newV5ContributionTestPlatform(t)
	v.manifest.Consumes = []PluginV5Consume{{Capability: V5KnowledgeSourceRead, Scope: PluginV5ConsumeScope{Kind: "collection", CollectionIDs: []string{"kc_" + strings.Repeat("a", 32)}}, Purpose: "current public citation", Required: true}}
	raw := mustV5JSON(v.manifest)
	if _, e := ValidatePluginV5SourceDeclaration(raw); e != nil {
		t.Fatal(e)
	}
	host, ext := CurrentPluginV5Catalogs()
	if _, e := ValidatePluginV5Manifest(raw, host, ext, v.envelope); e == nil {
		t.Fatal("empty availability snapshot became collection proof")
	}
	e := V5PublicKnowledgeEndpoint(V5KnowledgeSourceRead, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("invalid citation reached reader")
		return nil, nil
	}, v5NeverFeature{})
	if _, err := e.Authorize(t.Context(), "", v.manifest.Consumes[0], json.RawMessage(`{"collection_id":"kc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","member_id":"personal","revision":"none"}`)); err == nil {
		t.Fatal("private reference accepted")
	}
}

type v5KnowledgeFeature struct{}

func (v5KnowledgeFeature) Enabled(id string) bool { return id == "knowledge" }
