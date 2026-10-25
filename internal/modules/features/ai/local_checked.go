package ai

import (
	"context"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const PortLocalCheckedChat = "ai.local-checked-chat/v1"
const PortCheckedChat = "ai.checked-chat/v1"

// CheckedChat is available only to trusted composition. Resource-owning callers
// must supply their current consent/source proof; it is never a public bypass.
type CheckedChat interface {
	PlanChatDisclosure(context.Context, identityport.SessionPrincipalResolver, int64) (LocalDisclosurePlan, error)
	InvokeChecked(context.Context, identityport.SessionPrincipalResolver, string, int64, ChatRequest, func(context.Context) error) (ManagedResult, error)
}

func (s *ManagedService) PlanChatDisclosure(ctx context.Context, resolve identityport.SessionPrincipalResolver, revision int64) (LocalDisclosurePlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Zero is used only by the trusted status adapter. Execution and prepared
	// requests always carry an explicit positive revision checked by Knowledge.
	if revision == 0 {
		if e := s.check(ctx); e != nil {
			return LocalDisclosurePlan{}, managedPublicError(e)
		}
		if _, _, e := managedPrincipal(ctx, resolve); e != nil {
			return LocalDisclosurePlan{}, ErrManagedDenied
		}
		b, e := s.binding(ctx, false)
		if e != nil {
			return LocalDisclosurePlan{}, ErrManagedUnavailable
		}
		revision = b.Revision
	}
	p, b, e := s.chatPlanSnapshot(ctx, resolve, revision, false)
	if e != nil {
		return LocalDisclosurePlan{}, e
	}
	return LocalDisclosurePlan{LocalPlan: LocalPlan{p.ID, p.Revision, b.Revision}, Definition: p.Definition, Model: p.Model, Endpoint: p.Endpoint, MaxTokens: p.MaxTokens}, nil
}

func (s *ManagedService) InvokeChecked(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string, revision int64, request ChatRequest, guard func(context.Context) error) (ManagedResult, error) {
	if guard == nil {
		return ManagedResult{}, ErrManagedDenied
	}
	return s.invoke(ctx, resolve, id, revision, request, "", guard)
}

type LocalPlan struct {
	ProfileID       string
	ProfileRevision int64
	BindingRevision int64
}

// LocalDisclosurePlan describes the exact local destination and output bound
// a user must review. It contains no Secret reference, key, or credential.
type LocalDisclosurePlan struct {
	LocalPlan
	Definition string
	Model      string
	Endpoint   string
	MaxTokens  int
}

// LocalCheckedChat is composition-only. Its guard carries current source
// checks from the owning domain, never a reusable allow bit or plugin token.
type LocalCheckedChat interface {
	PlanLocal(context.Context, identityport.SessionPrincipalResolver, int64) (LocalPlan, error)
	PlanLocalDisclosure(context.Context, identityport.SessionPrincipalResolver, int64) (LocalDisclosurePlan, error)
	InvokeLocalChecked(context.Context, identityport.SessionPrincipalResolver, string, int64, ChatRequest, func(context.Context) error) (ManagedResult, error)
}

func (s *ManagedService) PlanLocal(ctx context.Context, resolve identityport.SessionPrincipalResolver, bindingRevision int64) (LocalPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, b, e := s.localPlanSnapshot(ctx, resolve, bindingRevision)
	if e != nil {
		return LocalPlan{}, e
	}
	return LocalPlan{p.ID, p.Revision, b.Revision}, nil
}

func (s *ManagedService) PlanLocalDisclosure(ctx context.Context, resolve identityport.SessionPrincipalResolver, bindingRevision int64) (LocalDisclosurePlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, b, e := s.localPlanSnapshot(ctx, resolve, bindingRevision)
	if e != nil {
		return LocalDisclosurePlan{}, e
	}
	return LocalDisclosurePlan{
		LocalPlan:  LocalPlan{p.ID, p.Revision, b.Revision},
		Definition: p.Definition,
		Model:      p.Model,
		Endpoint:   p.Endpoint,
		MaxTokens:  p.MaxTokens,
	}, nil
}

func (s *ManagedService) localPlanSnapshot(ctx context.Context, resolve identityport.SessionPrincipalResolver, bindingRevision int64) (ManagedProfile, ManagedBinding, error) {
	return s.chatPlanSnapshot(ctx, resolve, bindingRevision, true)
}

func (s *ManagedService) chatPlanSnapshot(ctx context.Context, resolve identityport.SessionPrincipalResolver, bindingRevision int64, localOnly bool) (ManagedProfile, ManagedBinding, error) {
	var zeroProfile ManagedProfile
	var zeroBinding ManagedBinding
	if e := s.check(ctx); e != nil {
		return zeroProfile, zeroBinding, managedPublicError(e)
	}
	_, bound, e := managedPrincipal(ctx, resolve)
	if e != nil {
		return zeroProfile, zeroBinding, ErrManagedDenied
	}
	b, e := s.binding(ctx, false)
	if e != nil {
		return zeroProfile, zeroBinding, ErrManagedUnavailable
	}
	if b.Revision != bindingRevision {
		return zeroProfile, zeroBinding, ErrManagedConflict
	}
	p, e := s.profile(ctx, b.ProfileID, false)
	if e != nil {
		return zeroProfile, zeroBinding, ErrManagedUnavailable
	}
	local := p.ID == "local-chat" && p.Definition == "builtin.local-openai.text"
	remote := p.ID == "remote-chat" && p.Definition == "builtin.openai-compatible.text"
	if !p.Enabled || (!local && (!remote || localOnly)) {
		return zeroProfile, zeroBinding, ErrManagedDenied
	}
	secret, e := s.secret(ctx, p)
	clear(secret)
	if e != nil {
		return zeroProfile, zeroBinding, managedPublicError(e)
	}
	if _, e = bound(ctx); e != nil {
		return zeroProfile, zeroBinding, ErrManagedDenied
	}
	if e = s.current(ctx, p, b); e != nil {
		return zeroProfile, zeroBinding, managedPublicError(e)
	}
	if _, e = bound(ctx); e != nil {
		return zeroProfile, zeroBinding, ErrManagedDenied
	}
	if e = s.check(ctx); e != nil {
		return zeroProfile, zeroBinding, managedPublicError(e)
	}
	return p, b, nil
}

func (s *ManagedService) InvokeLocalChecked(ctx context.Context, resolve identityport.SessionPrincipalResolver, id string, bindingRevision int64, request ChatRequest, guard func(context.Context) error) (ManagedResult, error) {
	if guard == nil {
		return ManagedResult{}, ErrManagedDenied
	}
	return s.invoke(ctx, resolve, id, bindingRevision, request, "local-chat", guard)
}
