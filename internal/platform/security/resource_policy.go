package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const PluginHostResourcesContract = "campusos.plugin-host-resources/v1"

var (
	ErrResourceRequestInvalid    = errors.New("plugin.resource_request_invalid")
	ErrResourcePolicyUnavailable = errors.New("plugin.resource_policy_unavailable")
	ErrResourceGrantMissing      = errors.New("plugin.resource_grant_missing")
	ErrResourceGrantExpired      = errors.New("plugin.resource_grant_expired")
	ErrNetworkTargetForbidden    = errors.New("plugin.network_target_forbidden")
)

// ResourceSet mirrors the G1 host_resources contract. Its only network
// authority is a list of exact HTTPS origins, never a raw socket permission.
type ResourceSet struct {
	NetworkTargets []string `json:"network_targets"`
	StorageBytes   int64    `json:"storage_bytes"`
	CPUMillis      int64    `json:"cpu_millis"`
	MemoryMB       int64    `json:"memory_mb"`
	MaxConcurrency int64    `json:"max_concurrency"`
	TimeoutMS      int64    `json:"timeout_ms"`
}

// ResourceDecisionInput is the published G1 DTO. DeploymentCeiling is passed
// separately to DecideResourcePolicy so an untrusted package or grant cannot
// supply the deployment safety limit.
type ResourceDecisionInput struct {
	Contract         string      `json:"contract"`
	Requested        ResourceSet `json:"requested"`
	HostCeiling      ResourceSet `json:"host_ceiling"`
	AdminGrant       ResourceSet `json:"admin_grant"`
	GrantStatus      string      `json:"grant_status"`
	GrantExpiresAtMS int64       `json:"grant_expires_at_ms"`
	NowMS            int64       `json:"now_ms"`
}

type ResourceDecision struct {
	Effective ResourceSet
}

func (d ResourceDecision) AllowsNetworkTarget(origin string) bool {
	for _, allowed := range d.Effective.NetworkTargets {
		if allowed == origin {
			return true
		}
	}
	return false
}

// ParseResourceDecisionInputJSON refuses undeclared fields, including DB,
// host filesystem, token, Docker socket, privileged runtime and shell grants.
// Resource values are validated by DecideResourcePolicy before any use.
func ParseResourceDecisionInputJSON(raw []byte) (ResourceDecisionInput, error) {
	if len(raw) == 0 || len(raw) > 32*1024 {
		return ResourceDecisionInput{}, ErrResourceRequestInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input ResourceDecisionInput
	if err := decoder.Decode(&input); err != nil {
		return ResourceDecisionInput{}, ErrResourceRequestInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ResourceDecisionInput{}, ErrResourceRequestInvalid
	}
	return input, nil
}

// DecideResourcePolicy computes package request ∩ published host ceiling ∩
// administrator grant ∩ local deployment safety ceiling. The caller must
// evaluate current grant state for each new operation; this pure decision is
// not a durable authorization token or an OS resource sandbox.
func DecideResourcePolicy(input ResourceDecisionInput, deploymentCeiling ResourceSet) (ResourceDecision, error) {
	if input.Contract != PluginHostResourcesContract || input.GrantExpiresAtMS < 1 || input.NowMS < 0 ||
		(input.GrantStatus != "granted" && input.GrantStatus != "revoked") {
		return ResourceDecision{}, ErrResourceRequestInvalid
	}
	if !validResourceNumbers(input.Requested) || !validResourceNumbers(input.HostCeiling) || !validResourceNumbers(input.AdminGrant) ||
		!validNetworkShape(input.Requested.NetworkTargets) || !validNetworkShape(input.HostCeiling.NetworkTargets) || !validNetworkShape(input.AdminGrant.NetworkTargets) {
		return ResourceDecision{}, ErrResourceRequestInvalid
	}
	if !validResourceNumbers(deploymentCeiling) || !validNetworkShape(deploymentCeiling.NetworkTargets) {
		return ResourceDecision{}, ErrResourcePolicyUnavailable
	}
	for _, target := range input.HostCeiling.NetworkTargets {
		if !validExactHTTPSOrigin(target) {
			return ResourceDecision{}, ErrResourcePolicyUnavailable
		}
	}
	for _, target := range deploymentCeiling.NetworkTargets {
		if !validExactHTTPSOrigin(target) {
			return ResourceDecision{}, ErrResourcePolicyUnavailable
		}
	}
	for _, list := range [][]string{input.Requested.NetworkTargets, input.AdminGrant.NetworkTargets} {
		for _, target := range list {
			if !validExactHTTPSOrigin(target) {
				return ResourceDecision{}, ErrNetworkTargetForbidden
			}
		}
	}
	if input.GrantStatus != "granted" {
		return ResourceDecision{}, ErrResourceGrantMissing
	}
	if input.NowMS >= input.GrantExpiresAtMS {
		return ResourceDecision{}, ErrResourceGrantExpired
	}
	requested, published, granted, deployed := input.Requested, input.HostCeiling, input.AdminGrant, deploymentCeiling
	effective := ResourceSet{
		StorageBytes:   min(requested.StorageBytes, published.StorageBytes, granted.StorageBytes, deployed.StorageBytes),
		CPUMillis:      min(requested.CPUMillis, published.CPUMillis, granted.CPUMillis, deployed.CPUMillis),
		MemoryMB:       min(requested.MemoryMB, published.MemoryMB, granted.MemoryMB, deployed.MemoryMB),
		MaxConcurrency: min(requested.MaxConcurrency, published.MaxConcurrency, granted.MaxConcurrency, deployed.MaxConcurrency),
		TimeoutMS:      min(requested.TimeoutMS, published.TimeoutMS, granted.TimeoutMS, deployed.TimeoutMS),
		NetworkTargets: make([]string, 0, len(requested.NetworkTargets)),
	}
	for _, target := range requested.NetworkTargets {
		if containsResourceTarget(published.NetworkTargets, target) && containsResourceTarget(granted.NetworkTargets, target) &&
			containsResourceTarget(deployed.NetworkTargets, target) {
			effective.NetworkTargets = append(effective.NetworkTargets, target)
		}
	}
	return ResourceDecision{Effective: effective}, nil
}

func validResourceNumbers(resources ResourceSet) bool {
	return resources.StorageBytes >= 1 && resources.StorageBytes <= 10737418240 &&
		resources.CPUMillis >= 1 && resources.CPUMillis <= 4000 &&
		resources.MemoryMB >= 1 && resources.MemoryMB <= 65536 &&
		resources.MaxConcurrency >= 1 && resources.MaxConcurrency <= 1024 &&
		resources.TimeoutMS >= 1 && resources.TimeoutMS <= 300000
}

func validNetworkShape(targets []string) bool {
	if len(targets) > 32 {
		return false
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if len(target) < 10 || len(target) > 253 {
			return false
		}
		if _, exists := seen[target]; exists {
			return false
		}
		seen[target] = struct{}{}
	}
	return true
}

func containsResourceTarget(targets []string, target string) bool {
	for _, item := range targets {
		if item == target {
			return true
		}
	}
	return false
}

// validExactHTTPSOrigin uses the same conservative target class as the G1
// offline contract. DNS pinning and connection/redirect checks belong to Egress.
func validExactHTTPSOrigin(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Opaque != "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()
	if host == "" || host != strings.ToLower(host) || net.ParseIP(host) != nil ||
		host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.Contains(host, "--") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
				return false
			}
		}
	}
	port := parsed.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || number == 443 || strconv.Itoa(number) != port || parsed.Host != host+":"+port {
			return false
		}
	} else if parsed.Host != host {
		return false
	}
	return parsed.String() == target
}
