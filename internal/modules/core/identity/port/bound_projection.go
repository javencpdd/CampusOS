package port

import (
	"context"
	"reflect"
)

// ReadBoundProjection coordinates a request-local owning module's snapshot,
// projection and final reread. load must perform that module's named Policy;
// this helper grants no resource permission and never discovers a Fact Port.
// Snapshots contain data only, not open readers or authorization capabilities.
func ReadBoundProjection[S, R any](ctx context.Context, resolve SessionPrincipalResolver, allowAnonymous bool,
	load func(context.Context, PrincipalContext) (S, error), read func(context.Context, PrincipalContext, S) (R, error), closeResult func(R),
) (result R, err error) {
	var zero R
	if load == nil || read == nil {
		return zero, ErrPolicyDecisionMismatch
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p := PrincipalContext{Contract: PrincipalContract, Actor: PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}
	var bound SessionPrincipalResolver
	if resolve == nil {
		if !allowAnonymous {
			return zero, ErrUserSessionPrincipalUnavailable
		}
	} else {
		p, err = resolve(ctx)
		if err != nil {
			return zero, err
		}
		if p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" {
			return zero, ErrUserSessionPrincipalUnavailable
		}
		bound = func(ctx context.Context) (PrincipalContext, error) {
			if err := ctx.Err(); err != nil {
				return PrincipalContext{}, err
			}
			current, err := resolve(ctx)
			if err != nil {
				return PrincipalContext{}, err
			}
			if current.Validate() != nil {
				return PrincipalContext{}, ErrUserSessionPrincipalUnavailable
			}
			if !reflect.DeepEqual(p, current) {
				return PrincipalContext{}, ErrPolicyFactsChanged
			}
			return current, nil
		}
	}
	ctx = WithSessionPrincipalResolver(ctx, bound)
	initial, err := load(ctx, p)
	if err != nil {
		return zero, err
	}
	result, err = read(ctx, p, initial)
	if err != nil {
		if closeResult != nil {
			closeResult(result)
		}
		return zero, err
	}
	accepted := false
	held := result
	defer func() {
		if !accepted && closeResult != nil {
			closeResult(held)
		}
	}()
	if bound != nil {
		if _, err := bound(ctx); err != nil {
			return zero, err
		}
	}
	current, err := load(ctx, p)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(initial, current) {
		return zero, ErrPolicyFactsChanged
	}
	if bound != nil {
		if _, err := bound(ctx); err != nil {
			return zero, err
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	accepted = true
	return result, nil
}
