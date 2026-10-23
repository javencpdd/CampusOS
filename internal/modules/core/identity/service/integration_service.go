package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/identity/integrationmodel"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrIntegrationInvalid           = port.ErrIntegrationInputInvalid
	ErrIntegrationDenied            = port.ErrIntegrationDenied
	ErrIntegrationMFARequired       = port.ErrIntegrationMFARequired
	ErrIntegrationUnavailable       = port.ErrIntegrationUnavailable
	ErrIntegrationChanged           = port.ErrIntegrationChanged
	ErrIntegrationCredentialInvalid = port.ErrIntegrationCredentialInvalid
)

type IntegrationService struct {
	repo     repository.IntegrationRepository
	admins   port.AdminExecutionAuthorityReader
	reliable *reliability.Service
	now      func() time.Time
}

func NewIntegrationService(repo repository.IntegrationRepository, admins port.AdminExecutionAuthorityReader, reliable *reliability.Service) *IntegrationService {
	s := &IntegrationService{repo: repo, admins: admins, reliable: reliable, now: time.Now}
	if reliable != nil && !nilGovernanceAdapter(repo) {
		if snapshot, ok := repo.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
	return s
}

// These endpoints are compiled host inventory. Client-supplied names cannot
// register an endpoint or opt an existing Admin MCP tool into machine access.
func IntegrationEndpoints() []port.IntegrationAccessEndpoint {
	return []port.IntegrationAccessEndpoint{
		{Channel: "integration_http", ID: "public_thread.read", Action: "community.thread.read", ResourceKind: "community.thread", Enabled: true, IntegrationAllowed: true},
		{Channel: "integration_mcp", ID: "public_thread_read", Action: "community.thread.read", ResourceKind: "community.thread", Enabled: true, IntegrationAllowed: true},
		{Channel: "integration_http", ID: "knowledge.source.read", Action: "knowledge.source.read", ResourceKind: "public_collection", Enabled: true, IntegrationAllowed: true},
		{Channel: "integration_http", ID: "knowledge.search", Action: "knowledge.source.read", ResourceKind: "public_collection", Enabled: true, IntegrationAllowed: true},
		{Channel: "integration_mcp", ID: "knowledge.public.read", Action: "knowledge.source.read", ResourceKind: "public_collection", Enabled: true, IntegrationAllowed: true},
	}
}
func integrationEndpoint(t port.IntegrationAccessTarget) (port.IntegrationAccessEndpoint, bool) {
	for _, e := range IntegrationEndpoints() {
		if e.Channel == t.Channel && e.ID == t.Endpoint && e.Action == t.Action && e.ResourceKind == t.Resource.Kind {
			return e, true
		}
	}
	return port.IntegrationAccessEndpoint{}, false
}
func integrationID(prefix string, n int) (string, error) {
	v := make([]byte, n)
	if _, err := rand.Read(v); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(v), nil
}
func integrationDigest(raw string) [32]byte {
	return sha256.Sum256([]byte("campusos.integration.credential/v1\x00" + raw))
}
func validIntegrationName(name string) bool {
	return utf8.ValidString(name) && len(name) > 0 && len(name) <= 120 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\x00\r\n")
}
func (s *IntegrationService) available() bool {
	return s != nil && !nilGovernanceAdapter(s.repo) && !nilGovernanceAdapter(s.admins) && s.reliable != nil && s.now != nil
}
func integrationAdminQuery(code string) port.AdminExecutionQuery {
	return port.AdminExecutionQuery{PermissionCode: code, Scope: port.PolicyResourceRef{Kind: "identity.integration_inventory", ID: "all"}}
}
func (s *IntegrationService) admin(ctx context.Context, code string, resolve port.SessionPrincipalResolver, lock bool) (port.AdminExecutionAuthority, error) {
	if !s.available() || resolve == nil {
		return port.AdminExecutionAuthority{}, ErrIntegrationUnavailable
	}
	a, err := s.admins.LoadAdminExecutionAuthority(ctx, integrationAdminQuery(code), resolve, lock)
	if err != nil {
		return a, err
	}
	if port.CheckAdminExecutionAuthority(a, a) != nil || a.Query != integrationAdminQuery(code) {
		return a, ErrIntegrationDenied
	}
	if !a.Allowed || s.now().UnixMilli() >= a.ExpiresAtMS {
		if code == "identity.integration.manage" && (a.Principal.AuthenticationStrength != "mfa" || s.now().UnixMilli() >= a.ExpiresAtMS) {
			return a, ErrIntegrationMFARequired
		}
		return a, ErrIntegrationDenied
	}
	return a, nil
}
func (s *IntegrationService) managed(ctx context.Context, operation, id string, resolve port.SessionPrincipalResolver, action func(context.Context, string) (any, error)) error {
	a, err := s.admin(ctx, "identity.integration.manage", resolve, false)
	if err != nil {
		return err
	}
	var payload any
	return s.reliable.Execute(ctx, reliability.Command{Code: "identity.integration." + operation, ActorType: "admin", ActorID: a.Principal.Actor.ID, ResourceType: "integration_principal", ResourceID: id, PermissionCode: "identity.integration.manage", OperationCode: "identity.integration." + operation, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("authorization.changed", "integration_principal", id, payload)
	}}, func(tx context.Context) error {
		current, err := s.admin(tx, "identity.integration.manage", resolve, true)
		if err != nil {
			return err
		}
		if err = port.CheckAdminExecutionAuthority(a, current); err != nil {
			return err
		}
		payload, err = action(tx, a.Principal.Actor.ID)
		if err != nil {
			return err
		}
		current, err = s.admin(tx, "identity.integration.manage", resolve, true)
		if err != nil {
			return err
		}
		if err = port.CheckAdminExecutionAuthority(a, current); err != nil {
			return err
		}
		return tx.Err()
	})
}
func (s *IntegrationService) CreatePrincipal(ctx context.Context, name string, resolve port.SessionPrincipalResolver) (repository.IntegrationPrincipalRecord, error) {
	if !validIntegrationName(name) {
		return repository.IntegrationPrincipalRecord{}, ErrIntegrationInvalid
	}
	id, err := integrationID("ip_", 16)
	if err != nil {
		return repository.IntegrationPrincipalRecord{}, ErrIntegrationUnavailable
	}
	p := repository.IntegrationPrincipalRecord{ID: id, Name: name, Status: "active"}
	err = s.managed(ctx, "create", id, resolve, func(tx context.Context, admin string) (any, error) {
		p.CreatedByAdminID = admin
		if err := s.repo.CreateIntegrationPrincipal(tx, &p); err != nil {
			return nil, err
		}
		stored, err := s.repo.GetIntegrationPrincipal(tx, id, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(stored, p) {
			return nil, ErrIntegrationChanged
		}
		return p, nil
	})
	if err != nil {
		return repository.IntegrationPrincipalRecord{}, err
	}
	return p, nil
}
func (s *IntegrationService) UpdatePrincipal(ctx context.Context, id, name, status string, version int64, resolve port.SessionPrincipalResolver) (repository.IntegrationPrincipalRecord, error) {
	if !integrationmodel.OpaqueID(id) || !validIntegrationName(name) || version < 1 || (status != "active" && status != "suspended" && status != "revoked") {
		return repository.IntegrationPrincipalRecord{}, ErrIntegrationInvalid
	}
	var out repository.IntegrationPrincipalRecord
	err := s.managed(ctx, "update", id, resolve, func(tx context.Context, _ string) (any, error) {
		p, err := s.repo.GetIntegrationPrincipal(tx, id, true)
		if err != nil {
			return nil, err
		}
		if p.Version != version || p.Status == "revoked" {
			return nil, ErrIntegrationChanged
		}
		p.Name = name
		p.Status = status
		ok, err := s.repo.UpdateIntegrationPrincipal(tx, &p, version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrIntegrationChanged
		}
		out = p
		fresh, err := s.repo.GetIntegrationPrincipal(tx, id, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(fresh, out) {
			return nil, ErrIntegrationChanged
		}
		return out, nil
	})
	if err != nil {
		return repository.IntegrationPrincipalRecord{}, err
	}
	return out, nil
}

type IntegrationIssueRequest struct {
	PrincipalVersion int64                        `json:"principal_version,string"`
	NotBeforeMS      int64                        `json:"not_before_ms"`
	ExpiresAtMS      int64                        `json:"expires_at_ms"`
	Rules            []port.IntegrationAccessRule `json:"rules"`
}
type IntegrationIssueResult struct {
	Credential repository.IntegrationCredentialRecord `json:"credential"`
	Token      string                                 `json:"token"`
}

func (s *IntegrationService) validIssue(req IntegrationIssueRequest) bool {
	if s == nil || s.now == nil || req.PrincipalVersion < 1 || req.NotBeforeMS < 0 || req.ExpiresAtMS <= req.NotBeforeMS || req.ExpiresAtMS > 9007199254740991 || req.ExpiresAtMS <= s.now().UnixMilli() || req.ExpiresAtMS-s.now().UnixMilli() > int64((90*24*time.Hour)/time.Millisecond) || integrationmodel.ValidateRules(req.Rules) != nil {
		return false
	}
	for _, r := range req.Rules {
		if _, ok := integrationEndpoint(port.IntegrationAccessTarget{Channel: r.Channel, Endpoint: r.Endpoint, Action: r.Action, Resource: port.PolicyResourceRef{Kind: r.ResourceKind, ID: r.ResourceIDs[0]}}); !ok || r.NotBeforeMS < req.NotBeforeMS || r.ExpiresAtMS > req.ExpiresAtMS {
			return false
		}
	}
	return true
}
func (s *IntegrationService) newCredential(pid, admin string, req IntegrationIssueRequest) (repository.IntegrationCredentialRecord, string, error) {
	id, err := integrationID("", 8)
	if err != nil {
		return repository.IntegrationCredentialRecord{}, "", ErrIntegrationUnavailable
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return repository.IntegrationCredentialRecord{}, "", ErrIntegrationUnavailable
	}
	prefix := "cint1_" + id
	raw := prefix + "." + base64.RawURLEncoding.EncodeToString(secret)
	return repository.IntegrationCredentialRecord{ID: id, PrincipalID: pid, Prefix: prefix, Digest: integrationDigest(raw), Purpose: "integration_api", Status: "active", Rules: repository.CloneIntegrationCredential(repository.IntegrationCredentialRecord{Rules: req.Rules}).Rules, NotBeforeMS: req.NotBeforeMS, ExpiresAtMS: req.ExpiresAtMS, CreatedByAdminID: admin}, raw, nil
}
func (s *IntegrationService) IssueCredential(ctx context.Context, pid string, req IntegrationIssueRequest, resolve port.SessionPrincipalResolver) (IntegrationIssueResult, error) {
	return s.issue(ctx, pid, "", 0, req, resolve)
}
func (s *IntegrationService) RotateCredential(ctx context.Context, pid, oldID string, oldVersion int64, req IntegrationIssueRequest, resolve port.SessionPrincipalResolver) (IntegrationIssueResult, error) {
	if !integrationmodel.OpaqueID(oldID) || oldVersion < 1 {
		return IntegrationIssueResult{}, ErrIntegrationInvalid
	}
	return s.issue(ctx, pid, oldID, oldVersion, req, resolve)
}
func (s *IntegrationService) issue(ctx context.Context, pid, oldID string, oldVersion int64, req IntegrationIssueRequest, resolve port.SessionPrincipalResolver) (IntegrationIssueResult, error) {
	if !integrationmodel.OpaqueID(pid) || !s.validIssue(req) {
		return IntegrationIssueResult{}, ErrIntegrationInvalid
	}
	operation := "issue"
	if oldID != "" {
		operation = "rotate"
	}
	var out IntegrationIssueResult
	err := s.managed(ctx, operation, pid, resolve, func(tx context.Context, admin string) (any, error) {
		if !s.validIssue(req) {
			return nil, ErrIntegrationInvalid
		}
		p, err := s.repo.GetIntegrationPrincipal(tx, pid, true)
		if err != nil {
			return nil, err
		}
		if p.Status != "active" || p.Version != req.PrincipalVersion {
			return nil, ErrIntegrationChanged
		}
		var revoked *repository.IntegrationCredentialRecord
		if oldID != "" {
			old, err := s.repo.GetIntegrationCredential(tx, oldID, true)
			if err != nil {
				return nil, err
			}
			if old.PrincipalID != pid || old.Status != "active" || old.Version != oldVersion {
				return nil, ErrIntegrationChanged
			}
			old.Status = "revoked"
			ok, err := s.repo.UpdateIntegrationCredential(tx, &old, oldVersion)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, ErrIntegrationChanged
			}
			revoked = &old
		}
		c, token, err := s.newCredential(pid, admin, req)
		if err != nil {
			return nil, err
		}
		if err = s.repo.CreateIntegrationCredential(tx, &c); err != nil {
			return nil, err
		}
		current, err := s.repo.GetIntegrationCredential(tx, c.ID, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(c, current) {
			return nil, ErrIntegrationChanged
		}
		fresh, err := s.repo.GetIntegrationPrincipal(tx, pid, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(p, fresh) {
			return nil, ErrIntegrationChanged
		}
		if s.now().UnixMilli() >= c.ExpiresAtMS {
			return nil, ErrIntegrationInvalid
		}
		var revokedVersion int64
		if revoked != nil {
			old, err := s.repo.GetIntegrationCredential(tx, oldID, true)
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(old, *revoked) {
				return nil, ErrIntegrationChanged
			}
			revokedVersion = old.Version
		}
		out = IntegrationIssueResult{Credential: c, Token: token}
		return map[string]any{"operation": operation, "principal_id": pid, "credential_id": c.ID, "old_credential_id": oldID, "revoked_version": revokedVersion, "version": c.Version, "rules": c.Rules}, nil
	})
	if err != nil {
		return IntegrationIssueResult{}, err
	}
	return out, nil
}
func (s *IntegrationService) UpdateCredential(ctx context.Context, pid, id string, version int64, rules []port.IntegrationAccessRule, revoke bool, resolve port.SessionPrincipalResolver) (repository.IntegrationCredentialRecord, error) {
	if !integrationmodel.OpaqueID(pid) || !integrationmodel.OpaqueID(id) || version < 1 {
		return repository.IntegrationCredentialRecord{}, ErrIntegrationInvalid
	}
	operation := "scope"
	if revoke {
		operation = "revoke"
	}
	var out repository.IntegrationCredentialRecord
	err := s.managed(ctx, operation, pid, resolve, func(tx context.Context, _ string) (any, error) {
		p, err := s.repo.GetIntegrationPrincipal(tx, pid, true)
		if err != nil {
			return nil, err
		}
		c, err := s.repo.GetIntegrationCredential(tx, id, true)
		if err != nil {
			return nil, err
		}
		if c.PrincipalID != pid || c.Version != version || c.Status != "active" {
			return nil, ErrIntegrationChanged
		}
		if revoke {
			c.Status = "revoked"
		} else {
			req := IntegrationIssueRequest{PrincipalVersion: p.Version, NotBeforeMS: c.NotBeforeMS, ExpiresAtMS: c.ExpiresAtMS, Rules: rules}
			if p.Status != "active" || !s.validIssue(req) {
				return nil, ErrIntegrationInvalid
			}
			c.Rules = repository.CloneIntegrationCredential(repository.IntegrationCredentialRecord{Rules: rules}).Rules
		}
		ok, err := s.repo.UpdateIntegrationCredential(tx, &c, version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrIntegrationChanged
		}
		fresh, err := s.repo.GetIntegrationCredential(tx, id, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(c, fresh) {
			return nil, ErrIntegrationChanged
		}
		out = c
		return map[string]any{"operation": operation, "principal_id": pid, "credential_id": id, "version": c.Version, "rules": c.Rules}, nil
	})
	if err != nil {
		return repository.IntegrationCredentialRecord{}, err
	}
	return out, nil
}

type IntegrationInventory struct {
	Principals  []repository.IntegrationPrincipalRecord  `json:"principals"`
	Credentials []repository.IntegrationCredentialRecord `json:"credentials"`
	NextCursor  string                                   `json:"next_cursor"`
}

func (s *IntegrationService) Inventory(ctx context.Context, pid, after string, resolve port.SessionPrincipalResolver) (IntegrationInventory, error) {
	if pid != "" && !integrationmodel.OpaqueID(pid) || after != "" && !integrationmodel.OpaqueID(after) {
		return IntegrationInventory{}, ErrIntegrationInvalid
	}
	a, err := s.admin(ctx, "identity.integration.read", resolve, false)
	if err != nil {
		return IntegrationInventory{}, err
	}
	out := IntegrationInventory{Principals: []repository.IntegrationPrincipalRecord{}, Credentials: []repository.IntegrationCredentialRecord{}}
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		cur, err := s.admin(tx, "identity.integration.read", resolve, true)
		if err != nil {
			return err
		}
		if err = port.CheckAdminExecutionAuthority(a, cur); err != nil {
			return err
		}
		if pid == "" {
			out.Principals, err = s.repo.ListIntegrationPrincipals(tx, after, 100)
			if len(out.Principals) == 100 {
				out.NextCursor = out.Principals[99].ID
			}
		} else {
			p, e := s.repo.GetIntegrationPrincipal(tx, pid, true)
			if e != nil {
				return e
			}
			out.Principals = append(out.Principals, p)
			out.Credentials, err = s.repo.ListIntegrationCredentials(tx, pid, after, 100)
			if len(out.Credentials) == 100 {
				out.NextCursor = out.Credentials[99].ID
			}
		}
		if err != nil {
			return err
		}
		cur, err = s.admin(tx, "identity.integration.read", resolve, true)
		if err != nil {
			return err
		}
		return port.CheckAdminExecutionAuthority(a, cur)
	})
	if err != nil {
		return IntegrationInventory{}, err
	}
	return out, nil
}
