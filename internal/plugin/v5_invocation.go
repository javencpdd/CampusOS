package plugin

import (
	"context"
	"reflect"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type v5InvocationKey struct{}
type v5Invocation struct {
	instance, owner string
	resolve         identity.SessionPrincipalResolver
	require         func(context.Context, string) (PluginV5Consume, error)
	coordinate      V5Coordinate
	generation      int64
}

// V5InvocationAuthority exists only during an authenticated host-to-plugin call.
// It contains neither a credential nor reusable authority. Async instance-only
// calls cannot borrow a User session to spend a model budget.
type V5InvocationAuthority struct {
	Resolve    identity.SessionPrincipalResolver
	Require    func(context.Context, string) (PluginV5Consume, error)
	Coordinate V5Coordinate
	Generation int64
}

func V5CurrentInvocation(ctx context.Context, owner string) (V5InvocationAuthority, error) {
	v, ok := ctx.Value(v5InvocationKey{}).(v5Invocation)
	nested, _ := ctx.Value(v5NestedCallKey{}).(string)
	if !ok || owner == "" || owner != v.owner || nested != v.instance || v.resolve == nil || v.require == nil {
		return V5InvocationAuthority{}, ErrV5Denied
	}
	p, err := v.resolve(ctx)
	if err != nil || p.Validate() != nil || p.Actor.Kind != "user" || p.Actor.ID != owner {
		return V5InvocationAuthority{}, ErrV5Denied
	}
	return V5InvocationAuthority{v.resolve, v.require, v.coordinate, v.generation}, nil
}

func v5BindInvocation(ctx context.Context, i *v5ManagedInstance, resolve identity.SessionPrincipalResolver, p identity.PrincipalContext) context.Context {
	bound := func(c context.Context) (identity.PrincipalContext, error) {
		q, e := resolve(c)
		if e != nil || c.Err() != nil || q.Validate() != nil || !reflect.DeepEqual(q, p) {
			return identity.PrincipalContext{}, ErrV5Denied
		}
		return q, nil
	}
	return context.WithValue(ctx, v5InvocationKey{}, v5Invocation{instance: i.id, owner: i.owner, resolve: bound, coordinate: i.coordinate, generation: i.generation})
}
