package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

func (s *PostService) ListPostsForPrincipal(ctx context.Context, threadID string, page, size int, resolve identityport.SessionPrincipalResolver) ([]*domain.Post, int64, error) {
	return s.readPostsForPrincipal(ctx, threadID, "", page, size, resolve)
}
func (s *PostService) GetPostForPrincipal(ctx context.Context, threadID, id string, resolve identityport.SessionPrincipalResolver) (*domain.Post, error) {
	if id == "" {
		return nil, repository.ErrPostNotFound
	}
	posts, _, err := s.readPostsForPrincipal(ctx, threadID, id, 1, 1, resolve)
	if err != nil {
		return nil, err
	}
	if len(posts) != 1 {
		return nil, repository.ErrPostNotFound
	}
	return posts[0], nil
}
func postReadPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	if resolve == nil {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}, nil
	}
	return resolveAuthorListPrincipal(ctx, resolve)
}
func postReadRequest(parent *domain.Thread, p identityport.PrincipalContext, post *domain.Post, requestID string) (identityport.PostReadRequest, error) {
	if parent == nil {
		return identityport.PostReadRequest{}, repository.ErrThreadNotFound
	}
	facts, err := publicThreadFacts(parent, parent.ID)
	if err != nil {
		return identityport.PostReadRequest{}, err
	}
	r := identityport.PostReadRequest{Contract: identityport.PostReadContract, PolicyVersion: identityport.PostReadPolicyVersion, RequestID: requestID, Operation: "list", Principal: p, Parent: facts}
	if post != nil {
		r.Operation = "detail"
		r.Post = &identityport.PostOwnerWriteFacts{ID: post.ID, ThreadID: post.ThreadID, OwnerID: post.AuthorID, Status: post.Status, Version: post.FactsVersion, Deleted: post.DeletedAt != nil}
	}
	return r, r.Validate()
}
func (s *PostService) readPostsForPrincipal(ctx context.Context, threadID, postID string, page, size int, resolve identityport.SessionPrincipalResolver) ([]*domain.Post, int64, error) {
	if s == nil || nilGovernancePort(s.threadRepo) {
		return nil, 0, identityport.ErrPolicyDecisionMismatch
	}
	snapshots, ok := s.repo.(repository.PostReadSnapshotRepository)
	if !ok || nilGovernancePort(snapshots) {
		return nil, 0, identityport.ErrPolicyDecisionMismatch
	}
	// Strict bounded pagination prevents overflow before any repository call.
	if page < 1 || size < 1 || size > 100 || int64(page-1) > (1<<63-1)/int64(size) {
		return nil, 0, repository.ErrThreadPaginationOverflow
	}
	p, err := postReadPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	parent, err := s.threadRepo.GetByID(ctx, threadID)
	if err != nil {
		return nil, 0, err
	}
	request, err := postReadRequest(parent, p, nil, strconv.FormatInt(idgen.New(), 10))
	if err != nil {
		return nil, 0, err
	}
	decision, err := (policy.PostReadPolicy{}).DecidePostRead(request)
	if err != nil {
		return nil, 0, err
	}
	if err := identityport.CheckPostReadDecision(request, decision, request); err != nil {
		if errors.Is(err, identityport.ErrPolicyResourceNotPublic) {
			return nil, 0, repository.ErrThreadNotFound
		}
		return nil, 0, err
	}
	viewer := ""
	if p.Actor.Kind == "user" {
		viewer = p.Actor.ID
	}
	initial, total, err := snapshots.ReadPublishedPostSnapshot(ctx, threadID, parent.FactsVersion, viewer, postID, page, size)
	if err != nil {
		return nil, 0, err
	}
	if err := validatePostReadPage(initial, total, threadID, page, size, request); err != nil {
		return nil, 0, err
	}
	before, err := postReadPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	if !reflect.DeepEqual(p, before) {
		return nil, 0, identityport.ErrPolicyFactsChanged
	}
	current, currentTotal, err := snapshots.ReadPublishedPostSnapshot(ctx, threadID, parent.FactsVersion, viewer, postID, page, size)
	if err != nil {
		return nil, 0, err
	}
	if err := validatePostReadPage(current, currentTotal, threadID, page, size, request); err != nil {
		return nil, 0, err
	}
	after, err := postReadPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	finalParent, err := s.threadRepo.GetByID(ctx, threadID)
	if err != nil {
		return nil, 0, err
	}
	finalRequest, err := postReadRequest(finalParent, after, nil, request.RequestID)
	if err != nil {
		return nil, 0, err
	}
	if err := identityport.CheckPostReadDecision(request, decision, finalRequest); err != nil {
		return nil, 0, err
	}
	if total != currentTotal || len(initial) != len(current) {
		return nil, 0, identityport.ErrPolicyFactsChanged
	}
	for i := range current {
		if current[i].FactsVersion != initial[i].FactsVersion || !samePostCommandState(initial[i], current[i]) {
			return nil, 0, identityport.ErrPolicyFactsChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	return current, currentTotal, nil
}
func validatePostReadPage(posts []*domain.Post, total int64, threadID string, page, size int, list identityport.PostReadRequest) error {
	offset := int64(page-1) * int64(size)
	if total < 0 || int64(len(posts)) != max(int64(0), min(total-offset, int64(size))) {
		return identityport.ErrPolicyDecisionMismatch
	}
	seen := map[string]bool{}
	for _, post := range posts {
		if post == nil || post.ThreadID != threadID || seen[post.ID] {
			return identityport.ErrPolicyDecisionMismatch
		}
		seen[post.ID] = true
		// Reuse the already validated actual parent facts, without fabricating a parent row.
		r := list
		r.Operation = "detail"
		r.Post = &identityport.PostOwnerWriteFacts{ID: post.ID, ThreadID: post.ThreadID, OwnerID: post.AuthorID, Status: post.Status, Version: post.FactsVersion, Deleted: post.DeletedAt != nil}
		d, err := (policy.PostReadPolicy{}).DecidePostRead(r)
		if err != nil {
			return err
		}
		if err := identityport.CheckPostReadDecision(r, d, r); err != nil {
			return err
		}
	}
	return nil
}
