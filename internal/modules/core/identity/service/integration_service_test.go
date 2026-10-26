package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func integrationEnv(t *testing.T, mode string) (*IntegrationService, port.SessionPrincipalResolver, *reliability.MemoryStore) {
	t.Helper()
	sessions, proof := governanceAdminProof(t, mode)
	admins := sessions.admins.(*repository.MemoryAdminDomainRepository)
	ctx := context.Background()
	if err := admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: "binding.integration", AccountID: "22001", RoleID: "security_admin", GrantedBy: "22001"}); err != nil {
		t.Fatal(err)
	}
	codes := []string{"identity.integration.read", "identity.integration.manage"}
	if mode == "no_permission" {
		codes = []string{"integration.integration.read"}
	}
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", codes, "integration-test"); err != nil {
		t.Fatal(err)
	}
	r := repository.NewMemoryIntegrationRepository()
	store := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(r, admins, sessions.sessions.(*repository.MemoryAdminSessionRepository)), store)
	return NewIntegrationService(r, NewBoardGovernanceAuthorityService(nil, admins, sessions), reliable), proof, store
}
func integrationRequest(p repository.IntegrationPrincipalRecord) IntegrationIssueRequest {
	now := time.Now().UnixMilli()
	return IntegrationIssueRequest{PrincipalVersion: p.Version, NotBeforeMS: now - 1000, ExpiresAtMS: now + 3600000, Rules: []port.IntegrationAccessRule{{ID: "http", Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", ResourceKind: "community.thread", ResourceIDs: []string{"123", "124"}, Status: "active", NotBeforeMS: now - 1000, ExpiresAtMS: now + 3600000}, {ID: "mcp", Channel: "integration_mcp", Endpoint: "public_thread_read", Action: "community.thread.read", ResourceKind: "community.thread", ResourceIDs: []string{"123"}, Status: "active", NotBeforeMS: now - 1000, ExpiresAtMS: now + 3600000}}}
}
func TestIntegrationLifecycleAndSecretProjection(t *testing.T) {
	s, proof, store := integrationEnv(t, "current")
	ctx := context.Background()
	p, err := s.CreatePrincipal(ctx, "external reader", proof)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Token) != 66 || issued.Token[:22] != issued.Credential.Prefix {
		t.Fatal("token shape")
	}
	list, err := s.Inventory(ctx, p.ID, "", proof)
	if err != nil || len(list.Credentials) != 1 {
		t.Fatal(list, err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), issued.Token) || strings.Contains(string(raw), "digest") {
		t.Fatal("secret leaked in list")
	}
	rotated, err := s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token == issued.Token || rotated.Credential.ID == issued.Credential.ID {
		t.Fatal("rotation reused credential")
	}
	old, _ := s.repo.GetIntegrationCredential(ctx, issued.Credential.ID, false)
	if old.Status != "revoked" {
		t.Fatal("old key remains active")
	}
	_, err = s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, integrationRequest(p), proof)
	if err == nil {
		t.Fatal("rotation replay")
	}
	if _, err = s.UpdateCredential(ctx, p.ID, rotated.Credential.ID, rotated.Credential.Version, nil, true, proof); err != nil {
		t.Fatal(err)
	}
	p, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "suspended", p.Version, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.IssueCredential(ctx, p.ID, integrationRequest(p), proof); err == nil {
		t.Fatal("suspended issued key")
	}
	p, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "active", p.Version, proof)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "revoked", p.Version, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdatePrincipal(ctx, p.ID, p.Name, "active", p.Version, proof); err == nil {
		t.Fatal("principal resurrected")
	}
	audits, total, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 100})
	if err != nil || total != 7 {
		t.Fatalf("audit count %d: %v", total, err)
	}
	for _, a := range audits {
		if a.ActorType != "admin" || a.ActorID != "22001" || a.EventID == "" {
			t.Fatal("untyped or missing outbox", a)
		}
	}
	snapshot, _ := json.Marshal(store.Snapshot())
	if strings.Contains(string(snapshot), issued.Token) || strings.Contains(string(snapshot), rotated.Token) || strings.Contains(string(snapshot), "digest") {
		t.Fatal("raw token in audit/outbox")
	}
}

type integrationFailStore struct {
	reliability.Store
	audit bool
	event bool
}

func (s integrationFailStore) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.audit {
		return errors.New("injected audit failure")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s integrationFailStore) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.event {
		return nil, errors.New("injected outbox failure")
	}
	return s.Store.Enqueue(ctx, e)
}
func TestIntegrationLifecycleRollback(t *testing.T) {
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			s, proof, store := integrationEnv(t, "current")
			ctx := context.Background()
			p, err := s.CreatePrincipal(ctx, "rollback", proof)
			if err != nil {
				t.Fatal(err)
			}
			issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
			if err != nil {
				t.Fatal(err)
			}
			s.reliable = reliability.NewService(transaction.NewMemory(s.repo.(transaction.Snapshotter), store), integrationFailStore{Store: store, audit: failure == "audit", event: failure == "outbox"})
			result, err := s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, integrationRequest(p), proof)
			if err == nil || result.Token != "" {
				t.Fatal("failed rotation exposed token")
			}
			old, _ := s.repo.GetIntegrationCredential(ctx, issued.Credential.ID, false)
			if old.Status != "active" || old.Version != issued.Credential.Version {
				t.Fatal("failed rotation revoked old")
			}
			keys, _ := s.repo.ListIntegrationCredentials(ctx, p.ID, "", 100)
			if len(keys) != 1 {
				t.Fatal("uncommitted replacement survived")
			}
		})
	}
}
func TestIntegrationManagementDomainAndMFA(t *testing.T) {
	for _, mode := range []string{"current", "password", "expired", "no_permission", "user", "integration", "nil"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, _ := integrationEnv(t, mode)
			if mode == "user" {
				proof = governanceUserProof(t)
			}
			if mode == "nil" {
				proof = nil
			}
			if mode == "integration" {
				proof = func(context.Context) (port.PrincipalContext, error) {
					return port.PrincipalContext{Contract: port.PrincipalContract, Actor: port.PrincipalRef{Kind: "integration", ID: "22001"}, Audience: "integration_api", AuthenticationStrength: "credential", CredentialID: "same"}, nil
				}
			}
			p, err := s.CreatePrincipal(context.Background(), "domain check", proof)
			if mode == "current" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || p.ID != "" {
				t.Fatal("wrong domain/MFA allowed")
			}
		})
	}
}
func TestIntegrationConcurrentRotationSingleWinner(t *testing.T) {
	s, proof, _ := integrationEnv(t, "current")
	ctx := context.Background()
	p, err := s.CreatePrincipal(ctx, "concurrent", proof)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	req := integrationRequest(p)
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result, err := s.RotateCredential(ctx, p.ID, issued.Credential.ID, issued.Credential.Version, req, proof); err == nil {
				if result.Token == "" {
					t.Error("empty committed token")
				}
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("rotation winners", count.Load())
	}
	keys, _ := s.repo.ListIntegrationCredentials(ctx, p.ID, "", 100)
	if len(keys) != 2 {
		t.Fatal("partial rotations", len(keys))
	}
}
