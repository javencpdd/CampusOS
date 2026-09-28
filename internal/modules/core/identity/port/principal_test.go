package port

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPrincipalG1Fixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../../../sdk/typescript/tests/principal-context-v1.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Contract string `json:"contract"`
		Cases    []struct {
			Name      string          `json:"name"`
			Valid     bool            `json:"valid"`
			Context   json.RawMessage `json:"context"`
			ErrorCode string          `json:"error_code"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Contract != PrincipalContract || len(corpus.Cases) != 63 {
		t.Fatal("unexpected frozen Principal corpus")
	}
	for _, item := range corpus.Cases {
		t.Run(item.Name, func(t *testing.T) {
			before := bytes.Clone(item.Context)
			p, err := ParsePrincipalJSON(item.Context)
			if !bytes.Equal(before, item.Context) {
				t.Fatal("parser changed input bytes")
			}
			if !item.Valid {
				if err == nil || err.Error() != item.ErrorCode || !reflect.DeepEqual(p, PrincipalContext{}) {
					t.Fatalf("rejection = %#v, %v; want zero and %s", p, err, item.ErrorCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Validate(); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			var original, roundtrip any
			if err := json.Unmarshal(item.Context, &original); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &roundtrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, roundtrip) {
				t.Fatal("wire shape changed on roundtrip")
			}
		})
	}
}

func TestPrincipalDomainMatrix(t *testing.T) {
	type combination struct {
		kind, audience string
		strengths      []string
	}
	allowed := []combination{
		{"anonymous", "public", []string{"none"}},
		{"user", "user", []string{"password", "mfa"}},
		{"admin", "admin", []string{"password", "mfa"}},
		{"integration", "integration_api", []string{"credential"}},
		{"plugin_instance", "host_api", []string{"workload"}},
		{"worker", "worker", []string{"workload"}},
	}
	count := 0
	for _, domain := range allowed {
		for _, audience := range []string{"public", "user", "admin", "integration_api", "host_api", "worker"} {
			for _, strength := range []string{"none", "password", "mfa", "credential", "workload"} {
				t.Run(domain.kind+"/"+audience+"/"+strength, func(t *testing.T) {
					p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: domain.kind}, Audience: audience, AuthenticationStrength: strength}
					if domain.kind != "anonymous" {
						p.Actor.ID = "same-42"
						p.CredentialID = "credential-42"
					}
					want := false
					for _, accepted := range domain.strengths {
						want = want || (audience == domain.audience && strength == accepted)
					}
					if got := p.Validate() == nil; got != want {
						t.Fatalf("Validate allowed=%v; want %v", got, want)
					}
					raw, err := json.Marshal(p)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := ParsePrincipalJSON(raw); (err == nil) != want {
						t.Fatalf("Parse error=%v; want allowed=%v", err, want)
					}
				})
				count++
			}
		}
	}
	if count != 180 {
		t.Fatalf("matrix ran %d cases", count)
	}
	if (PrincipalRef{Kind: "user", ID: "same-42"}) == (PrincipalRef{Kind: "admin", ID: "same-42"}) {
		t.Fatal("actor domains merged")
	}
}

func TestPrincipalStrictJSON(t *testing.T) {
	base := `{"contract":"campusos.principal/v1","actor":{"kind":"anonymous"},"audience":"public","authentication_strength":"none"}`
	invalid := map[string][]byte{
		"empty":                         nil,
		"duplicate-contract":            []byte(strings.Replace(base, `"contract":`, `"contract":"campusos.principal/v1","contract":`, 1)),
		"escaped-duplicate":             []byte(strings.Replace(base, `"contract":`, `"contr\u0061ct":"campusos.principal/v1","contract":`, 1)),
		"duplicate-nested-kind":         []byte(strings.Replace(base, `"kind":"anonymous"`, `"kind":"anonymous","kind":"anonymous"`, 1)),
		"trailing-object":               []byte(base + ` {}`),
		"trailing-scalar":               []byte(base + ` true`),
		"trailing-invalid":              []byte(base + ` !`),
		"uppercase-root":                []byte(strings.Replace(base, `"actor"`, `"Actor"`, 1)),
		"uppercase-nested":              []byte(strings.Replace(base, `"kind"`, `"Kind"`, 1)),
		"null-contract":                 []byte(strings.Replace(base, `"campusos.principal/v1"`, `null`, 1)),
		"null-actor":                    []byte(strings.Replace(base, `{"kind":"anonymous"}`, `null`, 1)),
		"anonymous-explicit-empty-id":   []byte(strings.Replace(base, `"kind":"anonymous"`, `"kind":"anonymous","id":""`, 1)),
		"anonymous-explicit-credential": []byte(strings.Replace(base, `"actor":`, `"credential_id":"","actor":`, 1)),
		"anonymous-null-credential":     []byte(strings.Replace(base, `"actor":`, `"credential_id":null,"actor":`, 1)),
		"anonymous-empty-delegation":    []byte(strings.Replace(base, `"actor":`, `"delegation_id":"","actor":`, 1)),
		"oversized":                     []byte(base + strings.Repeat(" ", policyJSONLimit-len(base)+1)),
		"invalid-utf8":                  append(append([]byte{}, []byte(base[:len(base)-1])...), 0xff, '}'),
		"sensitive-key":                 []byte(strings.Replace(base, `"actor":`, `"private-sensitive-token-42":"secret","actor":`, 1)),
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			p, err := ParsePrincipalJSON(raw)
			if !errors.Is(err, ErrPrincipalInvalid) || !reflect.DeepEqual(p, PrincipalContext{}) {
				t.Fatalf("got %#v, %v", p, err)
			}
			if err.Error() != "principal.context_invalid" {
				t.Fatal("error leaked input")
			}
		})
	}
	for _, raw := range []string{base, "\n\t" + base + "\r\n", base + strings.Repeat(" ", policyJSONLimit-len(base))} {
		if _, err := ParsePrincipalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrincipalContractErrorPriority(t *testing.T) {
	for _, raw := range []string{
		`{"contract":"future-sensitive-version","extra":null}`,
		`{"contract":""}`,
		`{"contract":"future-sensitive-version","actor":null}`,
		`{"contract":"future-sensitive-version","extra":[{"a":1},{"a":1e999}]}`,
	} {
		_, err := ParsePrincipalJSON([]byte(raw))
		if !errors.Is(err, ErrPrincipalContractUnsupported) || err.Error() != "principal.contract_unsupported" {
			t.Fatalf("got %v", err)
		}
	}
	for _, raw := range []string{
		`{"contract":"future","contract":"future"}`,
		`{"contract":"future","actor":{"kind":"user","kind":"admin"}}`,
		`{"contract":"future","extra":[{"secret":1,"secret":2}]}`,
		`{"contract":"future","extra":{"child":{"key":1,"k\u0065y":2}}}`,
		`{"contract":"future","extra":` + strings.Repeat("[", policyJSONMaxDepth+1) + "0" + strings.Repeat("]", policyJSONMaxDepth+1) + "}",
		`{"contract":"future"} {}`,
		`{"contract":null}`, `{"contract":42}`, `{"contract":true}`, `{}`,
	} {
		if _, err := ParsePrincipalJSON([]byte(raw)); !errors.Is(err, ErrPrincipalInvalid) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestPrincipalOpaqueIDAndDelegation(t *testing.T) {
	valid := []string{"a", "0", "Z", "a._:-09AZaz", strings.Repeat("a", 128)}
	invalid := []string{"", ".a", "_a", ":a", "-a", "a/b", "a*b", " a", "a ", "a\n", "é", "中", "a\x00", strings.Repeat("a", 129)}
	for _, id := range valid {
		if !validOpaqueID(id) {
			t.Fatalf("valid ID rejected %q", id)
		}
	}
	for _, id := range invalid {
		if validOpaqueID(id) {
			t.Fatalf("invalid ID accepted %q", id)
		}
	}
	base := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "worker", ID: "worker-1"}, Audience: "worker", AuthenticationStrength: "workload", CredentialID: "credential-1", EffectiveSubject: &PrincipalRef{Kind: "user", ID: "user-1"}, DelegationID: "delegation-1"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []func(*PrincipalContext){
		func(p *PrincipalContext) { p.EffectiveSubject = nil },
		func(p *PrincipalContext) { p.DelegationID = "" },
		func(p *PrincipalContext) { p.EffectiveSubject = &PrincipalRef{Kind: "admin", ID: "user-1"} },
		func(p *PrincipalContext) { p.EffectiveSubject = &PrincipalRef{Kind: "user", ID: ""} },
		func(p *PrincipalContext) {
			p.Actor.Kind = "user"
			p.Audience = "user"
			p.AuthenticationStrength = "mfa"
		},
		func(p *PrincipalContext) {
			p.Actor.Kind = "admin"
			p.Audience = "admin"
			p.AuthenticationStrength = "mfa"
		},
		func(p *PrincipalContext) {
			p.Actor.Kind = "integration"
			p.Audience = "integration_api"
			p.AuthenticationStrength = "credential"
		},
	}
	for i, mutate := range cases {
		p := base
		mutate(&p)
		if !errors.Is(p.Validate(), ErrPrincipalInvalid) {
			t.Fatalf("invalid delegation %d accepted", i)
		}
	}
	wire, _ := json.Marshal(base)
	for name, raw := range map[string][]byte{
		"duplicate-subject": bytes.Replace(wire, []byte(`"kind":"user"`), []byte(`"kind":"user","kind":"user"`), 1),
		"subject-null":      bytes.Replace(wire, []byte(`{"kind":"user","id":"user-1"}`), []byte(`null`), 1),
		"subject-extra":     bytes.Replace(wire, []byte(`"kind":"user"`), []byte(`"kind":"user","role":"admin"`), 1),
		"delegation-empty":  bytes.Replace(wire, []byte(`"delegation-1"`), []byte(`""`), 1),
		"delegation-null":   bytes.Replace(wire, []byte(`"delegation-1"`), []byte(`null`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePrincipalJSON(raw); !errors.Is(err, ErrPrincipalInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
