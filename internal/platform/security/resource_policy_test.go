package security

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func resourcePolicyFixture() (ResourceDecisionInput, ResourceSet) {
	requested := ResourceSet{
		NetworkTargets: []string{"https://api.example.test", "https://models.example.test"},
		StorageBytes:   1000, CPUMillis: 500, MemoryMB: 512, MaxConcurrency: 8, TimeoutMS: 10000,
	}
	published := requested
	published.NetworkTargets = append([]string(nil), requested.NetworkTargets...)
	published.StorageBytes = 800
	published.CPUMillis = 400
	published.MaxConcurrency = 4
	grant := requested
	grant.NetworkTargets = []string{"https://api.example.test"}
	grant.StorageBytes = 600
	grant.MemoryMB = 256
	deployed := requested
	deployed.NetworkTargets = append([]string(nil), requested.NetworkTargets...)
	deployed.CPUMillis = 300
	deployed.TimeoutMS = 5000
	return ResourceDecisionInput{
		Contract: PluginHostResourcesContract, Requested: requested, HostCeiling: published,
		AdminGrant: grant, GrantStatus: "granted", GrantExpiresAtMS: 2000, NowMS: 1000,
	}, deployed
}

func TestResourcePolicyFourWayIntersection(t *testing.T) {
	input, deployed := resourcePolicyFixture()
	before, _ := json.Marshal(input)
	decision, err := DecideResourcePolicy(input, deployed)
	if err != nil {
		t.Fatal(err)
	}
	want := ResourceSet{
		NetworkTargets: []string{"https://api.example.test"},
		StorageBytes:   600, CPUMillis: 300, MemoryMB: 256, MaxConcurrency: 4, TimeoutMS: 5000,
	}
	if !reflect.DeepEqual(decision.Effective, want) {
		t.Fatalf("four-way resource limit mismatch: %+v", decision.Effective)
	}
	if !decision.AllowsNetworkTarget("https://api.example.test") || decision.AllowsNetworkTarget("https://models.example.test") ||
		decision.AllowsNetworkTarget("https://api.example.test.evil") {
		t.Fatal("network decision did not enforce exact origin")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("decision mutated its input")
	}
	input.AdminGrant.NetworkTargets = nil
	decision, err = DecideResourcePolicy(input, deployed)
	if err != nil || len(decision.Effective.NetworkTargets) != 0 {
		t.Fatalf("empty network intersection was not denied: decision=%+v err=%v", decision, err)
	}
}

func TestResourcePolicyRevocationAndExpiry(t *testing.T) {
	input, deployed := resourcePolicyFixture()
	input.GrantStatus = "revoked"
	if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourceGrantMissing) {
		t.Fatalf("revoked grant returned %v", err)
	}
	input.GrantStatus = "granted"
	input.NowMS = input.GrantExpiresAtMS
	if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourceGrantExpired) {
		t.Fatalf("expired grant returned %v", err)
	}
	input.NowMS--
	if _, err := DecideResourcePolicy(input, deployed); err != nil {
		t.Fatalf("valid pre-expiry grant rejected: %v", err)
	}
}

func TestResourcePolicyRejectsUnsafeNetworkOrigins(t *testing.T) {
	for _, target := range []string{
		"http://api.example.test", "https://*.example.test", "https://127.0.0.1", "https://[::1]",
		"https://localhost", "https://host.localhost", "https://u:p@api.example.test",
		"https://api.example.test/private", "https://api.example.test/", "https://api.example.test?x=1",
		"https://api.example.test#frag", "https://API.example.test", "https://api.example.test:443",
		"https://api.example.test:0", "https://api.example.test:65536", "https://api--prod.example.test",
		"https://api_example.test", "https://api.example.test.",
	} {
		t.Run(target, func(t *testing.T) {
			input, deployed := resourcePolicyFixture()
			input.Requested.NetworkTargets = []string{target}
			if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrNetworkTargetForbidden) && !errors.Is(err, ErrResourceRequestInvalid) {
				t.Fatalf("unsafe requested origin %q was accepted: %v", target, err)
			}
			input, deployed = resourcePolicyFixture()
			deployed.NetworkTargets = []string{target}
			if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourcePolicyUnavailable) {
				t.Fatalf("unsafe deployment origin %q was accepted: %v", target, err)
			}
		})
	}
	input, deployed := resourcePolicyFixture()
	input.Requested.NetworkTargets = []string{"https://api.example.test:8443"}
	input.HostCeiling.NetworkTargets = input.Requested.NetworkTargets
	input.AdminGrant.NetworkTargets = input.Requested.NetworkTargets
	deployed.NetworkTargets = input.Requested.NetworkTargets
	if _, err := DecideResourcePolicy(input, deployed); err != nil {
		t.Fatalf("explicit safe HTTPS port was rejected: %v", err)
	}
}

func TestResourcePolicyRejectsUnknownPrivilegesAndInvalidBounds(t *testing.T) {
	input, deployed := resourcePolicyFixture()
	raw, _ := json.Marshal(input)
	parsed, err := ParseResourceDecisionInputJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecideResourcePolicy(parsed, deployed); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"database_url", "host_path", "docker_socket", "privileged", "shell", "jwt_secret", "token"} {
		unsafe := strings.Replace(string(raw), `"storage_bytes":1000`, `"storage_bytes":1000,"`+field+`":"forbidden"`, 1)
		if _, err := ParseResourceDecisionInputJSON([]byte(unsafe)); !errors.Is(err, ErrResourceRequestInvalid) {
			t.Fatalf("unknown dangerous field %q was accepted: %v", field, err)
		}
	}
	input.Requested.MemoryMB = 0
	if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourceRequestInvalid) {
		t.Fatalf("zero memory accepted: %v", err)
	}
	input, deployed = resourcePolicyFixture()
	deployed.CPUMillis = 4001
	if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourcePolicyUnavailable) {
		t.Fatalf("invalid deployment ceiling accepted: %v", err)
	}
	input, deployed = resourcePolicyFixture()
	input.Requested.NetworkTargets = []string{"https://api.example.test", "https://api.example.test"}
	if _, err := DecideResourcePolicy(input, deployed); !errors.Is(err, ErrResourceRequestInvalid) {
		t.Fatalf("duplicate requested origin accepted: %v", err)
	}
}
