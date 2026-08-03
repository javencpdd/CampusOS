package moderation

import (
	"context"
	"strings"
	"testing"
	"time"

	communitycore "github.com/campusos/CampusOS/internal/modules/core/community"
	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identitysvc "github.com/campusos/CampusOS/internal/modules/core/identity/service"
)

type recordingModerationNotifier struct {
	granted []string
	revoked []string
}

func (n *recordingModerationNotifier) NotifyModeratorScopeGranted(_ context.Context, userID string, categories []communityport.NamedCategory) error {
	n.granted = append(n.granted, userID+":"+joinCategoryIDs(categories))
	return nil
}

func (n *recordingModerationNotifier) NotifyModeratorScopeRevoked(_ context.Context, userID string, categories []communityport.NamedCategory) error {
	n.revoked = append(n.revoked, userID+":"+joinCategoryIDs(categories))
	return nil
}

func joinCategoryIDs(categories []communityport.NamedCategory) string {
	ids := make([]string, 0, len(categories))
	for _, category := range categories {
		ids = append(ids, category.ID)
	}
	return strings.Join(ids, ",")
}

func newModeratorNotificationFixture(t *testing.T) (*Service, *recordingModerationNotifier) {
	t.Helper()
	ctx := context.Background()
	userRepo := identityrepo.NewMemoryUserRepository()
	for _, user := range []*identitydomain.User{
		{ID: "1001", Username: "moderator", Nickname: "Moderator", Email: "moderator@example.test", Status: identitydomain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "9001", Username: "admin", Nickname: "Admin", Email: "admin@example.test", Status: identitydomain.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := userRepo.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	categoryRepo := communityrepo.NewMemoryCategoryRepository()
	for _, category := range []*communitydomain.Category{
		{ID: "10", Name: "Campus News", Slug: "campus-news"},
		{ID: "20", Name: "Marketplace", Slug: "marketplace"},
		{ID: "30", Name: "Lost and Found", Slug: "lost-found"},
	} {
		if err := categoryRepo.Create(ctx, category); err != nil {
			t.Fatalf("create category: %v", err)
		}
	}
	threadRepo := communityrepo.NewMemoryThreadRepository()
	postRepo := communityrepo.NewMemoryPostRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	postSvc := communitysvc.NewPostService(postRepo, nil)
	postSvc.SetThreadRepository(threadRepo)
	permissionSvc := identitysvc.NewPermissionService(identityrepo.NewMemoryRoleRepository(), userRepo)
	if _, err := permissionSvc.AssignRole(ctx, "9001", 1); err != nil {
		t.Fatalf("assign test administrator: %v", err)
	}
	service := NewService(identityport.NewPermissionModerationPolicy(permissionSvc),
		communitycore.NewModerationGateway(categoryRepo, threadRepo, postRepo, threadSvc, postSvc),
		NewMemoryAuditStore(), Config{AllowPin: true, AllowLock: true, AllowDeletePost: true})
	notifier := &recordingModerationNotifier{}
	service.SetNotificationWriter(notifier)
	return service, notifier
}

func TestSetModeratorCategoriesNotifiesGrantAndRevoke(t *testing.T) {
	ctx := context.Background()
	service, notifier := newModeratorNotificationFixture(t)

	if _, err := service.SetModeratorCategories(ctx, "9001", "1001", []string{"10", "20"}, OperationContext{TraceID: "grant"}); err != nil {
		t.Fatalf("grant categories: %v", err)
	}
	if len(notifier.granted) != 1 || notifier.granted[0] != "1001:10,20" {
		t.Fatalf("grant notification mismatch: %#v", notifier.granted)
	}
	if len(notifier.revoked) != 0 {
		t.Fatalf("initial grant must not revoke anything: %#v", notifier.revoked)
	}

	if _, err := service.SetModeratorCategories(ctx, "9001", "1001", []string{"20", "30"}, OperationContext{TraceID: "update"}); err != nil {
		t.Fatalf("update categories: %v", err)
	}
	if len(notifier.granted) != 2 || notifier.granted[1] != "1001:30" {
		t.Fatalf("incremental grant must only cover newly added boards: %#v", notifier.granted)
	}
	if len(notifier.revoked) != 1 || notifier.revoked[0] != "1001:10" {
		t.Fatalf("revoke must only cover removed boards: %#v", notifier.revoked)
	}

	if _, err := service.SetModeratorCategories(ctx, "9001", "1001", []string{"20", "30"}, OperationContext{TraceID: "idempotent"}); err != nil {
		t.Fatalf("idempotent update: %v", err)
	}
	if len(notifier.granted) != 2 || len(notifier.revoked) != 1 {
		t.Fatalf("unchanged assignment must not re-notify: granted=%#v revoked=%#v", notifier.granted, notifier.revoked)
	}

	if _, err := service.SetModeratorCategories(ctx, "9001", "1001", nil, OperationContext{TraceID: "clear"}); err != nil {
		t.Fatalf("clear categories: %v", err)
	}
	if len(notifier.revoked) != 2 || notifier.revoked[1] != "1001:20,30" {
		t.Fatalf("clearing assignment must notify every removed board: %#v", notifier.revoked)
	}
}

// selfAssignablePolicy bypasses the identity-layer self-escalation guard so the
// service-level self-notification suppression can be exercised directly.
type selfAssignablePolicy struct{ scopes map[string][]int64 }

func (p *selfAssignablePolicy) CheckScoped(context.Context, string, string, string, string, int64) (bool, error) {
	return true, nil
}

func (p *selfAssignablePolicy) ListRoleAssignments(_ context.Context, userID, _ string) ([]identityport.RoleAssignment, error) {
	assignments := make([]identityport.RoleAssignment, 0, len(p.scopes[userID]))
	for _, categoryID := range p.scopes[userID] {
		scopeID := categoryID
		assignments = append(assignments, identityport.RoleAssignment{UserID: userID, ScopeType: "category", ScopeID: &scopeID})
	}
	return assignments, nil
}

func (p *selfAssignablePolicy) ReplaceCategoryRoleScopes(_ context.Context, userID, _ string, categoryIDs []int64) (bool, error) {
	return p.replace(userID, categoryIDs)
}

func (p *selfAssignablePolicy) ReplaceCategoryRoleScopesByActor(_ context.Context, _, userID, _ string, categoryIDs []int64) (bool, error) {
	return p.replace(userID, categoryIDs)
}

func (p *selfAssignablePolicy) replace(userID string, categoryIDs []int64) (bool, error) {
	before := p.scopes[userID]
	if len(before) == len(categoryIDs) {
		same := true
		for i := range before {
			if before[i] != categoryIDs[i] {
				same = false
				break
			}
		}
		if same {
			return false, nil
		}
	}
	p.scopes[userID] = append([]int64{}, categoryIDs...)
	return true, nil
}

func TestSetModeratorCategoriesSkipsSelfNotification(t *testing.T) {
	ctx := context.Background()
	categoryRepo := communityrepo.NewMemoryCategoryRepository()
	if err := categoryRepo.Create(ctx, &communitydomain.Category{ID: "10", Name: "Campus News", Slug: "campus-news"}); err != nil {
		t.Fatalf("create category: %v", err)
	}
	threadRepo := communityrepo.NewMemoryThreadRepository()
	postRepo := communityrepo.NewMemoryPostRepository()
	threadSvc := communitysvc.NewThreadService(threadRepo, nil)
	postSvc := communitysvc.NewPostService(postRepo, nil)
	postSvc.SetThreadRepository(threadRepo)
	service := NewService(&selfAssignablePolicy{scopes: map[string][]int64{}},
		communitycore.NewModerationGateway(categoryRepo, threadRepo, postRepo, threadSvc, postSvc),
		NewMemoryAuditStore(), Config{AllowPin: true, AllowLock: true, AllowDeletePost: true})
	notifier := &recordingModerationNotifier{}
	service.SetNotificationWriter(notifier)

	if _, err := service.SetModeratorCategories(ctx, "9001", "9001", []string{"10"}, OperationContext{TraceID: "self-grant"}); err != nil {
		t.Fatalf("self grant: %v", err)
	}
	if len(notifier.granted) != 0 || len(notifier.revoked) != 0 {
		t.Fatalf("operator changing own assignment must not self-notify: granted=%#v revoked=%#v", notifier.granted, notifier.revoked)
	}
}
