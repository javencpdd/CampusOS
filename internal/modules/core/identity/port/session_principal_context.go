package port

import "context"

// SessionPrincipalResolver carries a fresh credential verifier across trusted
// in-process composition adapters. It is neither a cached Principal nor a wire
// field; consumers still validate the domain and bind the complete Principal.
type SessionPrincipalResolver func(context.Context) (PrincipalContext, error)

type sessionPrincipalResolverKey struct{}

func WithSessionPrincipalResolver(ctx context.Context, resolve SessionPrincipalResolver) context.Context {
	return context.WithValue(ctx, sessionPrincipalResolverKey{}, resolve)
}

func SessionPrincipalResolverFromContext(ctx context.Context) SessionPrincipalResolver {
	resolve, _ := ctx.Value(sessionPrincipalResolverKey{}).(SessionPrincipalResolver)
	return resolve
}
