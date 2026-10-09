package delegation

import "context"

// ActorProof carries the host entry's verified authentication facts into the
// service layer. It is populated only by trusted middleware/handlers after
// server-side checks; an absent proof fails closed for MFA-gated paths.
type ActorProof struct {
	AuthenticationStrength string
	CredentialID           string
}

type actorProofKey struct{}

// WithActorProof attaches a verified actor proof to the command context.
func WithActorProof(ctx context.Context, proof ActorProof) context.Context {
	return context.WithValue(ctx, actorProofKey{}, proof)
}

// ActorProofFrom returns the attached proof; missing strength/credential are
// empty and must fail closed rather than default to a stronger level.
func ActorProofFrom(ctx context.Context) ActorProof {
	proof, _ := ctx.Value(actorProofKey{}).(ActorProof)
	return proof
}

// DelegationDeniedError reports a delegation proposal rejected by the policy
// chain; Reason is the stable contract code for client-facing mapping.
type DelegationDeniedError struct {
	Reason string
}

func (e *DelegationDeniedError) Error() string { return e.Reason }
