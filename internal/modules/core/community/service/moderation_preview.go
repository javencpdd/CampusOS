package service

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

func validateModerationPreview(threadID string, postIDs []string) error {
	if threadID == "" || strings.TrimSpace(threadID) != threadID || len(threadID) > 128 || len(postIDs) > communityport.MaxModerationPreviewPosts {
		return communityport.ErrModerationPreviewInvalid
	}
	seen := map[string]bool{}
	for _, id := range postIDs {
		if id == "" || strings.TrimSpace(id) != id || len(id) > 128 || seen[id] {
			return communityport.ErrModerationPreviewInvalid
		}
		seen[id] = true
	}
	return nil
}

type moderationPreviewSnapshot struct {
	Thread    *domain.Thread
	Posts     []*domain.Post
	Pin, Lock identityport.UserScopedExecutionAuthority
	Delete    identityport.BoardGovernanceAuthority
	Config    communityport.ModerationPreviewConfig
}

func detachedModerationPreviewPost(row *domain.Post) *domain.Post {
	if row == nil {
		return nil
	}
	copy := *row
	copy.Metadata = append(copy.Metadata[:0:0], row.Metadata...)
	if row.ParentID != nil {
		id := *row.ParentID
		copy.ParentID = &id
	}
	if row.DeletedAt != nil {
		at := *row.DeletedAt
		copy.DeletedAt = &at
	}
	return &copy
}

func (s *PostService) loadModerationPreview(ctx context.Context, threadID string, ids []string, resolve identityport.SessionPrincipalResolver, config func(context.Context) (communityport.ModerationPreviewConfig, error)) (moderationPreviewSnapshot, error) {
	var snapshot moderationPreviewSnapshot
	threads := s.threadRepo.(repository.ModerationThreadSnapshotReader)
	posts := s.repo.(repository.FactsGuardedPostRepository)
	current, err := config(ctx)
	if err != nil {
		return snapshot, err
	}
	parent, err := threads.ReadModerationThreadSnapshot(ctx, threadID)
	if err != nil {
		return snapshot, err
	}
	if parent == nil {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	reader := s.threadCommandSource.userExecution
	query := identityport.UserScopedExecutionQuery{PermissionCode: "community.thread.pin", Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: parent.CategoryID}}
	pin, err := reader.LoadUserScopedExecutionAuthority(ctx, query, resolve, false)
	if err != nil {
		return snapshot, err
	}
	query.PermissionCode = "community.thread.lock"
	lock, err := reader.LoadUserScopedExecutionAuthority(ctx, query, resolve, false)
	if err != nil {
		return snapshot, err
	}
	if !reflect.DeepEqual(pin.Principal, lock.Principal) || pin.Principal.Actor.Kind != "user" {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	snapshot = moderationPreviewSnapshot{Thread: detachedGovernanceThread(parent), Pin: pin, Lock: lock, Config: current, Posts: []*domain.Post{}}
	if len(ids) > 0 {
		grant, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, identityport.BoardGovernanceQuery{BoardID: parent.CategoryID, Action: "community.post.delete"}, resolve, false)
		if err != nil {
			return snapshot, err
		}
		if !reflect.DeepEqual(pin.Principal, grant.Principal) {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
		snapshot.Delete = grant
	}
	for _, id := range ids {
		row, err := posts.ReadPostCommandSnapshot(ctx, id)
		if err != nil {
			return snapshot, err
		}
		if row == nil || row.ThreadID != parent.ID || row.ID != id {
			return snapshot, communityport.ErrModerationPreviewInvalid
		}
		snapshot.Posts = append(snapshot.Posts, detachedModerationPreviewPost(row))
	}
	// Resources may call a storage adapter while a credential, assignment,
	// delegation or module switch changes. Re-prove those after all item reads.
	query.PermissionCode = "community.thread.pin"
	finalPin, err := reader.LoadUserScopedExecutionAuthority(ctx, query, resolve, false)
	if err != nil {
		return snapshot, err
	}
	query.PermissionCode = "community.thread.lock"
	finalLock, err := reader.LoadUserScopedExecutionAuthority(ctx, query, resolve, false)
	if err != nil {
		return snapshot, err
	}
	if identityport.CheckUserScopedExecutionAuthority(pin, finalPin) != nil || identityport.CheckUserScopedExecutionAuthority(lock, finalLock) != nil {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	if len(ids) > 0 {
		finalGrant, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, identityport.BoardGovernanceQuery{BoardID: parent.CategoryID, Action: "community.post.delete"}, resolve, false)
		if err != nil {
			return snapshot, err
		}
		if identityport.CheckBoardGovernanceAuthority(snapshot.Delete, finalGrant) != nil {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
	}
	finalParent, err := threads.ReadModerationThreadSnapshot(ctx, threadID)
	if err != nil {
		return snapshot, err
	}
	finalConfig, err := config(ctx)
	if err != nil {
		return snapshot, err
	}
	if finalConfig != current || !sameGovernanceThread(parent, finalParent) {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	return snapshot, nil
}

func sameModerationPreviewSnapshot(initial, current moderationPreviewSnapshot) bool {
	if !sameGovernanceThread(initial.Thread, current.Thread) || initial.Config != current.Config || identityport.CheckUserScopedExecutionAuthority(initial.Pin, current.Pin) != nil || identityport.CheckUserScopedExecutionAuthority(initial.Lock, current.Lock) != nil || len(initial.Posts) != len(current.Posts) {
		return false
	}
	if len(initial.Posts) > 0 && identityport.CheckBoardGovernanceAuthority(initial.Delete, current.Delete) != nil {
		return false
	}
	for i, post := range initial.Posts {
		if !samePostCommandState(post, current.Posts[i]) {
			return false
		}
	}
	return true
}

// PreviewUserModeration shares the command policies and current authority
// readers. Its low-sensitive booleans are never execution credentials.
func (s *PostService) PreviewUserModeration(ctx context.Context, threadID string, postIDs []string, resolve identityport.SessionPrincipalResolver, config func(context.Context) (communityport.ModerationPreviewConfig, error)) (*communityport.UserModerationPreview, error) {
	if err := validateModerationPreview(threadID, postIDs); err != nil {
		return nil, err
	}
	ids := append([]string(nil), postIDs...)
	if s == nil || s.threadCommandSource == nil || nilGovernancePort(s.threadCommandSource.userExecution) || config == nil || nilGovernancePort(s.governanceAuthority) {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	if threads, ok := s.threadRepo.(repository.ModerationThreadSnapshotReader); !ok || nilGovernancePort(threads) {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	if posts, ok := s.repo.(repository.FactsGuardedPostRepository); !ok || nilGovernancePort(posts) {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	initial, err := s.loadModerationPreview(ctx, threadID, ids, resolve, config)
	if err != nil {
		return nil, err
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	evaluatedAt := time.Now().UnixMilli()
	result := &communityport.UserModerationPreview{ThreadID: initial.Thread.ID, Posts: []communityport.PostModerationPreview{}}
	for _, action := range []struct {
		op        string
		authority identityport.UserScopedExecutionAuthority
		enabled   bool
	}{{"pin", initial.Pin, initial.Config.Pin}, {"lock", initial.Lock, initial.Config.Lock}} {
		request, err := userFlagRequest(initial.Thread, action.authority, action.op, requestID, evaluatedAt)
		if err != nil {
			return nil, err
		}
		decision, err := (policy.ThreadUserFlagPolicy{}).DecideThreadUserFlag(request)
		if err != nil {
			return nil, err
		}
		if action.op == "pin" {
			result.Pin = action.enabled && decision.Effect == "allow"
		} else {
			result.Lock = action.enabled && decision.Effect == "allow"
		}
	}
	for _, post := range initial.Posts {
		request, err := postDeleteRequest(initial.Thread, post, initial.Delete, requestID)
		if err != nil {
			return nil, err
		}
		decision, err := (policy.ResourcePolicy{}).DecideResource(request)
		if err != nil {
			return nil, err
		}
		result.Posts = append(result.Posts, communityport.PostModerationPreview{ID: post.ID, Delete: initial.Config.DeletePost && decision.Effect == "allow"})
	}
	current, err := s.loadModerationPreview(ctx, threadID, ids, resolve, config)
	if err != nil {
		return nil, err
	}
	if !sameModerationPreviewSnapshot(initial, current) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	// After re-reading every resource, prove the whole batch once more. A change
	// to an earlier item while loading a later one cannot escape the return check.
	final, err := s.loadModerationPreview(ctx, threadID, ids, resolve, config)
	if err != nil {
		return nil, err
	}
	if !sameModerationPreviewSnapshot(current, final) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
