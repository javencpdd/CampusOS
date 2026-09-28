package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func testPluginV5Manifest() []byte {
	return []byte(`{"api_version":"campusos.plugin/v5","configuration":{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"locale":{"type":"string","title":"语言","enum":["zh-CN","en-US"]},"profile_id":{"type":"string","title":"Profile","maxLength":128},"max_results":{"type":"integer","title":"数量","minimum":1,"maximum":50}},"required":["locale","profile_id","max_results"]},"defaults":{"locale":"zh-CN","profile_id":"public","max_results":10},"selectors":[{"field":"profile_id","kind":"profile"}],"secret_names":["token","backup"]}}`)
}

func TestPluginV5ConfigContractRejectsUnsupportedSchemaAndDefaults(t *testing.T) {
	manifest := testPluginV5Manifest()
	definition, err := parsePluginV5ConfigDefinition(manifest)
	if err != nil || definition.Version != "v1" {
		t.Fatalf("valid definition rejected: %+v %v", definition, err)
	}
	cases := []struct {
		name string
		old  string
		new  string
		want error
	}{
		{"remote-ref", `"title":"语言"`, `"title":"语言","$ref":"https://example.invalid"`, ErrPluginV5ConfigSchemaUnsupported},
		{"script-keyword", `"title":"语言"`, `"title":"语言","x-script":"eval"`, ErrPluginV5ConfigSchemaUnsupported},
		{"secret-field-overlap", `"secret_names":["token","backup"]`, `"secret_names":["locale"]`, ErrPluginV5ConfigSchemaUnsupported},
		{"selector-numeric", `"field":"profile_id"`, `"field":"max_results"`, ErrPluginV5ConfigSchemaUnsupported},
		{"default-out-of-range", `"max_results":10}`, `"max_results":99}`, ErrPluginV5ConfigDefaultsInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := []byte(string(manifest))
			changed = []byte(replaceOnce(string(changed), tc.old, tc.new))
			_, got := parsePluginV5ConfigDefinition(changed)
			if !errors.Is(got, tc.want) {
				t.Fatalf("error = %v, want %v", got, tc.want)
			}
		})
	}
}

func replaceOnce(input, old, new string) string {
	for index := 0; index+len(old) <= len(input); index++ {
		if input[index:index+len(old)] == old {
			return input[:index] + new + input[index+len(old):]
		}
	}
	return input
}

func TestPluginV5ConfigValuesRefsAndSelectors(t *testing.T) {
	definition, err := parsePluginV5ConfigDefinition(testPluginV5Manifest())
	if err != nil {
		t.Fatal(err)
	}
	valid := json.RawMessage(`{"locale":"en-US","profile_id":"public","max_results":20}`)
	if err := validatePluginV5Values(definition, valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"locale":"en-US","profile_id":"public","max_results":99}`,
		`{"locale":"en-US","profile_id":"public","max_results":20,"token":"plain"}`,
		`{"locale":"en-US","profile_id":"public","max_results":20,"other":true}`,
		`{"locale":"en-US","profile_id":"public","max_results":[]}`,
		`{"locale":"en-US","profile_id":"public"}`,
	} {
		if err := validatePluginV5Values(definition, []byte(raw)); !errors.Is(err, ErrPluginV5ConfigInvalid) {
			t.Fatalf("accepted invalid values %s: %v", raw, err)
		}
	}
	if err := validatePluginV5SecretRefs(definition, map[string]string{"token": "secret-ref:approved-1", "backup": "secret-ref:approved-2"}); err != nil {
		t.Fatal(err)
	}
	for _, refs := range []map[string]string{
		{"token": "plain-secret"}, {"token": "********"},
		{"token": "secret-ref:approved-1", "backup": "secret-ref:approved-1"},
		{"unknown": "secret-ref:approved-1"},
	} {
		if err := validatePluginV5SecretRefs(definition, refs); !errors.Is(err, ErrPluginV5SecretRefInvalid) {
			t.Fatalf("accepted invalid refs %+v: %v", refs, err)
		}
	}
	if err := validatePluginV5Selectors(context.Background(), definition, valid, 9, nil, nil); !errors.Is(err, ErrPluginV5ConfigSelectorForbidden) {
		t.Fatalf("missing host selector verifier accepted: %v", err)
	}
	allow := func(_ context.Context, _ int64, _ *int64, kind, value string) (bool, error) {
		return kind == "profile" && value == "public", nil
	}
	if err := validatePluginV5Selectors(context.Background(), definition, valid, 9, nil, allow); err != nil {
		t.Fatal(err)
	}
	deny := func(_ context.Context, _ int64, _ *int64, _, _ string) (bool, error) { return false, nil }
	if err := validatePluginV5Selectors(context.Background(), definition, valid, 9, nil, deny); !errors.Is(err, ErrPluginV5ConfigSelectorForbidden) {
		t.Fatalf("removed profile accepted: %v", err)
	}
}

func TestPluginV5ConfigExactNumbers(t *testing.T) {
	integer := pluginV5ConfigField{Type: "integer", Title: "整数"}
	for _, literal := range []string{"9007199254740993", "1.0", "1e3"} {
		if !pluginV5ValueMatchesField(integer, json.Number(literal), true) {
			t.Fatalf("exact integer %s rejected", literal)
		}
	}
	for _, literal := range []string{"9007199254740992.1", "1e-1"} {
		if pluginV5ValueMatchesField(integer, json.Number(literal), true) {
			t.Fatalf("fraction %s rounded into integer", literal)
		}
	}
	minimum, maximum := json.Number("9007199254740992.1"), json.Number("9007199254740992.3")
	ranged := pluginV5ConfigField{Type: "number", Title: "精确范围", Minimum: &minimum, Maximum: &maximum}
	if !validPluginV5ConfigField(ranged) || !pluginV5ValueMatchesField(ranged, json.Number("9007199254740992.2"), true) {
		t.Fatal("precise value inside range rejected")
	}
	for _, literal := range []string{"9007199254740992.0", "9007199254740992.4"} {
		if pluginV5ValueMatchesField(ranged, json.Number(literal), true) {
			t.Fatalf("out of range %s accepted after rounding", literal)
		}
	}
	reversed := ranged
	reversed.Minimum, reversed.Maximum = &maximum, &minimum
	if validPluginV5ConfigField(reversed) {
		t.Fatal("reversed precise bounds accepted")
	}
	enumerated := pluginV5ConfigField{Type: "number", Title: "枚举", Enum: []json.RawMessage{json.RawMessage("1")}}
	for _, literal := range []string{"1", "1.0", "1e0", "0.1e1"} {
		if !pluginV5ValueMatchesField(enumerated, json.Number(literal), true) {
			t.Fatalf("equal numeric enum representation %s rejected", literal)
		}
	}
	for _, duplicated := range [][]json.RawMessage{
		{json.RawMessage("1"), json.RawMessage("1.0")},
		{json.RawMessage("1e0"), json.RawMessage("0.1e1")},
	} {
		enumerated.Enum = duplicated
		if validPluginV5ConfigField(enumerated) {
			t.Fatalf("duplicate numeric enum accepted: %s", duplicated)
		}
	}
	enumerated.Enum = []json.RawMessage{json.RawMessage("9007199254740993")}
	if !pluginV5ValueMatchesField(enumerated, json.Number("9007199254740993.0"), true) ||
		pluginV5ValueMatchesField(enumerated, json.Number("9007199254740992"), true) {
		t.Fatal("distinct large integer enum values collapsed")
	}
	if _, valid := exactPluginV5Number(json.Number("1e1000000000")); valid {
		t.Fatal("unbounded numeric exponent accepted")
	}
}
