package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestV5ConfigurationPartValidation(t *testing.T) {
	for _, r := range []V5Mutation{
		{Kind: "grant", ConfigurationPart: "values"},
		{Kind: "configure", ConfigurationPart: "raw"},
		{Kind: "configure", ConfigurationPart: "values", Values: json.RawMessage(`{}`), SecretRefs: map[string]string{}},
		{Kind: "configure", ConfigurationPart: "secret_refs", Values: json.RawMessage(`{}`), SecretRefs: map[string]string{}},
	} {
		r.Version = "1.0.0"
		r.IdempotencyKey = "parts"
		if v5MutationValid(r, true) {
			t.Fatal("mixed part accepted")
		}
	}
}

type v5ConfigChoices struct {
	active bool
	calls  []string
}

func (p *v5ConfigChoices) ConfigurationOptions(_ context.Context, c V5Coordinate, owner, kind, name string) ([]V5ConfigOption, error) {
	p.calls = append(p.calls, owner+":"+kind+":"+name)
	if !p.active || owner != "" || c.Key == "other" {
		return []V5ConfigOption{}, nil
	}
	switch kind {
	case "profile":
		return []V5ConfigOption{{"current-profile", "当前宿主配置"}}, nil
	case "public_collection":
		return []V5ConfigOption{{"current-public", "当前公开集合"}}, nil
	case "secret_ref":
		if name == "api_key" {
			return []V5ConfigOption{{"secret-ref:current", "受管引用"}}, nil
		}
	}
	return []V5ConfigOption{}, nil
}
func TestV5ConfigCurrentOptionsAndSeparateRepairs(t *testing.T) {
	v := newV5TestPlatform(t)
	m := v.manifest
	m.Configuration = json.RawMessage(`{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"profile":{"type":"string","title":"Profile"},"collection":{"type":"string","title":"Collection"},"count":{"type":"integer","title":"Count"}},"required":["count"]},"defaults":{"count":9007199254740993},"selectors":[{"field":"profile","kind":"profile"},{"field":"collection","kind":"public_collection"}],"secret_names":["api_key"]}`)
	wire, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(v.dir, "plugin.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _, err := BuildPluginV5Package(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, trust := testV5Trust(t)
	envelope, err := SignPluginV5Package(raw, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.s.catalog.SetTrust(trust); err != nil {
		t.Fatal(err)
	}
	v.raw = raw
	v.envelope = envelope
	v.installed(t)
	provider := &v5ConfigChoices{active: true}
	v.s.ConfigResources = provider
	ctx := context.Background()
	options, err := v.s.ConfigurationOptions(ctx, v.c, true, v.admin)
	if err != nil || len(options.Profiles) != 1 || len(options.Collections) != 1 || len(options.Secrets["api_key"]) != 1 {
		t.Fatal(options, err)
	}
	v.mutate(t, V5Mutation{Kind: "configure", ConfigurationPart: "secret_refs", SecretRefs: map[string]string{"api_key": "secret-ref:current"}, IdempotencyKey: "bind-ref"}, true, v.admin)
	cfg, _ := v.s.configuration(ctx, v.c, "", m)
	v.mutate(t, V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: json.RawMessage(`{"profile":"current-profile","collection":"current-public","count":9007199254740994}`), ExpectedRevision: cfg.Revision, IdempotencyKey: "values-preserve-ref"}, true, v.admin)
	cfg, _ = v.s.configuration(ctx, v.c, "", m)
	if cfg.SecretRefs["api_key"] != "secret-ref:current" {
		t.Fatal("ordinary write erased Secret binding")
	}
	provider.active = false
	// A revoked selector may be removed without the ordinary form deleting a
	// stale Secret binding, and the separate Secret form can then repair it.
	v.mutate(t, V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: json.RawMessage(`{"count":9007199254740995}`), ExpectedRevision: cfg.Revision, IdempotencyKey: "repair-selector"}, true, v.admin)
	cfg, _ = v.s.configuration(ctx, v.c, "", m)
	if cfg.SecretRefs["api_key"] != "secret-ref:current" {
		t.Fatal("repair removed unselected binding")
	}
	v.mutate(t, V5Mutation{Kind: "configure", ConfigurationPart: "secret_refs", SecretRefs: map[string]string{}, ExpectedRevision: cfg.Revision, IdempotencyKey: "repair-secret"}, true, v.admin)
	after, _ := v.s.configuration(ctx, v.c, "", m)
	if string(after.Values) != string(cfg.Values) || len(after.SecretRefs) != 0 {
		t.Fatal("Secret repair altered ordinary values")
	}
	request := V5Mutation{Kind: "configure", Version: v.c.Version, ConfigurationPart: "values", Values: json.RawMessage(`{"profile":"current-profile","count":1}`), ExpectedRevision: after.Revision, IdempotencyKey: "stale-option"}
	if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, request, true, v.admin); err != ErrV5Denied {
		t.Fatal("stale current-fact option accepted", err)
	}
	options, err = v.s.ConfigurationOptions(ctx, v.c, true, v.admin)
	if err != nil || len(options.Profiles) != 0 || len(options.Secrets["api_key"]) != 0 {
		t.Fatal("stale choices retained", options, err)
	}
}
func TestV5OrdinaryConfigAtomicMerge(t *testing.T) {
	v := newV5TestPlatform(t)
	v.imported(t)
	v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "install"}, true, v.admin)
	m := v.manifest
	ctx := context.Background()
	cfg, err := v.s.configuration(ctx, v.c, "", m)
	if err != nil {
		t.Fatal(err)
	}
	before := cfg.Revision
	v.mutate(t, V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: json.RawMessage(`{"locale":"en-US"}`), ExpectedRevision: before, IdempotencyKey: "values"}, true, v.admin)
	after, err := v.s.configuration(ctx, v.c, "", m)
	if err != nil || string(after.Values) != `{"locale":"en-US"}` || len(after.SecretRefs) != 0 || after.Revision == before {
		t.Fatal("merge", after, err)
	}
	r := V5Mutation{Kind: "configure", Version: v.c.Version, ConfigurationPart: "secret_refs", SecretRefs: map[string]string{}, ExpectedRevision: before, IdempotencyKey: "stale"}
	if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, r, true, v.admin); err != ErrV5Conflict {
		t.Fatal("stale part did not conflict", err)
	}
	r.ExpectedRevision = after.Revision
	r.IdempotencyKey = "refs"
	if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, r, true, v.admin); err != nil {
		t.Fatal(err)
	}
	final, _ := v.s.configuration(ctx, v.c, "", m)
	if string(final.Values) != string(after.Values) {
		t.Fatal("Secret editor overwrote normal config")
	}
	options, err := v.s.ConfigurationOptions(ctx, v.c, true, v.admin)
	if err != nil || len(options.Profiles) != 0 || len(options.Collections) != 0 {
		t.Fatal("missing provider fabricated options", options, err)
	}
}
