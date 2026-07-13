package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	communitycore "github.com/campusos/CampusOS/internal/community"
	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/core/identity/service"
)

func TestCategoryModeratorCanOnlyGovernAssignedCategories(t *testing.T) {
	ctx := context.Background()
	userRepo := identityrepo.NewMemoryUserRepository()
	if err := userRepo.Create(ctx, &identitydomain.User{
		ID: "1001", Username: "moderator", Nickname: "Moderator",
		Email: "moderator@example.test", Status: identitydomain.UserStatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	categoryRepo := communityrepo.NewMemoryCategoryRepository()
	for _, category := range []*communitydomain.Category{
		{ID: "10", Name: "Campus News", Slug: "campus-news"},
		{ID: "20", Name: "Marketplace", Slug: "marketplace"},
	} {
		if err := categoryRepo.Create(ctx, category); err != nil {
			t.Fatalf("create category: %v", err)
		}
	}
	threadRepo := communityrepo.NewMemoryThreadRepository()
	for _, thread := range []*communitydomain.Thread{
		{ID: "101", Title: "News", CategoryID: "10", Status: communitydomain.ThreadStatusPublished},
		{ID: "102", Title: "Sale", CategoryID: "20", Status: communitydomain.ThreadStatusPublished},
	} {
		if err := threadRepo.Create(ctx, thread); err != nil {
			t.Fatalf("create thread: %v", err)
		}
	}
	postRepo := communityrepo.NewMemoryPostRepository()
	if err := postRepo.Create(ctx, &communitydomain.Post{
		ID: "201", ThreadID: "101", AuthorID: "2001", AuthorName: "Alice", Content: "reply",
		Status: "published", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create post: %v", err)
	}
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	postSvc := communitysvc.NewPostService(postRepo, nil)
	postSvc.SetThreadRepository(threadRepo)
	audit := NewMemoryAuditStore()
	permissionSvc := identitysvc.NewPermissionService(identityrepo.NewMemoryRoleRepository(), userRepo)
	service := NewService(identityport.NewPermissionModerationPolicy(permissionSvc), communitycore.NewModerationGateway(categoryRepo, threadRepo, postRepo, threadSvc, postSvc), audit, Config{
		AllowPin: true, AllowLock: true, AllowDeletePost: true,
	})

	assignment, err := service.SetModeratorCategories(ctx, "9001", "1001", []string{"10"}, OperationContext{TraceID: "scope-test"})
	if err != nil {
		t.Fatalf("set moderator categories: %v", err)
	}
	if len(assignment.CategoryIDs) != 1 || assignment.CategoryIDs[0] != "10" {
		t.Fatalf("unexpected assignment: %#v", assignment)
	}

	access, err := service.AccessForThread(ctx, "1001", "101")
	if err != nil {
		t.Fatalf("get assigned access: %v", err)
	}
	if !access.CanModerate || !access.Actions.Pin || !access.Actions.Lock || !access.Actions.DeletePost {
		t.Fatalf("expected all configured actions in assigned category: %#v", access)
	}
	outside, err := service.AccessForThread(ctx, "1001", "102")
	if err != nil {
		t.Fatalf("get outside access: %v", err)
	}
	if outside.CanModerate {
		t.Fatalf("unexpected access outside assigned category: %#v", outside)
	}

	updated, err := service.SetPinned(ctx, "1001", "101", true, OperationContext{TraceID: "pin-test"})
	if err != nil || !updated.IsPinned {
		t.Fatalf("pin assigned thread: thread=%#v err=%v", updated, err)
	}
	if _, err := service.SetPinned(ctx, "1001", "102", true, OperationContext{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected outside category denial, got %v", err)
	}
	if err := service.DeletePost(ctx, "1001", "101", "201", OperationContext{TraceID: "post-delete-test"}); err != nil {
		t.Fatalf("delete post in assigned category: %v", err)
	}
	if len(audit.Records()) < 3 {
		t.Fatalf("expected scope, pin and post audit records, got %#v", audit.Records())
	}

	liveConfig := Config{AllowPin: false, AllowLock: true, AllowDeletePost: true}
	service.SetConfigProvider(func() Config { return liveConfig })
	hotUpdated, err := service.AccessForThread(ctx, "1001", "101")
	if err != nil {
		t.Fatalf("get hot-updated access: %v", err)
	}
	if hotUpdated.Actions.Pin || !hotUpdated.Actions.Lock {
		t.Fatalf("expected hot-updated action switches: %#v", hotUpdated.Actions)
	}
	if _, err := service.SetPinned(ctx, "1001", "101", false, OperationContext{}); !errors.Is(err, ErrActionDisabled) {
		t.Fatalf("expected hot-disabled pin action, got %v", err)
	}
	liveConfig.AllowPin = true
	if _, err := service.SetPinned(ctx, "1001", "101", false, OperationContext{}); err != nil {
		t.Fatalf("expected hot-enabled pin action: %v", err)
	}

	service.SetEnabledChecker(func() bool { return false })
	disabled, err := service.AccessForThread(ctx, "1001", "101")
	if err != nil {
		t.Fatalf("get disabled access: %v", err)
	}
	if disabled.PluginEnabled || disabled.CanModerate {
		t.Fatalf("disabled plugin must remove effective moderation: %#v", disabled)
	}
	if _, err := service.SetLocked(ctx, "1001", "101", true, OperationContext{}); !errors.Is(err, ErrPluginDisabled) {
		t.Fatalf("expected disabled plugin rejection, got %v", err)
	}
}
