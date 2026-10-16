package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type AssetMutationAck struct {
	AssetID string `json:"asset_id"`
	Status  string `json:"status"`
}
type assetOwnerSnapshot struct {
	Raw        json.RawMessage
	Asset      *UserAsset
	References int
	Object     *corestorage.Object
}

func (s *Service) assetOwnerSnapshot(ctx context.Context, id, operation, owner string, lock bool) (*assetOwnerSnapshot, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	raw, err := s.store.LoadAssetAuthorityFacts(ctx, id, lock)
	if err != nil {
		return nil, err
	}
	asset, err := assetFromAuthorityFact(raw)
	if err != nil {
		return nil, err
	}
	if asset.OwnerID != owner {
		return nil, ErrPermissionDenied
	}
	if err := s.store.CheckAssetReferenceAuthority(ctx); err != nil {
		return nil, err
	}
	refs, err := s.store.AssetReferenceCount(ctx, id)
	if err != nil || refs < 0 {
		return nil, errors.Join(ErrAssetBindingFactsUnavailable, err)
	}
	f := &assetOwnerSnapshot{Raw: raw, Asset: asset, References: refs}
	if operation == "restore" {
		if nilReadAdapter(s.objects) {
			return nil, ErrAssetUnavailable
		}
		var object corestorage.Object
		if lock {
			reader, ok := s.objects.(corestorage.OwnedObjectCommandLocker)
			if !ok || nilReadAdapter(reader) {
				return nil, ErrAssetUnavailable
			}
			object, err = reader.LockOwnedObjectForCommand(ctx, asset.OwnerID, asset.StorageObjectID)
		} else {
			object, err = s.objects.Stat(ctx, asset.OwnerID, asset.StorageObjectID)
		}
		if err != nil {
			return nil, err
		}
		if object.ID != asset.StorageObjectID || object.OwnerID != asset.OwnerID || object.Status != corestorage.ObjectStatusReady || object.Version < 1 || object.SizeBytes != asset.SizeBytes || object.MimeType != asset.MimeType || object.DeletedAt != nil {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		object = canonicalAttachmentObject(object)
		f.Object = &object
	}
	if err := s.store.CheckAssetBindingAuthority(ctx); err != nil {
		return nil, err
	}
	if err := s.store.CheckAssetReferenceAuthority(ctx); err != nil {
		return nil, err
	}
	return f, nil
}
func assetOwnerRequest(f *assetOwnerSnapshot, p identityport.PrincipalContext, operation, id string) (identityport.RichTextAssetOwnerRequest, error) {
	nonce, err := rawMetadataNonce(f.Raw)
	if err != nil {
		return identityport.RichTextAssetOwnerRequest{}, err
	}
	r := identityport.RichTextAssetOwnerRequest{Contract: identityport.RichTextAssetOwnerContract, RequestID: id, PolicyVersion: identityport.RichTextAssetOwnerPolicyVersion, Principal: p, Operation: operation, Facts: identityport.RichTextAssetOwnerFacts{ID: f.Asset.ID, OwnerID: f.Asset.OwnerID, Kind: f.Asset.Kind, Status: f.Asset.Status, FactsVersion: strconv.FormatInt(nonce, 10), ReferenceCount: f.References}}
	return r, r.Validate()
}
func checkAssetOwnerMutation(before, after *assetOwnerSnapshot, status string) error {
	if before.References != after.References || !reflect.DeepEqual(before.Object, after.Object) {
		return identityport.ErrPolicyFactsChanged
	}
	if after.Asset.UpdatedAt.Before(before.Asset.UpdatedAt) {
		return identityport.ErrPolicyFactsChanged
	}
	expected := cloneUserAsset(before.Asset)
	expected.Status, expected.Version, expected.UpdatedAt = status, before.Asset.Version+1, after.Asset.UpdatedAt
	if status == AssetStatusTrashed {
		expected.TrashedAt = after.Asset.TrashedAt
		if expected.TrashedAt == nil || !expected.TrashedAt.Equal(after.Asset.UpdatedAt) {
			return identityport.ErrPolicyFactsChanged
		}
	}
	if status == AssetStatusActive {
		expected.TrashedAt = nil
	}
	if status == AssetStatusDeleted {
		expected.DeletedAt = after.Asset.DeletedAt
		if expected.DeletedAt == nil || !expected.DeletedAt.Equal(after.Asset.UpdatedAt) {
			return identityport.ErrPolicyFactsChanged
		}
	}
	if !reflect.DeepEqual(canonicalReadAsset(expected), after.Asset) {
		return identityport.ErrPolicyFactsChanged
	}
	initial, err := factRowFields(before.Raw, nil)
	if err != nil {
		return err
	}
	current, err := factRowFields(after.Raw, nil)
	if err != nil {
		return err
	}
	nonce, err := rawMetadataNonce(before.Raw)
	if err != nil {
		return err
	}
	next, err := rawMetadataNonce(after.Raw)
	if err != nil || next <= nonce {
		return identityport.ErrPolicyFactsChanged
	}
	for _, name := range []string{"status", "version", "updated_at", "trashed_at", "facts_version"} {
		delete(initial, name)
		delete(current, name)
	}
	if status == AssetStatusDeleted {
		delete(initial, "deleted_at")
		delete(current, "deleted_at")
	}
	if !reflect.DeepEqual(initial, current) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}

func (s *Service) executeAssetOwner(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver) (*AssetMutationAck, error) {
	if s == nil || nilReadAdapter(s.store) || s.reliable == nil {
		return nil, ErrAssetUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil {
		return nil, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	if p.Actor.Kind != "user" || p.Audience != "user" {
		return nil, ErrPermissionDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if err := ctx.Err(); err != nil {
			return identityport.PrincipalContext{}, err
		}
		current, err := resolve(ctx)
		if err != nil {
			return current, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
		}
		if current.Validate() != nil || !reflect.DeepEqual(current, p) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	initial, err := s.assetOwnerSnapshot(ctx, id, operation, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	request, err := assetOwnerRequest(initial, p, operation, fmt.Sprint(idgen.New()))
	if err != nil {
		return nil, err
	}
	decision, err := (identitypolicy.RichTextAssetOwnerPolicy{}).DecideRichTextAssetOwner(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		if p.Actor.ID == initial.Asset.OwnerID && operation == "trash" && initial.References > 0 {
			return nil, ErrAssetReferenced
		}
		return nil, ErrPermissionDenied
	}
	status := AssetStatusTrashed
	if operation == "restore" {
		status = AssetStatusActive
	}
	ack := AssetMutationAck{AssetID: id, Status: status}
	code := "feature.richtext.asset." + operation
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "user_asset", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("richtext.asset.updated", "user_asset", id, ack)
	}}, func(tx context.Context) error {
		currentP, err := bound(tx)
		if err != nil {
			return err
		}
		current, err := s.assetOwnerSnapshot(tx, id, operation, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		fresh, err := assetOwnerRequest(current, currentP, operation, request.RequestID)
		if err != nil {
			return err
		}
		if err := identitypolicy.CheckRichTextAssetOwnerDecision(request, decision, fresh); err != nil {
			return err
		}
		action := "trashed"
		if operation == "restore" {
			action = "restored"
		}
		if err := s.store.TransitionUserAsset(tx, id, p.Actor.ID, []string{initial.Asset.Status}, status, AssetLifecycleAudit{ActorID: p.Actor.ID, ActorType: "user", Action: action, Reason: "用户附件" + operation}); err != nil {
			return err
		}
		result, err := s.assetOwnerSnapshot(tx, id, operation, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if err := checkAssetOwnerMutation(initial, result, status); err != nil {
			return err
		}
		if _, err := bound(tx); err != nil {
			return err
		}
		final, err := s.assetOwnerSnapshot(tx, id, operation, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := bound(tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.refreshAssetMetrics(ctx)
	return &ack, nil
}
func (s *Service) TrashUserAssetForPrincipal(ctx context.Context, id string, p identityport.SessionPrincipalResolver) (*AssetMutationAck, error) {
	return s.executeAssetOwner(ctx, id, "trash", p)
}
func (s *Service) RestoreUserAssetForPrincipal(ctx context.Context, id string, p identityport.SessionPrincipalResolver) (*AssetMutationAck, error) {
	return s.executeAssetOwner(ctx, id, "restore", p)
}
