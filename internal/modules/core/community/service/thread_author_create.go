package service

import (
	"context"
	"reflect"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type postingCategoryAuthority struct {
	ID          string
	ParentID    *string
	NodeKind    domain.CategoryNodeKind
	Lifecycle   domain.CategoryLifecycleStatus
	Version     int64
	Closed      bool
	DefaultTags []string
}
type threadCreateAuthority struct {
	Board postingCategoryAuthority
	Group *postingCategoryAuthority
	Types []domain.CategoryThreadTypePolicy
	Type  domain.ThreadType
}
type threadCreateProof struct {
	Principal identityport.PrincipalContext
	Authority threadCreateAuthority
	Resolve   identityport.SessionPrincipalResolver
}
type threadCreateProofKey struct{}

func categoryAuthority(c *domain.Category) postingCategoryAuthority {
	var parent *string
	if c.ParentID != nil {
		id := *c.ParentID
		parent = &id
	}
	return postingCategoryAuthority{ID: c.ID, ParentID: parent, NodeKind: c.NodeKind, Lifecycle: c.LifecycleStatus, Version: c.Version, Closed: c.IsClosed, DefaultTags: append([]string{}, c.DefaultTags...)}
}
func (s *ThreadService) createAuthority(ctx context.Context, boardID string, kind domain.ThreadType, lock bool) (threadCreateAuthority, error) {
	if s.categoryRepo == nil || s.typePolicies == nil {
		return threadCreateAuthority{}, ErrThreadAuthorWriteUnavailable
	}
	get := s.categoryRepo.GetByID
	if lock {
		get = s.categoryRepo.GetByIDForUpdate
	}
	board, err := get(ctx, boardID)
	if err != nil {
		return threadCreateAuthority{}, err
	}
	if board == nil || board.ID != boardID || board.Version < 1 || board.NodeKind != domain.CategoryNodeBoard || board.LifecycleStatus != domain.CategoryLifecycleActive || board.IsClosed {
		return threadCreateAuthority{}, ErrThreadAuthorWriteDenied
	}
	facts := threadCreateAuthority{Board: categoryAuthority(board), Type: kind}
	if board.ParentID != nil {
		group, err := get(ctx, *board.ParentID)
		if err != nil {
			return facts, err
		}
		if group == nil || group.ID != *board.ParentID || group.Version < 1 || group.NodeKind != domain.CategoryNodeGroup || group.LifecycleStatus != domain.CategoryLifecycleActive {
			return facts, ErrThreadAuthorWriteDenied
		}
		projection := categoryAuthority(group)
		facts.Group = &projection
	}
	types, err := s.typePolicies.List(ctx, boardID)
	if err != nil {
		return facts, err
	}
	allowed := false
	seen := map[domain.ThreadType]bool{}
	for _, entry := range types {
		if entry.CategoryID != boardID || !domain.IsKnownThreadType(entry.ThreadType) || seen[entry.ThreadType] {
			return facts, identityport.ErrPolicyDecisionMismatch
		}
		seen[entry.ThreadType] = true
		if entry.ThreadType == kind && entry.Enabled {
			allowed = true
		}
	}
	if !allowed {
		return facts, ErrThreadAuthorWriteDenied
	}
	facts.Types = types
	if s.typeEnabled != nil && !s.typeEnabled(kind) {
		return facts, ErrThreadAuthorWriteDenied
	}
	return facts, nil
}

// CreateThreadForPrincipal binds creation to an actual current User and board
// capability. An uncreated thread is never invented as an editable resource.
func (s *ThreadService) CreateThreadForPrincipal(ctx context.Context, authorName string, req domain.CreateThreadRequest, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if s.reliable == nil || s.governance == nil {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	p, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	if p.Actor.Kind != "user" || p.Audience != "user" {
		return nil, ErrThreadAuthorWriteDenied
	}
	req.CategoryID = strings.TrimSpace(req.CategoryID)
	facts, err := s.createAuthority(ctx, req.CategoryID, domain.ThreadTypeDiscussion, false)
	if err != nil {
		return nil, err
	}
	normalized, err := coreeditor.Normalize(req.Content, "markdown")
	if err != nil {
		return nil, ErrThreadAuthorWriteInvalid
	}
	req.Content = normalized.Content
	proof := threadCreateProof{Principal: p, Authority: facts, Resolve: resolve}
	result, err := s.CreateThread(context.WithValue(ctx, threadCreateProofKey{}, proof), p.Actor.ID, authorName, req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ThreadService) bindCreateCommand(ctx context.Context, actorID string, action func(context.Context) (*domain.Thread, error)) (func(context.Context) (*domain.Thread, error), error) {
	proof, ok := ctx.Value(threadCreateProofKey{}).(threadCreateProof)
	if !ok {
		return action, nil
	}
	if s.reliable == nil || actorID != proof.Principal.Actor.ID {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	return func(tx context.Context) (*domain.Thread, error) {
		p, err := resolveThreadOwnerPrincipal(tx, proof.Resolve)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(p, proof.Principal) {
			return nil, identityport.ErrPolicyFactsChanged
		}
		facts, err := s.createAuthority(tx, proof.Authority.Board.ID, proof.Authority.Type, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(facts, proof.Authority) {
			return nil, identityport.ErrPolicyFactsChanged
		}
		result, err := action(tx)
		if err != nil {
			return nil, err
		}
		p, err = resolveThreadOwnerPrincipal(tx, proof.Resolve)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(p, proof.Principal) {
			return nil, identityport.ErrPolicyFactsChanged
		}
		current, err := s.createAuthority(tx, proof.Authority.Board.ID, proof.Authority.Type, true)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(current, proof.Authority) || result == nil || result.AuthorID != p.Actor.ID || result.CategoryID != proof.Authority.Board.ID || result.ThreadType != proof.Authority.Type || result.DeletionStatus != domain.DeletionStatusActive {
			return nil, identityport.ErrPolicyFactsChanged
		}
		return result, nil
	}, nil
}
