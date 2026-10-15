package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrPostAuthorDenied = errors.New("post author command denied")
var ErrPostAuthorUnavailable = errors.New("post author command unavailable")
var ErrPostAuthorInvalid = errors.New("post author command invalid")

func (s *PostService) SetThreadCommandSource(source *ThreadService) { s.threadCommandSource = source }
func postOwnerRequest(parent *domain.Thread, post *domain.Post, p identityport.PrincipalContext, operation, requestID string) (identityport.PostOwnerWriteRequest, error) {
	facts, err := publicThreadFacts(parent, parent.ID)
	if err != nil {
		return identityport.PostOwnerWriteRequest{}, err
	}
	r := identityport.PostOwnerWriteRequest{Contract: identityport.PostOwnerWriteContract, PolicyVersion: identityport.PostOwnerWritePolicyVersion, RequestID: requestID, Principal: p, Operation: operation, Parent: facts, ParentLocked: parent.IsLocked}
	if post != nil {
		r.Post = &identityport.PostOwnerWriteFacts{ID: post.ID, ThreadID: post.ThreadID, OwnerID: post.AuthorID, Status: post.Status, Version: post.FactsVersion, Deleted: post.DeletedAt != nil}
	}
	return r, nil
}
func samePostCommandState(a, b *domain.Post) bool {
	if a == nil || b == nil {
		return false
	}
	x, y := *a, *b
	x.LikeCount, y.LikeCount = 0, 0
	x.UpdatedAt, y.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(x, y)
}

func (s *PostService) CreatePostForPrincipal(ctx context.Context, threadID, authorName string, req domain.CreatePostRequest, resolve identityport.SessionPrincipalResolver) (*domain.Post, error) {
	return s.authorPostCommand(ctx, "create", threadID, "", authorName, req.Content, normalizePostParentID(req.ParentID), resolve)
}
func (s *PostService) UpdatePostForPrincipal(ctx context.Context, id, content string, resolve identityport.SessionPrincipalResolver) (*domain.Post, error) {
	return s.authorPostCommand(ctx, "edit", "", id, "", content, nil, resolve)
}
func (s *PostService) DeletePostForPrincipal(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) error {
	_, err := s.authorPostCommand(ctx, "delete", "", id, "", "", nil, resolve)
	return err
}

func (s *PostService) authorPostCommand(ctx context.Context, operation, threadID, postID, authorName, content string, parentID *string, resolve identityport.SessionPrincipalResolver) (*domain.Post, error) {
	if s == nil || s.reliable == nil || nilGovernancePort(s.repo) || nilGovernancePort(s.threadRepo) {
		return nil, ErrPostAuthorUnavailable
	}
	posts, ok := s.repo.(repository.FactsGuardedPostRepository)
	if !ok || nilGovernancePort(posts) {
		return nil, ErrPostAuthorUnavailable
	}
	threads, ok := s.threadRepo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(threads) {
		return nil, ErrPostAuthorUnavailable
	}
	p, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	var initial *domain.Post
	if operation != "create" {
		initial, err = s.repo.GetByID(ctx, postID)
		if err != nil {
			return nil, err
		}
		threadID = initial.ThreadID
	}
	parent, err := s.threadRepo.GetByID(ctx, threadID)
	if err != nil {
		return nil, err
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	request, err := postOwnerRequest(parent, initial, p, operation, requestID)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.PostOwnerWritePolicy{}).DecidePostOwnerWrite(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrPostAuthorDenied
	}
	var authority threadCreateAuthority
	var quote *domain.Post
	if operation == "create" {
		if s.threadCommandSource == nil {
			return nil, ErrPostAuthorUnavailable
		}
		authority, err = s.threadCommandSource.createAuthority(ctx, parent.CategoryID, parent.ThreadType, false)
		if err != nil {
			return nil, err
		}
		if parentID != nil {
			quote, err = s.repo.GetByID(ctx, *parentID)
			if err != nil {
				return nil, err
			}
			if quote.ThreadID != threadID || quote.Status != "published" || quote.DeletedAt != nil {
				return nil, ErrPostAuthorDenied
			}
		}
		postID = strconv.FormatInt(idgen.New(), 10)
	}
	format := "markdown"
	if initial != nil {
		format = initial.ContentFormat
	}
	if operation != "delete" {
		normalized, err := coreeditor.Normalize(content, format)
		if err != nil {
			return nil, ErrPostAuthorInvalid
		}
		content, format = normalized.Content, normalized.Format
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(current, p) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	var result *domain.Post
	code := "community.post.author_" + operation
	event := eventbus.EventPostCreated
	if operation == "edit" {
		event = "post.updated"
	}
	if operation == "delete" {
		event = eventbus.EventPostDeleted
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "post", ResourceID: postID, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(event, "post", postID, map[string]string{"post_id": postID, "thread_id": threadID})
	}}, func(tx context.Context) error {
		currentP, err := bound(tx)
		if err != nil {
			return err
		}
		if operation == "create" {
			currentAuthority, err := s.threadCommandSource.createAuthority(tx, parent.CategoryID, parent.ThreadType, true)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(authority, currentAuthority) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		currentParent, err := threads.LockThreadForWrite(tx, threadID)
		if err != nil {
			return err
		}
		var current *domain.Post
		if initial != nil {
			current, err = posts.LockPostForWrite(tx, postID)
			if err != nil {
				return err
			}
		}
		currentRequest, err := postOwnerRequest(currentParent, current, currentP, operation, requestID)
		if err != nil {
			return err
		}
		if err := policy.CheckPostOwnerWriteDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if quote != nil {
			locked, err := posts.LockPostForWrite(tx, quote.ID)
			if err != nil {
				return err
			}
			if !samePostCommandState(quote, locked) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		expectedReplies := currentParent.ReplyCount
		switch operation {
		case "create":
			now := time.Now().UTC()
			created := &domain.Post{ID: postID, ThreadID: threadID, AuthorID: p.Actor.ID, AuthorName: authorName, ParentID: parentID, Content: content, ContentFormat: format, Status: "published", CreatedAt: now, UpdatedAt: now}
			if err := s.createPost(tx, created); err != nil {
				return err
			}
			expectedReplies++
		case "edit":
			current.Content, current.ContentFormat = content, format
			if err := posts.UpdatePostIfFactsVersion(tx, current, current.FactsVersion); err != nil {
				return err
			}
		case "delete":
			if expectedReplies < 1 {
				return ErrThreadStateConflict
			}
			if err := posts.DeletePostIfFactsVersion(tx, postID, current.FactsVersion, p.Actor); err != nil {
				return err
			}
			if err := s.threadRepo.IncrementReplyCount(tx, threadID, -1); err != nil {
				return err
			}
			expectedReplies--
		}
		written, err := posts.ReadPostCommandSnapshot(tx, postID)
		if err != nil {
			return err
		}
		if written == nil || written.AuthorID != p.Actor.ID || written.ThreadID != threadID || written.FactsVersion < 1 {
			return identityport.ErrPolicyFactsChanged
		}
		if operation == "delete" {
			if written.DeletedAt == nil || written.DeletedActorKind != "user" || written.DeletedActorID != p.Actor.ID {
				return identityport.ErrPolicyFactsChanged
			}
		} else if written.DeletedAt != nil || written.Content != content || written.ContentFormat != format {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := bound(tx); err != nil {
			return err
		}
		if operation == "create" {
			finalAuthority, err := s.threadCommandSource.createAuthority(tx, parent.CategoryID, parent.ThreadType, true)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(authority, finalAuthority) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if quote != nil {
			finalQuote, err := posts.ReadPostCommandSnapshot(tx, quote.ID)
			if err != nil {
				return err
			}
			if !samePostCommandState(quote, finalQuote) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		finalParent, err := threads.LockThreadForWrite(tx, threadID)
		if err != nil {
			return err
		}
		finalFacts, err := publicThreadFacts(finalParent, threadID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(finalFacts, request.Parent) || finalParent.ReplyCount != expectedReplies || finalParent.IsLocked != request.ParentLocked {
			return identityport.ErrPolicyFactsChanged
		}
		finalPost, err := posts.ReadPostCommandSnapshot(tx, postID)
		if err != nil {
			return err
		}
		if !samePostCommandState(written, finalPost) {
			return identityport.ErrPolicyFactsChanged
		}
		result = finalPost
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateThreadListCache(ctx)
	return result, nil
}
