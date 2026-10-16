package richtext

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func assetOwnerFixture(t *testing.T, operation string) (*Service, *ArticleAttachment, *attachmentUploadObjects, *corestorage.LocalAdapter, *reliability.MemoryStore) {
	t.Helper()
	ctx := context.Background()
	s, _, _, _, events, _ := articleCreateFixture(t)
	objects, adapter, _ := attachmentUploadObjectFixture(t, s)
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	a, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "asset.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.RemoveAttachment(ctx, draft.Article.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if operation == "restore" {
		if err := s.TrashUserAsset(ctx, "1001", a.AssetID); err != nil {
			t.Fatal(err)
		}
	}
	return s, a, objects, adapter, events
}
func TestAssetOwnerLifecycleCurrentPrincipalStateAndAuditAtomicity(t *testing.T) {
	for _, operation := range []string{"trash", "restore"} {
		for _, mode := range []string{"current", "other", "admin", "missing", "credential", "disabled", "audit", "quarantined", "references", "object_unavailable"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				s, a, objects, adapter, events := assetOwnerFixture(t, operation)
				original := s.store
				proof := identityport.SessionPrincipalResolver(articleCreateProof)
				switch mode {
				case "other":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, err := articleCreateProof(ctx)
						p.Actor.ID = "1002"
						return p, err
					}
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, err := articleCreateProof(ctx)
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
						return p, err
					}
				case "missing":
					proof = nil
				case "credential":
					calls := 0
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, err := articleCreateProof(ctx)
						calls++
						if calls > 1 {
							p.CredentialID = "new-session"
						}
						return p, err
					}
				case "disabled":
					s.enabled = func() bool { return false }
				case "audit":
					durable := reliability.NewService(transaction.NewMemory(original.(transaction.Snapshotter), events), articleCreateFailAudit{events})
					s.SetReliability(durable)
				case "quarantined":
					from := AssetStatusActive
					if operation == "restore" {
						from = AssetStatusTrashed
					}
					if err := s.store.TransitionUserAsset(ctx, a.AssetID, "1001", []string{from}, AssetStatusQuarantined, AssetLifecycleAudit{ActorID: "1001", ActorType: "user", Action: "test"}); err != nil {
						t.Fatal(err)
					}
				case "references":
					// Existing references deny trash. Restore preserves them.
					if err := s.store.CreateAttachment(ctx, a); err != nil {
						t.Fatal(err)
					}
				case "object_unavailable":
					object, err := objects.ObjectPort.Stat(ctx, "1001", a.Asset.StorageObjectID)
					if err != nil {
						t.Fatal(err)
					}
					if err := objects.ObjectPort.Delete(ctx, "1001", object.ID, object.Version); err != nil {
						t.Fatal(err)
					}
				}
				beforeStore, beforeEvents := original.(transaction.Snapshotter).Snapshot(), events.Snapshot()
				beforeUsage, err := adapter.Usage("1001")
				if err != nil {
					t.Fatal(err)
				}
				var ack *AssetMutationAck
				if operation == "trash" {
					ack, err = s.TrashUserAssetForPrincipal(ctx, a.AssetID, proof)
				} else {
					ack, err = s.RestoreUserAssetForPrincipal(ctx, a.AssetID, proof)
				}
				want := mode == "current" || operation == "restore" && mode == "references" || operation == "trash" && mode == "object_unavailable"
				if (err == nil) != want {
					t.Fatal(operation, mode, ack, err)
				}
				if want {
					expected := AssetStatusTrashed
					if operation == "restore" {
						expected = AssetStatusActive
					}
					if ack == nil || ack.AssetID != a.AssetID || ack.Status != expected {
						t.Fatal(ack)
					}
					row, err := s.store.GetUserAsset(ctx, a.AssetID)
					if err != nil || row.Status != expected {
						t.Fatal(row, err)
					}
				} else if ack != nil || !reflect.DeepEqual(beforeStore, original.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("failed asset command did not roll back", operation, mode, ack, err)
				}
				if usage, err := adapter.Usage("1001"); err != nil || usage != beforeUsage {
					t.Fatal("metadata lifecycle changed bytes", usage, beforeUsage, err)
				}
			})
		}
	}
}

type assetOwnerMutationStore struct {
	Store
	after func(context.Context, *UserAsset)
}

func (s *assetOwnerMutationStore) TransitionUserAsset(ctx context.Context, id, owner string, from []string, status string, audit AssetLifecycleAudit) error {
	if err := s.Store.TransitionUserAsset(ctx, id, owner, from, status, audit); err != nil {
		return err
	}
	a, err := s.Store.GetUserAsset(ctx, id)
	if err != nil {
		return err
	}
	s.after(ctx, a)
	return nil
}
func TestAssetOwnerLifecycleRejectsExtraMutationAndFinalSessionChanges(t *testing.T) {
	for _, mode := range []string{"extra_status", "new_reference", "credential", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, a, _, _, events := assetOwnerFixture(t, "trash")
			base := s.store
			before, history := base.(transaction.Snapshotter).Snapshot(), events.Snapshot()
			p, _ := articleCreateProof(ctx)
			resolve := func(context.Context) (identityport.PrincipalContext, error) { return p, nil }
			ran := false
			s.store = &assetOwnerMutationStore{Store: base, after: func(tx context.Context, row *UserAsset) {
				ran = true
				switch mode {
				case "extra_status":
					if err := base.TransitionUserAsset(tx, row.ID, "1001", []string{AssetStatusTrashed}, AssetStatusQuarantined, AssetLifecycleAudit{ActorID: "1001", ActorType: "user", Action: "test"}); err != nil {
						t.Fatal(err)
					}
				case "new_reference":
					if err := base.CreateAttachment(tx, a); err != nil {
						t.Fatal(err)
					}
				case "credential":
					p.CredentialID = "new-session"
				case "disabled":
					s.enabled = func() bool { return false }
				}
			}}
			ack, err := s.TrashUserAssetForPrincipal(ctx, a.AssetID, resolve)
			if err == nil || ack != nil || !ran {
				t.Fatal("actual mutation was not exercised", mode, ack, err, ran)
			}
			if !reflect.DeepEqual(before, base.(transaction.Snapshotter).Snapshot()) || !reflect.DeepEqual(history, events.Snapshot()) {
				t.Fatal("failed final asset proof leaked mutation", mode)
			}
			if mode == "credential" && !errors.Is(err, identityport.ErrPolicyFactsChanged) {
				t.Fatal(err)
			}
		})
	}
}
