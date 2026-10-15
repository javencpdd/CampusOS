package richtext

import (
	"context"
	"fmt"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/idgen"
	"reflect"
	"strings"
	"time"
)

func (s *Service) adminAssetSnapshot(ctx context.Context, id, operation string, lock bool) (*assetOwnerSnapshot, error) {
	raw, err := s.store.LoadAssetAuthorityFacts(ctx, id, lock)
	if err != nil {
		return nil, err
	}
	asset, err := assetFromAuthorityFact(raw)
	if err != nil {
		return nil, err
	}
	byteOperation := "trash"
	if operation == "restore" {
		byteOperation = "restore"
	}
	f, err := s.assetOwnerSnapshot(ctx, id, byteOperation, asset.OwnerID, lock)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(raw, f.Raw) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	return f, nil
}
func adminAssetRequest(f *assetOwnerSnapshot, a identityport.AdminExecutionAuthority, operation, id string, at int64) (identityport.RichTextAssetAdminRequest, error) {
	facts, err := assetOwnerRequest(f, a.Principal, "restore", id)
	if err != nil {
		return identityport.RichTextAssetAdminRequest{}, err
	}
	r := identityport.RichTextAssetAdminRequest{Contract: identityport.RichTextAssetAdminContract, PolicyVersion: identityport.RichTextAssetAdminPolicyVersion, RequestID: id, Operation: operation, Principal: a.Principal, Facts: facts.Facts, ExecutionQuery: a.Query, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: at}
	return r, r.Validate()
}

// Governs metadata only; neither execution authority nor the ack exposes bytes.
func (s *Service) AdminAssetLifecycleForPrincipal(ctx context.Context, id, operation, reason string, resolve identityport.SessionPrincipalResolver) (*AssetMutationAck, error) {
	if s == nil || nilReadAdapter(s.store) || nilReadAdapter(s.adminExecution) || s.reliable == nil {
		return nil, identityport.ErrGovernanceAuthorityUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrAdminSessionPrincipalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if operation != "quarantine" && operation != "restore" {
		return nil, identityport.ErrResourcePolicyUnknownAction
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return nil, ErrAssetInvalid
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.asset.manage", Scope: identityport.PolicyResourceRef{Kind: "richtext.user_asset", ID: id}}
	initialAuthority, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, err
	}
	if !initialAuthority.Allowed {
		return nil, ErrPermissionDenied
	}
	initial, err := s.adminAssetSnapshot(ctx, id, operation, false)
	if err != nil {
		return nil, err
	}
	r, err := adminAssetRequest(initial, initialAuthority, operation, fmt.Sprint(idgen.New()), time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	d, err := (identitypolicy.RichTextAssetAdminPolicy{}).DecideRichTextAssetAdmin(r)
	if err != nil {
		return nil, err
	}
	if d.Effect != "allow" {
		return nil, ErrPermissionDenied
	}
	checkAuthority := func(ctx context.Context, lock bool) (identityport.AdminExecutionAuthority, error) {
		a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
		if err != nil {
			return a, err
		}
		if err := identityport.CheckAdminExecutionAuthority(initialAuthority, a); err != nil {
			return a, err
		}
		if !a.Allowed || time.Now().UnixMilli() >= a.ExpiresAtMS {
			return a, identityport.ErrPolicyFactsChanged
		}
		return a, nil
	}
	if _, err := checkAuthority(ctx, false); err != nil {
		return nil, err
	}
	status, action := AssetStatusQuarantined, "quarantined"
	if operation == "restore" {
		status, action = AssetStatusActive, "restored"
	}
	ack := AssetMutationAck{AssetID: id, Status: status}
	code := "feature.richtext.asset.admin_" + operation
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: initialAuthority.Principal.Actor.ID, ActorType: "admin", ResourceType: "user_asset", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("richtext.asset.updated", "user_asset", id, ack)
	}}, func(tx context.Context) error {
		a, err := checkAuthority(tx, true)
		if err != nil {
			return err
		}
		current, err := s.adminAssetSnapshot(tx, id, operation, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		fresh, err := adminAssetRequest(current, a, operation, r.RequestID, r.EvaluatedAtMS)
		if err != nil {
			return err
		}
		if err := identitypolicy.CheckRichTextAssetAdminDecision(r, d, fresh); err != nil {
			return err
		}
		if err := s.store.TransitionUserAsset(tx, id, initial.Asset.OwnerID, []string{initial.Asset.Status}, status, AssetLifecycleAudit{ActorID: a.Principal.Actor.ID, ActorType: "admin", Action: action, Reason: reason}); err != nil {
			return err
		}
		result, err := s.adminAssetSnapshot(tx, id, operation, true)
		if err != nil {
			return err
		}
		if err := checkAssetOwnerMutation(initial, result, status); err != nil {
			return err
		}
		if _, err := checkAuthority(tx, true); err != nil {
			return err
		}
		final, err := s.adminAssetSnapshot(tx, id, operation, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		_, err = checkAuthority(tx, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.refreshAssetMetrics(ctx)
	return &ack, nil
}
