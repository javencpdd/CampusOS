package service

import (
	"context"
	"reflect"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrPostGovernanceDenied = communityport.ErrPostGovernanceDenied
var ErrPostGovernanceUnavailable = communityport.ErrPostGovernanceUnavailable

func (s *PostService) SetGovernanceAuthority(reader identityport.BoardGovernanceAuthorityReader) {
	s.governanceAuthority = reader
}
func postDeleteRequest(parent *domain.Thread, post *domain.Post, a identityport.BoardGovernanceAuthority, id string) (identityport.ResourcePolicyRequest, error) {
	facts, err := publicThreadFacts(parent, parent.ID)
	if err != nil {
		return identityport.ResourcePolicyRequest{}, err
	}
	if post == nil || post.ThreadID != parent.ID || post.FactsVersion < 1 {
		return identityport.ResourcePolicyRequest{}, identityport.ErrPolicyFactsChanged
	}
	status := "active"
	if post.DeletedAt != nil || post.Status != "published" || facts.DeletionStatus != "active" {
		status = "deleted"
	}
	return identityport.ResourcePolicyRequest{Contract: identityport.ResourcePolicyContract, RequestID: id, Principal: a.Principal, Action: "community.post.delete", Facts: identityport.ResourcePolicyFacts{Kind: "post", ID: post.ID, OwnerID: post.AuthorID, BoardID: parent.CategoryID, Status: status, PublicationStatus: facts.PublicationStatus, Version: facts.FactsVersion + ":" + strconv.FormatInt(post.FactsVersion, 10)}, Grants: a.Grants, EvaluatedAtMS: time.Now().UnixMilli(), PolicyVersion: identityport.ResourcePolicyContract}, nil
}

func (s *PostService) GovernanceDeleteForPrincipal(ctx context.Context, expectedThreadID, postID string, resolve identityport.SessionPrincipalResolver, participant communityport.PostGovernanceParticipant) error {
	if s == nil || s.reliable == nil || nilGovernancePort(s.governanceAuthority) {
		return ErrPostGovernanceUnavailable
	}
	posts, ok := s.repo.(repository.FactsGuardedPostRepository)
	if !ok || nilGovernancePort(posts) {
		return ErrPostGovernanceUnavailable
	}
	threads, ok := s.threadRepo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(threads) {
		return ErrPostGovernanceUnavailable
	}
	if participant != nil {
		if nilGovernancePort(participant) {
			return ErrPostGovernanceUnavailable
		}
		if err := participant.CheckPostGovernance(ctx); err != nil {
			return err
		}
		if snapshot, ok := participant.(transaction.Snapshotter); ok {
			s.reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
	initial, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		return err
	}
	if initial == nil || expectedThreadID != "" && initial.ThreadID != expectedThreadID {
		return ErrPostGovernanceDenied
	}
	parent, err := s.threadRepo.GetByID(ctx, initial.ThreadID)
	if err != nil {
		return err
	}
	q := identityport.BoardGovernanceQuery{BoardID: parent.CategoryID, Action: "community.post.delete"}
	authority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, q, resolve, false)
	if err != nil {
		return err
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	request, err := postDeleteRequest(parent, initial, authority, requestID)
	if err != nil {
		return err
	}
	decision, err := (policy.ResourcePolicy{}).DecideResource(request)
	if err != nil {
		return err
	}
	if decision.Effect != "allow" {
		return ErrPostGovernanceDenied
	}
	actor := authority.Principal.Actor
	err = s.reliable.Execute(ctx, reliability.Command{Code: q.Action, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "post", ResourceID: postID, OperationCode: q.Action, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventPostDeleted, "post", postID, map[string]string{"post_id": postID, "thread_id": initial.ThreadID})
	}}, func(tx context.Context) error {
		currentAuthority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckBoardGovernanceAuthority(authority, currentAuthority); err != nil {
			return err
		}
		currentParent, err := threads.LockThreadForWrite(tx, initial.ThreadID)
		if err != nil {
			return err
		}
		current, err := posts.LockPostForWrite(tx, postID)
		if err != nil {
			return err
		}
		currentRequest, err := postDeleteRequest(currentParent, current, currentAuthority, requestID)
		if err != nil {
			return err
		}
		if err := policy.CheckResourceDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if currentParent.ReplyCount < 1 {
			return ErrThreadStateConflict
		}
		if participant != nil {
			if err := participant.CheckPostGovernance(tx); err != nil {
				return err
			}
		}
		if err := posts.DeletePostIfFactsVersion(tx, postID, current.FactsVersion, actor); err != nil {
			return err
		}
		if err := s.threadRepo.IncrementReplyCount(tx, current.ThreadID, -1); err != nil {
			return err
		}
		writtenPost, err := posts.ReadPostCommandSnapshot(tx, postID)
		if err != nil {
			return err
		}
		if s.notifications != nil && (actor.Kind != "user" || actor.ID != current.AuthorID) {
			if err := s.notifications.NotifyPostDeletedByModerator(tx, current.AuthorID, current.ThreadID, currentParent.Title, postID); err != nil {
				return err
			}
		}
		if participant != nil {
			if err := participant.RecordPostGovernance(tx, currentAuthority.Principal, currentParent, current); err != nil {
				return err
			}
			if err := participant.CheckPostGovernance(tx); err != nil {
				return err
			}
		}
		finalAuthority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckBoardGovernanceAuthority(currentAuthority, finalAuthority); err != nil {
			return err
		}
		finalParent, err := threads.LockThreadForWrite(tx, current.ThreadID)
		if err != nil {
			return err
		}
		finalFacts, err := publicThreadFacts(finalParent, current.ThreadID)
		if err != nil {
			return err
		}
		initialParentFacts, err := publicThreadFacts(currentParent, current.ThreadID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initialParentFacts, finalFacts) || finalParent.ReplyCount != currentParent.ReplyCount-1 {
			return identityport.ErrPolicyFactsChanged
		}
		finalPost, err := posts.ReadPostCommandSnapshot(tx, postID)
		if err != nil {
			return err
		}
		if finalPost.DeletedAt == nil || finalPost.Status != "deleted" || finalPost.FactsVersion <= current.FactsVersion || finalPost.ThreadID != current.ThreadID || finalPost.AuthorID != current.AuthorID || finalPost.Content != current.Content || finalPost.ContentFormat != current.ContentFormat || finalPost.DeletedActorKind != actor.Kind || finalPost.DeletedActorID != actor.ID {
			return identityport.ErrPolicyFactsChanged
		}
		if !samePostCommandState(writtenPost, finalPost) {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidateThreadListCache(ctx)
	return nil
}
