package port

import (
	"context"
	"reflect"
)

// Identity supplies current independent Admin execution facts. The business
// owner supplies the actual scope and applies its named resource policy.
type AdminExecutionQuery struct {
	PermissionCode string
	Scope          PolicyResourceRef
}

func (q AdminExecutionQuery) Validate() error {
	switch q.PermissionCode {
	case "community.thread.read", "community.thread.pin", "community.thread.lock", "community.thread.review", "community.thread.trash", "community.thread.direct_restore", "community.thread.restore", "community.thread.purge":
		if q.Scope.Kind != "community.board" || !validOpaqueID(q.Scope.ID) {
			return ErrResourcePolicyInvalid
		}
	case "platform.reliability.read":
		if q.Scope.Kind != "platform.event_metadata" || q.Scope.ID != "recent" {
			return ErrResourcePolicyInvalid
		}
	case "personal_space.asset.manage":
		if q.Scope.Kind != "richtext.user_asset" || !validOpaqueID(q.Scope.ID) {
			return ErrResourcePolicyInvalid
		}
	case "personal_space.asset.read_audit":
		if q.Scope.Kind != "richtext.asset_governance" || q.Scope.ID != "summary" {
			return ErrResourcePolicyInvalid
		}
	default:
		return ErrResourcePolicyUnknownAction
	}
	return nil
}
func (q AdminExecutionQuery) RequiresCurrentMFA() bool {
	return q.PermissionCode != "community.thread.read" && q.PermissionCode != "platform.reliability.read" && q.PermissionCode != "personal_space.asset.read_audit"
}

type AdminExecutionAuthority struct {
	Principal   PrincipalContext    `json:"-"`
	Query       AdminExecutionQuery `json:"-"`
	Allowed     bool                `json:"-"`
	ExpiresAtMS int64               `json:"-"`
	Binding     any                 `json:"-"`
}
type AdminExecutionAuthorityReader interface {
	LoadAdminExecutionAuthority(context.Context, AdminExecutionQuery, SessionPrincipalResolver, bool) (AdminExecutionAuthority, error)
}

func CheckAdminExecutionAuthority(expected, current AdminExecutionAuthority) error {
	if expected.Query.Validate() != nil || expected.Principal.Validate() != nil || expected.Principal.Actor.Kind != "admin" || nilUserSessionAdapter(expected.Binding) || nilUserSessionAdapter(current.Binding) || !reflect.DeepEqual(expected, current) {
		return ErrPolicyFactsChanged
	}
	return nil
}
