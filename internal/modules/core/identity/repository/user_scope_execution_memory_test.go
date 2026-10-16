package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func memoryUserScopeEpoch(t *testing.T, r *MemoryRoleRepository, user string, board int64) int64 {
	t.Helper()
	facts, err := r.LoadUserScopedExecutionFacts(context.Background(), user, board, false)
	if err != nil || len(facts) == 0 {
		t.Fatal(facts, err)
	}
	var row struct {
		Version int64 `json:"facts_version"`
	}
	if err := json.Unmarshal(facts[0].Assignment, &row); err != nil || row.Version < 1 {
		t.Fatal(row, err)
	}
	return row.Version
}

func TestUserScopedMemoryActualAssignmentsMutationABAAndAliasProtection(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryRoleRepository()
	board := int64(42)
	if rows, err := r.LoadUserScopedExecutionFacts(ctx, "1001", board, false); err != nil || len(rows) != 0 {
		t.Fatal("implicit roles are not scoped execution", rows, err)
	}
	if _, err := r.AssignRole(ctx, "1001", 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	before := memoryUserScopeEpoch(t, r, "1001", board)
	if rows, err := r.LoadUserScopedExecutionFacts(ctx, "1001", 43, false); err != nil || len(rows) != 0 {
		t.Fatal("cross board", rows, err)
	}
	if _, err := r.LoadUserScopedExecutionFacts(ctx, "1001", board, true); !errors.Is(err, ErrUserScopeFactsUnavailable) {
		t.Fatal(err)
	}
	role, _ := r.GetRoleByName(ctx, "moderator")
	role.Description = "injected"
	list, _ := r.GetUserRoles(ctx, "1001")
	for _, role := range list {
		role.Name = "injected"
	}
	assignments, _ := r.ListRoleAssignments(ctx, "1001", 2)
	*assignments[0].ScopeID = 43
	definitions, _ := r.ListPermissionDefinitions(ctx)
	for _, def := range definitions {
		if len(def.AllowedScopeTypes) > 0 {
			def.AllowedScopeTypes[0] = "injected"
		}
	}
	facts, err := r.LoadUserScopedExecutionFacts(ctx, "1001", board, false)
	if err != nil || len(facts) == 0 || memoryUserScopeEpoch(t, r, "1001", board) != before {
		t.Fatal("read aliases changed authority", facts, err)
	}
	for _, f := range facts {
		if !json.Valid(f.Role) || !json.Valid(f.Definition) {
			t.Fatal(f)
		}
	}
	stable := append(json.RawMessage(nil), facts[0].Role...)
	facts[0].Role[0] = 'x'
	fresh, _ := r.LoadUserScopedExecutionFacts(ctx, "1001", board, false)
	if !reflect.DeepEqual(stable, fresh[0].Role) {
		t.Fatal("raw fact alias")
	}
	codes := []string{"community.thread.pin", "community.thread.lock"}
	if err := r.ReplaceRolePermissions(ctx, 2, codes, "test"); err != nil {
		t.Fatal(err)
	}
	base := memoryUserScopeEpoch(t, r, "1001", board)
	if err := r.ReplaceRolePermissions(ctx, 2, codes, "test"); err != nil || memoryUserScopeEpoch(t, r, "1001", board) != base {
		t.Fatal("no-op permission set changed epoch", err)
	}
	if err := r.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin"}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceRolePermissions(ctx, 2, codes, "test"); err != nil || memoryUserScopeEpoch(t, r, "1001", board) <= base {
		t.Fatal("permission ABA reused facts", err)
	}
	base = memoryUserScopeEpoch(t, r, "1001", board)
	if _, err := r.ReplaceRoleScopes(ctx, "1001", 2, "category", []int64{43}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReplaceRoleScopes(ctx, "1001", 2, "category", []int64{42}); err != nil || memoryUserScopeEpoch(t, r, "1001", board) <= base {
		t.Fatal("scope ABA reused facts", err)
	}
	custom, err := r.CreateCustomRole(ctx, Role{Name: "custom_scope", Description: "original"})
	if err != nil {
		t.Fatal(err)
	}
	id := custom.ID
	custom.Description = "injected"
	actual, _ := r.GetRoleByID(ctx, id)
	if actual.Description != "original" {
		t.Fatal("creation alias")
	}
	if err := r.ReplaceRolePermissions(ctx, id, []string{"community.thread.pin"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AssignRole(ctx, "1002", id, "global", nil); err != nil {
		t.Fatal(err)
	}
	base = memoryUserScopeEpoch(t, r, "1002", board)
	actual.Description = "changed"
	if _, err := r.UpdateCustomRole(ctx, *actual); err != nil {
		t.Fatal(err)
	}
	actual.Description = "original"
	if _, err := r.UpdateCustomRole(ctx, *actual); err != nil || memoryUserScopeEpoch(t, r, "1002", board) <= base {
		t.Fatal("role metadata ABA reused facts", err)
	}
}

func TestUserScopedMemorySnapshotRollbackHighWaterAndExhaustion(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryRoleRepository()
	board := int64(42)
	if _, err := r.AssignRole(ctx, "1001", 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	base := memoryUserScopeEpoch(t, r, "1001", board)
	high := base
	err := transaction.NewMemory(r).Within(ctx, func(tx context.Context) error {
		if _, err := r.LoadUserScopedExecutionFacts(tx, "1001", board, true); err != nil {
			return err
		}
		if err := r.ReplaceRolePermissions(tx, 2, []string{"community.thread.pin"}, "test"); err != nil {
			return err
		}
		high = memoryUserScopeEpoch(t, r, "1001", board)
		return errors.New("force rollback")
	})
	if err == nil || !reflect.DeepEqual(before, r.Snapshot()) || memoryUserScopeEpoch(t, r, "1001", board) != base {
		t.Fatal("bound snapshot was not restored", err)
	}
	if err := r.ReplaceRolePermissions(ctx, 2, []string{"community.thread.pin"}, "test"); err != nil || memoryUserScopeEpoch(t, r, "1001", board) <= high {
		t.Fatal("rollback allocation reused", err)
	}
	if _, err := r.RevokeRole(ctx, "1001", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AssignRole(ctx, "1001", 2, "category", &board); err != nil {
		t.Fatal(err)
	}
	facts, err := r.LoadUserScopedExecutionFacts(ctx, "1001", board, false)
	if err != nil || len(facts) != 1 {
		t.Fatal(facts, err)
	}
	r.executionFactsHighWater = math.MaxInt64
	unchanged := r.Snapshot()
	if _, err := r.ReplaceRoleScopes(ctx, "1001", 2, "category", []int64{43}); !errors.Is(err, ErrUserScopeFactsUnavailable) || !reflect.DeepEqual(unchanged, r.Snapshot()) {
		t.Fatal("exhausted scope mutation persisted", err)
	}
	if err := r.ReplaceRolePermissions(ctx, 2, []string{"community.thread.lock"}, "test"); !errors.Is(err, ErrUserScopeFactsUnavailable) || !reflect.DeepEqual(unchanged, r.Snapshot()) {
		t.Fatal("exhausted permission mutation persisted", err)
	}
	if _, err := r.RevokeRole(ctx, "1001", 2); !errors.Is(err, ErrUserScopeFactsUnavailable) || !reflect.DeepEqual(unchanged, r.Snapshot()) {
		t.Fatal("exhausted revoke persisted", err)
	}
}
