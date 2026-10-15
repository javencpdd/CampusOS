package richtext

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func assetAdminReadFixture(t *testing.T, operation string) (articleGovernanceFixtureData, *ArticleAttachment) {
	t.Helper()
	g, a := assetAdminFixture(t, "adm.opaque.asset", "restore")
	code := "personal_space.asset.manage"
	if operation == "summary" {
		code = "personal_space.asset.read_audit"
	}
	if err := g.admins.ReplaceAdminRolePermissionFacts(context.Background(), "security_admin", []string{code}, "test"); err != nil {
		t.Fatal(err)
	}
	return g, a
}
func TestAssetAdminReadCurrentPolicyStateAndNoMutation(t *testing.T) {
	for _, op := range []string{"summary", "preview"} {
		for _, mode := range []string{"current", "user", "missing_proof", "missing_reader", "permission", "password", "expired", "disabled"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				g, a := assetAdminReadFixture(t, op)
				ctx := context.Background()
				proof := g.proof
				switch mode {
				case "user":
					proof = articleCreateProof
				case "missing_proof":
					proof = nil
				case "missing_reader":
					g.s.SetAdminExecutionAuthority(nil)
				case "permission":
					if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "test"); err != nil {
						t.Fatal(err)
					}
				case "password", "expired":
					row, err := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
					if err != nil {
						t.Fatal(err)
					}
					strength, at := "mfa", time.Now().Add(-time.Hour)
					if mode == "password" {
						strength = "password"
					}
					if _, ok, err := g.sessions.UpgradeSessionStrengthCAS(ctx, row.ID, row.Version, strength, at, time.Now()); err != nil || !ok {
						t.Fatal(err, ok)
					}
				case "disabled":
					g.s.enabled = func() bool { return false }
				}
				before, history := g.s.store.(*MemoryStore).Snapshot(), g.events.Snapshot()
				var err error
				if op == "summary" {
					_, err = g.s.AssetGovernanceSummaryForPrincipal(ctx, proof)
				} else {
					_, err = g.s.PreviewAssetPurgeForPrincipal(ctx, a.AssetID, proof)
				}
				want := mode == "current" || op == "summary" && (mode == "password" || mode == "expired")
				if (err == nil) != want {
					t.Fatal(mode, err)
				}
				if !reflect.DeepEqual(before, g.s.store.(*MemoryStore).Snapshot()) || !reflect.DeepEqual(history, g.events.Snapshot()) {
					t.Fatal("Admin read mutated business/history")
				}
			})
		}
	}
	for _, status := range []string{AssetStatusActive, AssetStatusTrashed, AssetStatusQuarantined, AssetStatusPurging, AssetStatusDeleted} {
		for _, refs := range []bool{false, true} {
			t.Run(status+fmtAssetRefs(refs), func(t *testing.T) {
				g, a := assetAdminReadFixture(t, "preview")
				ctx := context.Background()
				action := map[string]string{AssetStatusActive: "restored", AssetStatusTrashed: "trashed", AssetStatusQuarantined: "quarantined", AssetStatusPurging: "purge_started", AssetStatusDeleted: "purged"}[status]
				if err := g.s.store.TransitionUserAsset(ctx, a.AssetID, a.Asset.OwnerID, []string{AssetStatusQuarantined}, status, AssetLifecycleAudit{ActorID: g.actor, ActorType: "admin", Action: action}); err != nil {
					t.Fatal(err)
				}
				if refs {
					if err := g.s.store.CreateAttachment(ctx, a); err != nil {
						t.Fatal(err)
					}
				}
				preview, err := g.s.PreviewAssetPurgeForPrincipal(ctx, a.AssetID, g.proof)
				want := !refs && (status == AssetStatusTrashed || status == AssetStatusQuarantined || status == AssetStatusPurging)
				if err != nil || preview.Eligible != want || preview.AssetID != a.AssetID || preview.Status != status {
					t.Fatal(preview, err)
				}
			})
		}
	}
}
func fmtAssetRefs(refs bool) string {
	if refs {
		return "/referenced"
	}
	return "/unreferenced"
}

type assetAdminReadHook struct {
	Store
	after          func(context.Context)
	empty, invalid bool
}

func (s *assetAdminReadHook) AssetGovernanceSummary(ctx context.Context, at time.Time) (AssetGovernanceSummary, error) {
	v, err := s.Store.AssetGovernanceSummary(ctx, at)
	if s.empty {
		v = AssetGovernanceSummary{GeneratedAt: at}
	}
	if err == nil && s.after != nil {
		f := s.after
		s.after = nil
		f(ctx)
	}
	if s.invalid {
		v.ExpiredInvocations = -1
	}
	return v, err
}
func (s *assetAdminReadHook) AssetReferenceCount(ctx context.Context, id string) (int, error) {
	refs, err := s.Store.AssetReferenceCount(ctx, id)
	if err == nil && s.after != nil {
		f := s.after
		s.after = nil
		f(ctx)
	}
	return refs, err
}
func TestAssetAdminReadFinalWholeProjectionAuthorityAndEmpty(t *testing.T) {
	for _, op := range []string{"summary", "preview"} {
		for _, mode := range []string{"stable", "session", "permission_aba", "metadata", "metadata_aba", "reference", "disabled", "cancel", "empty_session", "empty_current", "invalid_summary"} {
			if op == "preview" && (mode == "empty_session" || mode == "empty_current" || mode == "invalid_summary") {
				continue
			}
			t.Run(op+"/"+mode, func(t *testing.T) {
				g, a := assetAdminReadFixture(t, op)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				base := g.s.store
				code := "personal_space.asset.manage"
				if op == "summary" {
					code = "personal_space.asset.read_audit"
				}
				ran := false
				hook := &assetAdminReadHook{Store: base, empty: mode == "empty_session" || mode == "empty_current", invalid: mode == "invalid_summary", after: func(ctx context.Context) {
					ran = true
					switch mode {
					case "session", "empty_session":
						row, _ := g.sessions.GetSessionByID(ctx, g.claims.SessionID)
						if _, ok, err := g.sessions.RevokeSessionCAS(ctx, row.ID, row.Version, "test", time.Now()); err != nil || !ok {
							t.Fatal(err, ok)
						}
					case "permission_aba":
						if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "test"); err != nil {
							t.Fatal(err)
						}
						if err := g.admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{code}, "test"); err != nil {
							t.Fatal(err)
						}
					case "metadata", "metadata_aba":
						if err := base.TransitionUserAsset(ctx, a.AssetID, a.Asset.OwnerID, []string{AssetStatusQuarantined}, AssetStatusTrashed, AssetLifecycleAudit{ActorID: g.actor, ActorType: "admin", Action: "trashed"}); err != nil {
							t.Fatal(err)
						}
						if mode == "metadata_aba" {
							if err := base.TransitionUserAsset(ctx, a.AssetID, a.Asset.OwnerID, []string{AssetStatusTrashed}, AssetStatusQuarantined, AssetLifecycleAudit{ActorID: g.actor, ActorType: "admin", Action: "quarantined"}); err != nil {
								t.Fatal(err)
							}
						}
					case "reference":
						if err := base.CreateAttachment(ctx, a); err != nil {
							t.Fatal(err)
						}
					case "disabled":
						g.s.enabled = func() bool { return false }
					case "cancel":
						cancel()
					}
				}}
				g.s.store = hook
				var err error
				if op == "summary" {
					v, e := g.s.AssetGovernanceSummaryForPrincipal(ctx, g.proof)
					err = e
					if mode == "empty_current" && e == nil && (len(v.Statuses) != 0 || len(v.Actions) != 0 || v.Statuses == nil || v.Actions == nil) {
						t.Fatal("empty arrays", v)
					}
				} else {
					_, err = g.s.PreviewAssetPurgeForPrincipal(ctx, a.AssetID, g.proof)
				}
				want := mode == "stable" || mode == "empty_current" || op == "summary" && mode == "reference"
				if (err == nil) != want || !ran {
					t.Fatal("final read proof wrong", mode, err, ran)
				}
			})
		}
	}
}
