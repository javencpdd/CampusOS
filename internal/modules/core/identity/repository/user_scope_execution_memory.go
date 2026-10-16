package repository

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func detachedUserScopeRole(role *Role) *Role {
	if role == nil {
		return nil
	}
	v := *role
	return &v
}

// A conservative repository epoch detects every authority mutation. Restore
// restores the bound epoch while the allocation high water never rolls back.
func (r *MemoryRoleRepository) advanceUserScopeEpoch() error {
	if r.executionFactsHighWater == math.MaxInt64 {
		return ErrUserScopeFactsUnavailable
	}
	r.executionFactsHighWater++
	r.executionFactsEpoch = r.executionFactsHighWater
	return nil
}
func userScopeJSON(value any, epoch int64) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["facts_version"], err = json.Marshal(epoch)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func (r *MemoryRoleRepository) LoadUserScopedExecutionFacts(ctx context.Context, user string, board int64, lock bool) ([]UserScopedExecutionFact, error) {
	if r == nil || user == "" || board <= 0 || lock && !transaction.Active(ctx) {
		return nil, ErrUserScopeFactsUnavailable
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.executionFactsEpoch < 1 {
		return nil, ErrUserScopeFactsUnavailable
	}
	assignments := append([]*UserRoleAssignment(nil), r.userRoles[user]...)
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].ID < assignments[j].ID })
	facts := make([]UserScopedExecutionFact, 0)
	for _, a := range assignments {
		if a.UserID != user || !(a.ScopeType == "global" && a.ScopeID == nil || a.ScopeType == "category" && a.ScopeID != nil && *a.ScopeID == board) {
			continue
		}
		role := r.roleByID(a.RoleID)
		if role == nil {
			return nil, ErrUserScopeFactsUnavailable
		}
		codes := make([]string, 0, len(r.rolePermissions[role.ID]))
		for code, allowed := range r.rolePermissions[role.ID] {
			if allowed {
				codes = append(codes, code)
			}
		}
		sort.Strings(codes)
		for _, code := range codes {
			definition := r.definitions[code]
			if definition == nil {
				return nil, ErrUserScopeFactsUnavailable
			}
			if definition.DeprecatedAt != nil {
				continue
			}
			assignment := map[string]any{"id": a.ID, "user_id": a.UserID, "role_id": a.RoleID, "scope_type": a.ScopeType, "scope_id": a.ScopeID}
			// Memory links are role/definition pairs, not invented PG primary IDs.
			link := map[string]any{"role_id": role.ID, "permission_id": definition.ID}
			values := []any{assignment, role, link, definition}
			parts := make([]json.RawMessage, 4)
			for i, value := range values {
				var err error
				parts[i], err = userScopeJSON(value, r.executionFactsEpoch)
				if err != nil {
					return nil, err
				}
			}
			facts = append(facts, UserScopedExecutionFact{Assignment: parts[0], Role: parts[1], Link: parts[2], Definition: parts[3]})
		}
	}
	return facts, nil
}
