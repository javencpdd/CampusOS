package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type v5ManifestFixture struct {
	Name     string          `json:"name"`
	Manifest json.RawMessage `json:"manifest"`
	Release  json.RawMessage `json:"release"`
	Expected string          `json:"expected"`
}

func v5ManifestFixtures(t *testing.T) []v5ManifestFixture {
	t.Helper()
	raw, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-manifest-native.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []v5ManifestFixture
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestPluginV5ManifestStrictDecode(t *testing.T) {
	cases := v5ManifestFixtures(t)
	for _, c := range cases[:4] {
		t.Run(c.Name, func(t *testing.T) {
			before := bytes.Clone(c.Manifest)
			m, err := ParsePluginV5Manifest(c.Manifest)
			if err != nil || m == nil {
				t.Fatalf("parse: %v", err)
			}
			if !bytes.Equal(c.Manifest, before) {
				t.Fatal("mutated wire")
			}
			m.HostContracts[0] = "changed"
			fresh, err := ParsePluginV5Manifest(c.Manifest)
			if err != nil || fresh.HostContracts[0] != "campusos.host/v1" {
				t.Fatal("shared parsed inputs")
			}
		})
	}
	good := string(cases[0].Manifest)
	mutations := map[string]string{
		"case-alias":        strings.Replace(good, `"api_version"`, `"API_VERSION"`, 1),
		"duplicate":         strings.Replace(good, `"runtime": "wasm"`, `"runtime":"none","runtime":"wasm"`, 1),
		"escaped-duplicate": strings.Replace(good, `"runtime": "wasm"`, `"runtime":"none","runt\u0069me":"wasm"`, 1),
		"null-boolean":      strings.Replace(good, `"required": true`, `"required": null`, 1),
		"missing-boolean":   strings.Replace(good, `"required": true`, `"unknown": true`, 1),
		"bad-unicode":       strings.Replace(good, `"Read public posts"`, `"\ud800"`, 1),
		"bad-utf8":          strings.Replace(good, `"Read public posts"`, "\"\xff\"", 1),
		"dot-path":          strings.Replace(good, `backend/main.wasm`, `../main.wasm`, 1),
		"noncanonical-path": strings.Replace(good, `backend/main.wasm`, `backend/./main.wasm`, 1),
		"parent-only-path":  strings.Replace(good, `backend/main.wasm`, `..`, 1),
		"null-empty-array":  strings.Replace(good, `"network_targets": []`, `"network_targets": null`, 1),
		"overflow":          strings.Replace(good, `"storage_bytes": 1024`, `"storage_bytes": 9007199254740993`, 1),
		"fraction":          strings.Replace(good, `"storage_bytes": 1024`, `"storage_bytes": 1.5`, 1),
		"trailing":          good + ` {}`,
		"too-large":         strings.Repeat(" ", PluginV5ManifestMaxBytes) + good,
		"too-deep":          strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
	}
	for name, raw := range mutations {
		t.Run(name, func(t *testing.T) {
			if raw == good {
				t.Fatal("mutation did not change fixture")
			}
			m, err := ParsePluginV5Manifest([]byte(raw))
			if !errors.Is(err, ErrPluginV5ManifestInvalid) || m != nil {
				t.Fatalf("wanted no manifest and invalid, got %v %v", m, err)
			}
		})
	}
	t.Run("exact-exponent-integer", func(t *testing.T) {
		m, err := ParsePluginV5Manifest([]byte(strings.Replace(good, `"storage_bytes": 1024`, `"storage_bytes": 1.024e3`, 1)))
		if err != nil || m.HostResources.StorageBytes != 1024 {
			t.Fatalf("exact integer: %v", err)
		}
	})
}

func TestPluginV5ManifestCatalogAndRelease(t *testing.T) {
	host, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-host-catalog.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	ext, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	results := []map[string]string{}
	for _, c := range v5ManifestFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			m, err := ValidatePluginV5Manifest(c.Manifest, host, ext, c.Release)
			code := "accepted"
			if err != nil {
				code = err.Error()
			}
			if code != c.Expected {
				t.Fatalf("expected %s got %s", c.Expected, code)
			}
			if (err != nil) != (m == nil) {
				t.Fatal("failure returned manifest")
			}
			results = append(results, map[string]string{"name": c.Name, "actual": code})
		})
	}
	if out := os.Getenv("CAMPUSOS_PLUGIN_V5_MANIFEST_OUTPUT"); out != "" {
		if !filepath.IsAbs(out) {
			t.Fatal("acceptance output must be explicit absolute path")
		}
		raw, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(out, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := v5ManifestFixtures(t)[0]
	for name, inputs := range map[string][][]byte{
		"missing-host": {nil, ext, c.Release}, "missing-extension": {host, nil, c.Release},
		"wrong-catalog-version": {bytes.Replace(host, []byte("host-capabilities/v1"), []byte("host-capabilities/v2"), 1), ext, c.Release},
		"null-catalog":          {[]byte("null"), ext, c.Release},
		"no-release":            {host, ext, nil},
		"release-no-signature":  {host, ext, []byte(`{}`)},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := ValidatePluginV5Manifest(c.Manifest, inputs[0], inputs[1], inputs[2])
			if err == nil || m != nil {
				t.Fatal("missing contract facts accepted")
			}
		})
	}
}

func TestPluginV5ManifestCatalogIsolation(t *testing.T) {
	c := v5ManifestFixtures(t)[0]
	host, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-host-catalog.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	ext, err := os.ReadFile("../../sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(raw []byte, fn func(map[string]any)) []byte {
		t.Helper()
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			t.Fatal("json")
		}
		fn(v)
		out, _ := json.Marshal(v)
		return out
	}
	for name, inputs := range map[string][][]byte{
		"duplicate-capability":           {mutate(host, func(v map[string]any) { a := v["items"].([]any); v["items"] = append(a, a[0]) }), ext},
		"duplicate-extension":            {host, mutate(ext, func(v map[string]any) { a := v["points"].([]any); v["points"] = append(a, a[0]) })},
		"self-without-consent":           {mutate(host, func(v map[string]any) { v["items"].([]any)[1].(map[string]any)["consent_required"] = false }), ext},
		"unpublished-current-capability": {mutate(host, func(v map[string]any) { v["items"] = []any{} }), ext},
		"unpublished-current-extension":  {host, mutate(ext, func(v map[string]any) { v["points"] = []any{} })},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := ValidatePluginV5Manifest(c.Manifest, inputs[0], inputs[1], c.Release)
			if err == nil || m != nil {
				t.Fatal("invalid current catalog accepted")
			}
		})
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := ValidatePluginV5Manifest(c.Manifest, host, ext, c.Release)
			if err != nil {
				t.Errorf("parallel parse: %v", err)
				return
			}
			m.Provides[0].Capabilities[0] = "mutated"
			m.Configuration[0] = ' '
			m.Data.PrivateKVBytes = 9
		}()
	}
	wg.Wait()
	m, err := ValidatePluginV5Manifest(c.Manifest, host, ext, c.Release)
	if err != nil || m.Provides[0].Capabilities[0] != "text" || m.Data.PrivateKVBytes != 0 {
		t.Fatal("shared validation state")
	}
}
