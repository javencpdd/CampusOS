package service

import (
	"context"
	"reflect"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrThreadAuthorWriteDenied = communityport.ErrAuthorCommandDenied
var ErrThreadAuthorWriteInvalid = communityport.ErrAuthorCommandInvalid

var ErrThreadAuthorWriteUnavailable = communityport.ErrAuthorCommandUnavailable

type authorWriteFactsKey struct{}
type authorWriteFacts struct {
	ID      string
	Version int64
}

func authorEditRequest(thread *domain.Thread, principal identityport.PrincipalContext, requestID string) (identityport.ResourcePolicyRequest, error) {
	if thread == nil {
		return identityport.ResourcePolicyRequest{}, identityport.ErrPolicyDecisionMismatch
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return identityport.ResourcePolicyRequest{}, err
	}
	status := "active"
	if facts.DeletionStatus != "active" {
		status = "deleted"
	}
	return identityport.ResourcePolicyRequest{Contract: identityport.ResourcePolicyContract, RequestID: requestID, Principal: principal, Action: "community.thread.edit", Facts: identityport.ResourcePolicyFacts{Kind: "thread", ID: thread.ID, OwnerID: thread.AuthorID, BoardID: thread.CategoryID, Status: status, PublicationStatus: facts.PublicationStatus, Version: facts.FactsVersion}, Grants: []identityport.ResourcePolicyGrant{}, EvaluatedAtMS: time.Now().UnixMilli(), PolicyVersion: identityport.ResourcePolicyContract}, nil
}

// UpdateThreadForPrincipal is the authenticated author ingress. An actor ID
// does not authenticate this command. Actual resource Policy, credential and
// write snapshots are checked within the same required-audit transaction.
func (s *ThreadService) UpdateThreadForPrincipal(ctx context.Context, id string, req domain.UpdateThreadRequest, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if s.reliable == nil || s.governance == nil {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	principal, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if initial == nil {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	if initial.ThreadType != domain.ThreadTypeDiscussion {
		return nil, ErrThreadAuthorWriteDenied
	}
	if req.Status != nil && *req.Status != domain.ThreadStatusPublished && *req.Status != domain.ThreadStatusPrivate && *req.Status != domain.ThreadStatusDraft {
		return nil, ErrThreadAuthorWriteInvalid
	}
	if req.Content != nil {
		normalized, err := coreeditor.Normalize(*req.Content, initial.ContentFormat)
		if err != nil {
			return nil, ErrThreadAuthorWriteInvalid
		}
		content := normalized.Content
		req.Content = &content
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	request, err := authorEditRequest(initial, principal, requestID)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ResourcePolicy{}).DecideResource(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadAuthorWriteDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		p, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return p, err
		}
		if !reflect.DeepEqual(principal, p) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return p, nil
	}
	ctx = identityport.WithSessionPrincipalResolver(ctx, bound)
	var result *domain.Thread
	err = s.reliable.Execute(ctx, reliability.Command{Code: "community.thread.author_update", ActorID: principal.Actor.ID, ActorType: "user", ResourceType: "thread", ResourceID: id, OperationCode: "community.thread.author_update", EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventThreadUpdated, "thread", id, result)
	}}, func(tx context.Context) error {
		currentPrincipal, err := bound(tx)
		if err != nil {
			return err
		}
		current, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		currentRequest, err := authorEditRequest(current, currentPrincipal, requestID)
		if err != nil {
			return err
		}
		if err := policy.CheckResourceDecision(request, decision, currentRequest); err != nil {
			return err
		}
		commandCtx := context.WithValue(tx, authorWriteFactsKey{}, authorWriteFacts{ID: id, Version: current.FactsVersion})
		result, err = s.updateThread(commandCtx, id, principal.Actor.ID, req)
		if err != nil {
			return err
		}
		if _, err := bound(tx); err != nil {
			return err
		}
		final, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		// This command intentionally creates a new content revision. Recheck its
		// actual resulting authority; another mutation cannot reuse the old allow.
		if result == nil || final.FactsVersion != result.FactsVersion || final.AuthorID != principal.Actor.ID || final.CategoryID != initial.CategoryID || final.DeletionStatus != domain.DeletionStatusActive {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	return result, nil
}
