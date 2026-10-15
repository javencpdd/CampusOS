package richtext

import (
	"context"
	"fmt"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
	"reflect"
	"time"
)

func (s *Service) loadAssetAdminReadAuthority(ctx context.Context, q identityport.AdminExecutionQuery, proof identityport.SessionPrincipalResolver) (identityport.AdminExecutionAuthority, error) {
	if s == nil || nilReadAdapter(s.store) || nilReadAdapter(s.adminExecution) {
		return identityport.AdminExecutionAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	if proof == nil {
		return identityport.AdminExecutionAuthority{}, identityport.ErrAdminSessionPrincipalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	if err := s.ensureEnabled(); err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, proof, false)
	if err != nil {
		return a, err
	}
	if a.Query != q {
		return a, identityport.ErrPolicyDecisionMismatch
	}
	if err := identityport.CheckAdminExecutionAuthority(a, a); err != nil {
		return a, err
	}
	if !a.Allowed {
		return a, ErrPermissionDenied
	}
	if time.Now().UnixMilli() >= a.ExpiresAtMS {
		return a, identityport.ErrPolicyFactsChanged
	}
	return a, nil
}
func (s *Service) checkAssetAdminReadAuthority(ctx context.Context, a identityport.AdminExecutionAuthority, proof identityport.SessionPrincipalResolver) error {
	current, err := s.loadAssetAdminReadAuthority(ctx, a.Query, proof)
	if err != nil {
		return err
	}
	return identityport.CheckAdminExecutionAuthority(a, current)
}
func checkedAssetSummary(value AssetGovernanceSummary, at time.Time) (AssetGovernanceSummary, error) {
	if !value.GeneratedAt.Equal(at) || value.ExpiredInvocations < 0 {
		return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
	}
	statuses, actions := map[string]bool{}, map[string]bool{}
	for _, v := range value.Statuses {
		switch v.Status {
		case AssetStatusActive, AssetStatusTrashed, AssetStatusQuarantined, AssetStatusPurging, AssetStatusDeleted:
		default:
			return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
		}
		if v.Count < 0 || v.SizeBytes < 0 || statuses[v.Status] {
			return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
		}
		statuses[v.Status] = true
	}
	for _, v := range value.Actions {
		switch v.Action {
		case "trashed", "restored", "quarantined", "purge_started", "purge_failed", "purged":
		default:
			return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
		}
		if v.Count < 0 || actions[v.Action] {
			return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
		}
		actions[v.Action] = true
	}
	if value.Statuses == nil {
		value.Statuses = []AssetStatusSummary{}
	}
	if value.Actions == nil {
		value.Actions = []AssetActionSummary{}
	}
	return value, nil
}
func (s *Service) AssetGovernanceSummaryForPrincipal(ctx context.Context, proof identityport.SessionPrincipalResolver) (AssetGovernanceSummary, error) {
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.asset.read_audit", Scope: identityport.PolicyResourceRef{Kind: "richtext.asset_governance", ID: "summary"}}
	a, err := s.loadAssetAdminReadAuthority(ctx, q, proof)
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	r := identityport.RichTextAssetSummaryRequest{Contract: identityport.RichTextAssetSummaryContract, PolicyVersion: identityport.RichTextAssetSummaryPolicyVersion, RequestID: fmt.Sprint(idgen.New()), Principal: a.Principal, ExecutionQuery: q, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: at.UnixMilli()}
	d, err := (identitypolicy.RichTextAssetSummaryPolicy{}).DecideRichTextAssetSummary(r)
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	if d.Effect != "allow" {
		return AssetGovernanceSummary{}, ErrPermissionDenied
	}
	load := func() (AssetGovernanceSummary, error) {
		v, err := s.store.AssetGovernanceSummary(ctx, at)
		if err != nil {
			return AssetGovernanceSummary{}, err
		}
		return checkedAssetSummary(v, at)
	}
	initial, err := load()
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	if err := s.checkAssetAdminReadAuthority(ctx, a, proof); err != nil {
		return AssetGovernanceSummary{}, err
	}
	current, err := load()
	if err != nil {
		return AssetGovernanceSummary{}, err
	}
	if !reflect.DeepEqual(initial, current) {
		return AssetGovernanceSummary{}, identityport.ErrPolicyFactsChanged
	}
	if err := s.checkAssetAdminReadAuthority(ctx, a, proof); err != nil {
		return AssetGovernanceSummary{}, err
	}
	return initial, nil
}
func assetPurgeRequest(f *assetOwnerSnapshot, a identityport.AdminExecutionAuthority, id string, at int64) (identityport.RichTextAssetPurgeRequest, error) {
	base, err := adminAssetRequest(f, a, "quarantine", id, at)
	if err != nil {
		return identityport.RichTextAssetPurgeRequest{}, err
	}
	r := identityport.RichTextAssetPurgeRequest{Contract: identityport.RichTextAssetPurgeContract, PolicyVersion: identityport.RichTextAssetPurgePolicyVersion, RequestID: id, Principal: a.Principal, Facts: base.Facts, ExecutionQuery: a.Query, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: at}
	return r, r.Validate()
}
func (s *Service) PreviewAssetPurgeForPrincipal(ctx context.Context, id string, proof identityport.SessionPrincipalResolver) (AssetPurgePreview, error) {
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.asset.manage", Scope: identityport.PolicyResourceRef{Kind: "richtext.user_asset", ID: id}}
	a, err := s.loadAssetAdminReadAuthority(ctx, q, proof)
	if err != nil {
		return AssetPurgePreview{}, err
	}
	initial, err := s.adminAssetSnapshot(ctx, id, "preview", false)
	if err != nil {
		return AssetPurgePreview{}, err
	}
	r, err := assetPurgeRequest(initial, a, fmt.Sprint(idgen.New()), time.Now().UnixMilli())
	if err != nil {
		return AssetPurgePreview{}, err
	}
	d, err := (identitypolicy.RichTextAssetPurgePolicy{}).DecideRichTextAssetPurge(r)
	if err != nil {
		return AssetPurgePreview{}, err
	}
	result := AssetPurgePreview{AssetID: id, Status: initial.Asset.Status, SizeBytes: initial.Asset.SizeBytes, ReferenceCount: initial.References, Eligible: d.Effect == "allow"}
	if result.Eligible {
		result.Reason = "当前状态与引用满足清除条件；执行时将再次校验权限、对象与引用。"
	} else if initial.References > 0 {
		result.Reason = "附件仍被文章正文或附件绑定引用，不能清除。"
	} else {
		result.Reason = "仅回收站、隔离或未完成清除状态的附件可以清除。"
	}
	if err := s.checkAssetAdminReadAuthority(ctx, a, proof); err != nil {
		return AssetPurgePreview{}, err
	}
	current, err := s.adminAssetSnapshot(ctx, id, "preview", false)
	if err != nil {
		return AssetPurgePreview{}, err
	}
	if !reflect.DeepEqual(initial, current) {
		return AssetPurgePreview{}, identityport.ErrPolicyFactsChanged
	}
	if err := s.checkAssetAdminReadAuthority(ctx, a, proof); err != nil {
		return AssetPurgePreview{}, err
	}
	return result, nil
}
