package richtext

import (
	"context"
	"errors"
	"fmt"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/idgen"
	"reflect"
	"strings"
	"time"
)

var ErrAssetPurgePending = errors.New("asset purge remains pending")

type assetPurgeProof struct {
	Asset  *assetOwnerSnapshot
	Object corestorage.ObjectLifecycleReceipt
}

func (s *Service) loadAssetPurgeProof(ctx context.Context, id string, lock bool) (assetPurgeProof, error) {
	a, err := s.adminAssetSnapshot(ctx, id, "purge", lock)
	if err != nil {
		return assetPurgeProof{}, err
	}
	objects, ok := s.objects.(corestorage.OwnedObjectLifecycleCommands)
	if !ok || nilReadAdapter(objects) {
		return assetPurgeProof{}, ErrAssetUnavailable
	}
	r, err := objects.GetOwnedObjectLifecycle(ctx, a.Asset.OwnerID, a.Asset.StorageObjectID)
	if err != nil {
		return assetPurgeProof{}, err
	}
	if err := checkAssetPurgeObject(a, r.Object); err != nil {
		return assetPurgeProof{}, err
	}
	return assetPurgeProof{Asset: a, Object: r}, nil
}
func checkAssetPurgeObject(a *assetOwnerSnapshot, o corestorage.Object) error {
	if o.ID != a.Asset.StorageObjectID || o.OwnerID != a.Asset.OwnerID || o.SizeBytes != a.Asset.SizeBytes || o.MimeType != a.Asset.MimeType || o.Namespace != "richtext" || o.Purpose != a.Asset.Kind || o.Version < 1 {
		return identityport.ErrPolicyFactsChanged
	}
	if o.Status != corestorage.ObjectStatusReady && !((a.Asset.Status == AssetStatusPurging || a.Asset.Status == AssetStatusDeleted) && (o.Status == corestorage.ObjectStatusDeleting || o.Status == corestorage.ObjectStatusDeleted)) {
		return ErrAssetUnavailable
	}
	if o.Status == corestorage.ObjectStatusDeleted && o.DeletedAt == nil {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func checkPurgedObject(before, after corestorage.Object) error {
	if after.Status != corestorage.ObjectStatusDeleted || after.DeletedAt == nil {
		return identityport.ErrPolicyFactsChanged
	}
	expected := canonicalAttachmentObject(before)
	if before.Status == corestorage.ObjectStatusReady {
		expected.Version += 2
	} else if before.Status == corestorage.ObjectStatusDeleting {
		expected.Version++
	}
	expected.Status, expected.UpdatedAt, expected.DeletedAt = after.Status, after.UpdatedAt, after.DeletedAt
	if before.Status == corestorage.ObjectStatusDeleted {
		expected = canonicalAttachmentObject(before)
	}
	if !reflect.DeepEqual(expected, canonicalAttachmentObject(after)) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (s *Service) purgeAuthority(ctx context.Context, a identityport.AdminExecutionAuthority, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	if nilReadAdapter(s.adminExecution) {
		return a, identityport.ErrGovernanceAuthorityUnavailable
	}
	current, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, a.Query, resolve, lock)
	if err != nil {
		return current, err
	}
	if err := identityport.CheckAdminExecutionAuthority(a, current); err != nil {
		return current, err
	}
	if !current.Allowed || time.Now().UnixMilli() >= current.ExpiresAtMS {
		return current, identityport.ErrPolicyFactsChanged
	}
	if err := s.ensureEnabled(); err != nil {
		return current, err
	}
	return current, ctx.Err()
}
func (s *Service) assetPurgeMetadataPhase(ctx context.Context, before assetPurgeProof, a identityport.AdminExecutionAuthority, target, action, reason string, resolve identityport.SessionPrincipalResolver) (assetPurgeProof, error) {
	r, err := assetPurgeRequest(before.Asset, a, fmt.Sprint(idgen.New()), time.Now().UnixMilli())
	if err != nil {
		return assetPurgeProof{}, err
	}
	d, err := (identitypolicy.RichTextAssetPurgePolicy{}).DecideRichTextAssetPurge(r)
	if err != nil {
		return assetPurgeProof{}, err
	}
	if d.Effect != "allow" {
		return assetPurgeProof{}, ErrAssetPurgeIneligible
	}
	var result assetPurgeProof
	code := "feature.richtext.asset.purge." + action
	ack := AssetMutationAck{AssetID: before.Asset.Asset.ID, Status: target}
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: a.Principal.Actor.ID, ActorType: "admin", ResourceType: "user_asset", ResourceID: ack.AssetID, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("richtext.asset.updated", "user_asset", ack.AssetID, ack)
	}}, func(tx context.Context) error {
		currentA, err := s.purgeAuthority(tx, a, resolve, true)
		if err != nil {
			return err
		}
		current, err := s.loadAssetPurgeProof(tx, ack.AssetID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, current) {
			return identityport.ErrPolicyFactsChanged
		}
		fresh, err := assetPurgeRequest(current.Asset, currentA, r.RequestID, r.EvaluatedAtMS)
		if err != nil {
			return err
		}
		if err := identitypolicy.CheckRichTextAssetPurgeDecision(r, d, fresh); err != nil {
			return err
		}
		if err := s.store.TransitionUserAsset(tx, ack.AssetID, before.Asset.Asset.OwnerID, []string{before.Asset.Asset.Status}, target, AssetLifecycleAudit{ActorID: a.Principal.Actor.ID, ActorType: "admin", Action: action, Reason: reason}); err != nil {
			return err
		}
		result, err = s.loadAssetPurgeProof(tx, ack.AssetID, true)
		if err != nil {
			return err
		}
		if err := checkAssetOwnerMutation(before.Asset, result.Asset, target); err != nil {
			return err
		}
		if !reflect.DeepEqual(before.Object, result.Object) {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := s.purgeAuthority(tx, a, resolve, true); err != nil {
			return err
		}
		finalAsset, err := s.adminAssetSnapshot(tx, ack.AssetID, "purge", true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result.Asset, finalAsset) {
			return identityport.ErrPolicyFactsChanged
		}
		_, err = s.purgeAuthority(tx, a, resolve, true)
		return err
	})
	return result, err
}

func (s *Service) AdminPurgeAssetForPrincipal(ctx context.Context, id, reason string, resolve identityport.SessionPrincipalResolver) (*AssetMutationAck, error) {
	if s == nil || s.reliable == nil || nilReadAdapter(s.store) || nilReadAdapter(s.adminExecution) {
		return nil, identityport.ErrGovernanceAuthorityUnavailable
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return nil, ErrAssetInvalid
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.asset.manage", Scope: identityport.PolicyResourceRef{Kind: "richtext.user_asset", ID: id}}
	a, err := s.loadAssetAdminReadAuthority(ctx, q, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.loadAssetPurgeProof(ctx, id, false)
	if err != nil {
		return nil, err
	}
	started, err := s.assetPurgeMetadataPhase(ctx, initial, a, AssetStatusPurging, "purge_started", reason, resolve)
	if err != nil {
		return nil, err
	}
	objects := s.objects.(corestorage.OwnedObjectLifecycleCommands)
	var terminal corestorage.ObjectLifecycleReceipt
	code := "feature.richtext.asset.purge.delete_bytes"
	ack := AssetMutationAck{AssetID: id, Status: AssetStatusPurging}
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: a.Principal.Actor.ID, ActorType: "admin", ResourceType: "user_asset", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("richtext.asset.updated", "user_asset", id, ack)
	}}, func(tx context.Context) error {
		if _, err := s.purgeAuthority(tx, a, resolve, true); err != nil {
			return err
		}
		asset, err := s.adminAssetSnapshot(tx, id, "purge", true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(started.Asset, asset) {
			return identityport.ErrPolicyFactsChanged
		}
		// Hold Admin/Asset/reference locks through bytes, while Object's own
		// durable transactions use the original root context independently.
		current, err := objects.GetOwnedObjectLifecycle(ctx, asset.Asset.OwnerID, asset.Asset.StorageObjectID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(started.Object, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := objects.DeleteOwnedObjectForCommand(ctx, current); err != nil {
			return err
		}
		terminal, err = objects.GetOwnedObjectLifecycle(ctx, asset.Asset.OwnerID, asset.Asset.StorageObjectID)
		if err != nil {
			return err
		}
		if err := checkPurgedObject(current.Object, terminal.Object); err != nil {
			return err
		}
		finalAsset, err := s.adminAssetSnapshot(tx, id, "purge", true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(asset, finalAsset) {
			return identityport.ErrPolicyFactsChanged
		}
		_, err = s.purgeAuthority(tx, a, resolve, true)
		return err
	})
	if err != nil {
		return nil, errors.Join(ErrAssetPurgePending, err)
	}
	finalProof := assetPurgeProof{Asset: started.Asset, Object: terminal}
	if _, err := s.assetPurgeMetadataPhase(ctx, finalProof, a, AssetStatusDeleted, "purged", reason, resolve); err != nil {
		return nil, errors.Join(ErrAssetPurgePending, err)
	}
	s.refreshAssetMetrics(ctx)
	return &AssetMutationAck{AssetID: id, Status: AssetStatusDeleted}, nil
}
