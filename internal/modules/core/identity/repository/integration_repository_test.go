package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/integrationmodel"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func integrationStoreContract(t *testing.T, r IntegrationRepository, tx transaction.Manager) {
	t.Helper()
	ctx := context.Background()
	p := IntegrationPrincipalRecord{ID: "ip_store", Name: "store", Status: "active", CreatedByAdminID: "admin_store"}
	if err := r.CreateIntegrationPrincipal(ctx, &p); err != nil {
		t.Fatal(err)
	}
	c := IntegrationCredentialRecord{ID: "0123456789abcdef", PrincipalID: p.ID, Prefix: "cint1_0123456789abcdef", Digest: [32]byte{1, 2, 3}, Purpose: "integration_api", Status: "active", Rules: []integrationmodel.Rule{{ID: "rule", Channel: "integration_http", Endpoint: "public_thread.read", Action: "community.thread.read", ResourceKind: "community.thread", ResourceIDs: []string{"123"}, Status: "active", NotBeforeMS: 1, ExpiresAtMS: 9999999999999}}, NotBeforeMS: 1, ExpiresAtMS: 9999999999999, CreatedByAdminID: "admin_store"}
	if err := r.CreateIntegrationCredential(ctx, &c); !errors.Is(err, ErrIntegrationStoreUnavailable) {
		t.Fatalf("unlocked create: %v", err)
	}
	if err := tx.Within(ctx, func(ctx context.Context) error {
		_, err := r.GetIntegrationPrincipal(ctx, p.ID, true)
		if err != nil {
			return err
		}
		return r.CreateIntegrationCredential(ctx, &c)
	}); err != nil {
		t.Fatal(err)
	}
	initial := CloneIntegrationCredential(c)
	c.Rules[0].ResourceIDs[0] = "mutated"
	stored, err := r.GetIntegrationCredential(ctx, c.ID, false)
	if err != nil || !reflect.DeepEqual(stored, initial) {
		t.Fatalf("mutable alias: %v", err)
	}
	raw, _ := json.Marshal(stored)
	if strings.Contains(string(raw), "digest") {
		t.Fatal("digest in projection")
	}
	if _, err := r.GetIntegrationPrincipal(ctx, p.ID, true); !errors.Is(err, ErrIntegrationStoreUnavailable) {
		t.Fatal("lock outside transaction")
	}
	rollback := errors.New("audit failure")
	var consumed int64
	err = tx.Within(ctx, func(ctx context.Context) error {
		cur, e := r.GetIntegrationCredential(ctx, c.ID, true)
		if e != nil {
			return e
		}
		cur.Status = "revoked"
		ok, e := r.UpdateIntegrationCredential(ctx, &cur, cur.Version)
		if e != nil || !ok {
			t.Fatalf("revoke: %v", e)
		}
		consumed = cur.Version
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	after, _ := r.GetIntegrationCredential(ctx, c.ID, false)
	if !reflect.DeepEqual(after, initial) {
		t.Fatal("rollback lost credential")
	}
	err = tx.Within(ctx, func(ctx context.Context) error {
		after.Status = "revoked"
		ok, e := r.UpdateIntegrationCredential(ctx, &after, after.Version)
		if e != nil || !ok {
			t.Fatalf("revoke: %v", e)
		}
		if after.Version <= consumed {
			t.Fatal("version reused after rollback")
		}
		ok, e = r.UpdateIntegrationCredential(ctx, &initial, initial.Version)
		if e != nil || ok {
			t.Fatalf("stale CAS: %v", e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Within(ctx, func(ctx context.Context) error {
		after.Status = "active"
		ok, _ := r.UpdateIntegrationCredential(ctx, &after, after.Version)
		if ok {
			t.Fatal("revoked restored")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListIntegrationPrincipals(ctx, "", 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v", err)
	}
	rows, err = r.ListIntegrationPrincipals(ctx, p.ID, 1)
	if err != nil || len(rows) != 0 {
		t.Fatalf("cursor: %v", err)
	}
}
func TestIntegrationMemoryStore(t *testing.T) {
	r := NewMemoryIntegrationRepository()
	integrationStoreContract(t, r, transaction.NewMemory(r))
}
func TestIntegrationMemoryVersionExhaustionFailsClosed(t *testing.T) {
	r := NewMemoryIntegrationRepository()
	r.highWater = math.MaxInt64
	p := IntegrationPrincipalRecord{ID: "exhausted", Name: "exhausted", Status: "active", CreatedByAdminID: "admin"}
	if err := r.CreateIntegrationPrincipal(context.Background(), &p); !errors.Is(err, ErrIntegrationStoreUnavailable) {
		t.Fatal("version wrapped", err)
	}
	if r.highWater != math.MaxInt64 || len(r.principals) != 0 {
		t.Fatal("exhausted write persisted")
	}
}
