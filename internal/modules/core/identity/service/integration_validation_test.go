package service

import (
	"context"
	"errors"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

func TestIntegrationIssueBoundsAndClosedInventory(t *testing.T) {
	for _, mode := range []string{"wildcard", "duplicates", "unknown_endpoint", "cross_channel", "admin_action", "private_kind", "knowledge_not_registered", "expired", "too_long", "rule_outside_credential", "null_rules", "invalid_version"} {
		t.Run(mode, func(t *testing.T) {
			s, proof, _ := integrationEnv(t, "current")
			ctx := context.Background()
			p, err := s.CreatePrincipal(ctx, "validation", proof)
			if err != nil {
				t.Fatal(err)
			}
			req := integrationRequest(p)
			switch mode {
			case "wildcard":
				req.Rules[0].ResourceIDs = []string{"*"}
			case "duplicates":
				req.Rules[0].ResourceIDs = []string{"123", "123"}
			case "unknown_endpoint":
				req.Rules[0].Endpoint = "new.read"
			case "cross_channel":
				req.Rules[0].Channel = "integration_mcp"
			case "admin_action":
				req.Rules[0].Action = "identity.admin_account.create"
			case "private_kind":
				req.Rules[0].ResourceKind = "personal.document"
			case "knowledge_not_registered":
				req.Rules[0].Action = "knowledge.source.read"
				req.Rules[0].ResourceKind = "public_collection"
				req.Rules[0].Endpoint = "knowledge.public.sources"
			case "expired":
				req.ExpiresAtMS = req.NotBeforeMS
			case "too_long":
				req.ExpiresAtMS += int64(100 * 24 * 60 * 60 * 1000)
			case "rule_outside_credential":
				req.Rules[0].ExpiresAtMS++
			case "null_rules":
				req.Rules = nil
			case "invalid_version":
				req.PrincipalVersion = 0
			}
			out, err := s.IssueCredential(ctx, p.ID, req, proof)
			if !errors.Is(err, ErrIntegrationInvalid) || out.Token != "" {
				t.Fatal("invalid scope issued key", err)
			}
			keys, err := s.repo.ListIntegrationCredentials(ctx, p.ID, "", 100)
			if err != nil || len(keys) != 0 {
				t.Fatal("invalid scope persisted", err)
			}
		})
	}
}

type integrationUnavailableRepo struct {
	repository.IntegrationRepository
}

func (integrationUnavailableRepo) GetIntegrationCredential(context.Context, string, bool) (repository.IntegrationCredentialRecord, error) {
	return repository.IntegrationCredentialRecord{}, repository.ErrIntegrationStoreUnavailable
}
func TestIntegrationMissingAdaptersAndUnavailableProof(t *testing.T) {
	s, proof, _ := integrationEnv(t, "current")
	ctx := context.Background()
	p, err := s.CreatePrincipal(ctx, "proof failure", proof)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.IssueCredential(ctx, p.ID, integrationRequest(p), proof)
	if err != nil {
		t.Fatal(err)
	}
	s.repo = integrationUnavailableRepo{s.repo}
	if _, err := s.CurrentIntegration(ctx, key.Token); !errors.Is(err, ErrIntegrationUnavailable) {
		t.Fatal("store fault treated as valid/stable credentials", err)
	}
	var typedNil *repository.MemoryIntegrationRepository
	s.repo = typedNil
	if _, err := s.CurrentIntegration(ctx, key.Token); !errors.Is(err, ErrIntegrationUnavailable) {
		t.Fatal("typed nil proof", err)
	}
	if _, err := s.CreatePrincipal(ctx, "unavailable", proof); !errors.Is(err, ErrIntegrationUnavailable) {
		t.Fatal("typed nil management", err)
	}
}
