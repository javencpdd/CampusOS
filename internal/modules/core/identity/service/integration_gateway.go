package service

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

type integrationBinding struct {
	Principal  repository.IntegrationPrincipalRecord
	Credential repository.IntegrationCredentialRecord
}

func integrationBindingError(err error) error {
	if errors.Is(err, repository.ErrIntegrationNotFound) {
		return ErrIntegrationCredentialInvalid
	}
	return ErrIntegrationUnavailable
}

func integrationTokenID(raw string) (string, bool) {
	if len(raw) != 66 || !strings.HasPrefix(raw, "cint1_") || raw[22] != '.' {
		return "", false
	}
	id := raw[6:22]
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 8 || hex.EncodeToString(decoded) != id {
		return "", false
	}
	secret, err := base64.RawURLEncoding.DecodeString(raw[23:])
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != raw[23:] {
		return "", false
	}
	return id, true
}
func (s *IntegrationService) binding(ctx context.Context, raw string, lock bool) (integrationBinding, error) {
	if s == nil || nilGovernanceAdapter(s.repo) || s.reliable == nil || s.now == nil {
		return integrationBinding{}, ErrIntegrationUnavailable
	}
	id, ok := integrationTokenID(raw)
	if !ok {
		return integrationBinding{}, ErrIntegrationCredentialInvalid
	}
	// The first lookup locates the owner only. Every command/read locks in the
	// same principal -> credential order and re-reads the actual credential.
	first, err := s.repo.GetIntegrationCredential(ctx, id, false)
	if err != nil {
		return integrationBinding{}, integrationBindingError(err)
	}
	p, err := s.repo.GetIntegrationPrincipal(ctx, first.PrincipalID, lock)
	if err != nil {
		return integrationBinding{}, integrationBindingError(err)
	}
	c, err := s.repo.GetIntegrationCredential(ctx, id, lock)
	if err != nil {
		return integrationBinding{}, integrationBindingError(err)
	}
	digest := integrationDigest(raw)
	now := s.now().UnixMilli()
	if c.PrincipalID != p.ID || c.ID != id || p.Status != "active" || c.Status != "active" || c.Purpose != "integration_api" || now < c.NotBeforeMS || now >= c.ExpiresAtMS || subtle.ConstantTimeCompare(digest[:], c.Digest[:]) != 1 {
		return integrationBinding{}, ErrIntegrationCredentialInvalid
	}
	return integrationBinding{Principal: p, Credential: c}, nil
}
func integrationPrincipal(b integrationBinding) port.PrincipalContext {
	return port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "integration", ID: b.Principal.ID}, Audience: "integration_api", AuthenticationStrength: "credential", CredentialID: b.Credential.ID}
}
func (s *IntegrationService) input(b integrationBinding, t port.IntegrationAccessTarget, e port.IntegrationAccessEndpoint, requestID string) port.IntegrationAccessInput {
	c, p := b.Credential, b.Principal
	return port.IntegrationAccessInput{Contract: port.IntegrationRuntimeContract, RequestID: requestID, Principal: integrationPrincipal(b), Target: t, Facts: port.IntegrationAccessFacts{IntegrationID: p.ID, CredentialID: c.ID, PrincipalStatus: p.Status, CredentialStatus: c.Status, CredentialAudience: c.Purpose, NotBeforeMS: c.NotBeforeMS, ExpiresAtMS: c.ExpiresAtMS, FactsVersion: "p" + strconv.FormatInt(p.Version, 10) + ".c" + strconv.FormatInt(c.Version, 10), Endpoint: e, Rules: repository.CloneIntegrationCredential(c).Rules}, NowMS: s.now().UnixMilli()}
}
func (s *IntegrationService) ReadForIntegration(ctx context.Context, raw string, target port.IntegrationAccessTarget, read func(context.Context) (any, error)) (any, error) {
	endpoint, ok := integrationEndpoint(target)
	if !ok || read == nil {
		return nil, ErrIntegrationDenied
	}
	before, err := s.binding(ctx, raw, false)
	if err != nil {
		return nil, err
	}
	requestID, err := integrationID("ir_", 16)
	if err != nil {
		return nil, ErrIntegrationUnavailable
	}
	engine := policy.IntegrationRuntimePolicy{}
	original := s.input(before, target, endpoint, requestID)
	decision, err := engine.DecideIntegrationAccess(original)
	if err != nil {
		return nil, err
	}
	if err = engine.CheckIntegrationAccessDecision(original, decision, original); err != nil {
		return nil, err
	}
	var data any
	event, err := reliability.NewEvent("integration.accessed", target.Resource.Kind, target.Resource.ID, map[string]any{"principal_id": before.Principal.ID, "credential_id": before.Credential.ID, "channel": target.Channel, "endpoint": target.Endpoint, "action": target.Action})
	if err != nil {
		return nil, err
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: "identity.integration.read", ActorType: "integration", ActorID: before.Principal.ID, ResourceType: target.Resource.Kind, ResourceID: target.Resource.ID, OperationCode: "identity.integration.read", RequestID: requestID, Event: &event}, func(tx context.Context) error {
		current, err := s.binding(tx, raw, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, current) {
			return ErrIntegrationChanged
		}
		input := s.input(current, target, endpoint, requestID)
		if err = engine.CheckIntegrationAccessDecision(original, decision, input); err != nil {
			return err
		}
		data, err = read(tx)
		if err != nil {
			data = nil
			return err
		}
		// The resource owner has checked its source row. Recheck the complete
		// machine witness after that I/O and before audit/outbox/commit or bytes.
		fresh, err := s.binding(tx, raw, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, fresh) {
			return ErrIntegrationChanged
		}
		freshEndpoint, ok := integrationEndpoint(target)
		if !ok {
			return ErrIntegrationDenied
		}
		if err = engine.CheckIntegrationAccessDecision(original, decision, s.input(fresh, target, freshEndpoint, requestID)); err != nil {
			return err
		}
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Self identifies only this currently authenticated machine. It grants no
// resource authority and never returns the secret, digest or Admin identity.
type IntegrationSelf struct {
	Principal   port.PrincipalContext `json:"principal"`
	Name        string                `json:"name"`
	Prefix      string                `json:"prefix"`
	ExpiresAtMS int64                 `json:"expires_at_ms"`
}

func (s *IntegrationService) CurrentIntegration(ctx context.Context, raw string) (IntegrationSelf, error) {
	before, err := s.binding(ctx, raw, false)
	if err != nil {
		return IntegrationSelf{}, err
	}
	var out IntegrationSelf
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		current, err := s.binding(tx, raw, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, current) {
			return ErrIntegrationChanged
		}
		out = IntegrationSelf{Principal: integrationPrincipal(current), Name: current.Principal.Name, Prefix: current.Credential.Prefix, ExpiresAtMS: current.Credential.ExpiresAtMS}
		return tx.Err()
	})
	if err != nil {
		return IntegrationSelf{}, err
	}
	return out, nil
}
