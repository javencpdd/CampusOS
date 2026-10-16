package port

import (
	"context"
	"errors"
	"reflect"
)

var ErrGovernanceAuthorityUnavailable = errors.New("governance authority unavailable")

// BoardGovernanceQuery is supplied by the resource-owning module after loading
// its actual board. It contains no client-supplied actor, state or grant facts.
type BoardGovernanceQuery struct {
	BoardID string
	Action  string
}

func (q BoardGovernanceQuery) Validate() error {
	if !validOpaqueID(q.BoardID) {
		return ErrResourcePolicyInvalid
	}
	if q.Action != "community.thread.take_down" && q.Action != "community.post.delete" {
		return ErrResourcePolicyUnknownAction
	}
	return nil
}

// BoardGovernanceAuthority is a host-side bound snapshot, never a wire DTO or
// reusable execution capability. Full grant IDs/versions/windows and private
// role/session evidence stay opaque; the frozen ResourcePolicyGrant is intact.
type BoardGovernanceAuthority struct {
	Principal PrincipalContext      `json:"-"`
	Grants    []ResourcePolicyGrant `json:"-"`
	Binding   any                   `json:"-"`
}
type BoardGovernanceAuthorityReader interface {
	LoadBoardGovernanceAuthority(context.Context, BoardGovernanceQuery, SessionPrincipalResolver, bool) (BoardGovernanceAuthority, error)
}

func CheckBoardGovernanceAuthority(initial, current BoardGovernanceAuthority) error {
	if initial.Principal.Validate() != nil || current.Principal.Validate() != nil || (initial.Principal.Actor.Kind != "user" && initial.Principal.Actor.Kind != "admin") || nilUserSessionAdapter(initial.Binding) || nilUserSessionAdapter(current.Binding) || !reflect.DeepEqual(initial, current) {
		return ErrPolicyFactsChanged
	}
	return nil
}
