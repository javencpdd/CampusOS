package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

type secretResourceFacts interface {
	VersionByID(context.Context, int64) (PluginVersion, error)
	ActiveVersion(context.Context, string) (PluginVersion, error)
	CurrentAdminGrant(context.Context, int64, string) (AdminGrant, error)
}

// SecretGrantResourceProvider derives the current resource intersection from
// the immutable version manifest, current administrator approval and two
// host-owned ceilings. It reads the grant for every call, so revocation or
// expiry stops new sends. The active runtime still has to enforce CPU/memory
// sandbox limits in V12-05/06; this Port only authorizes a broker operation.
type SecretGrantResourceProvider struct {
	facts      secretResourceFacts
	published  security.ResourceSet
	deployment security.ResourceSet
	now        func() time.Time
}

func NewSecretGrantResourceProvider(facts secretResourceFacts, published, deployment security.ResourceSet) (*SecretGrantResourceProvider, error) {
	if facts == nil {
		return nil, ErrSecretUseDenied
	}
	return &SecretGrantResourceProvider{
		facts: facts, published: published, deployment: deployment, now: time.Now,
	}, nil
}

func (p *SecretGrantResourceProvider) CurrentSecretResources(ctx context.Context, versionID int64,
	capabilityCode, purpose string) (security.ResourceDecision, error) {
	if p == nil || p.facts == nil || versionID <= 0 || !secretUsePurposePattern.MatchString(purpose) ||
		(capabilityCode != "secret.system.read" && capabilityCode != "secret.self.read") {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	version, err := p.facts.VersionByID(ctx, versionID)
	if err != nil || version.ID != versionID || version.LifecycleStatus != "active" {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	active, err := p.facts.ActiveVersion(ctx, version.PluginName)
	if err != nil || active.ID != versionID || active.LifecycleStatus != "active" {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	requested, err := decodeHostResourceSet(version.Manifest["host_resources"])
	if err != nil {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	grant, err := p.facts.CurrentAdminGrant(ctx, versionID, capabilityCode)
	if err != nil {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	granted, err := decodeHostResourceSet(grant.GrantedScope["host_resources"])
	if err != nil {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	expiresAtMS := int64(math.MaxInt64)
	if grant.ExpiresAt != nil {
		expiresAtMS = grant.ExpiresAt.UnixMilli()
	}
	decision, err := security.DecideResourcePolicy(security.ResourceDecisionInput{
		Contract: security.PluginHostResourcesContract, Requested: requested,
		HostCeiling: p.published, AdminGrant: granted, GrantStatus: grant.Status,
		GrantExpiresAtMS: expiresAtMS, NowMS: p.now().UnixMilli(),
	}, p.deployment)
	if err != nil {
		return security.ResourceDecision{}, ErrSecretUseDenied
	}
	return decision, nil
}

func decodeHostResourceSet(raw interface{}) (security.ResourceSet, error) {
	if raw == nil {
		return security.ResourceSet{}, ErrSecretUseDenied
	}
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > 32*1024 {
		return security.ResourceSet{}, ErrSecretUseDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var value security.ResourceSet
	if err := decoder.Decode(&value); err != nil {
		return security.ResourceSet{}, ErrSecretUseDenied
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return security.ResourceSet{}, ErrSecretUseDenied
	}
	return value, nil
}
