package moderation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"

	communitydomain "github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	identityport "github.com/campusos/CampusOS/internal/core/identity/port"
)

const PluginName = "category-moderation"

var (
	ErrPluginDisabled = errors.New("category moderation compatibility feature is disabled")
	ErrActionDisabled = errors.New("moderation action is disabled")
	ErrForbidden      = errors.New("moderation scope denied")
	ErrInvalidScope   = errors.New("invalid moderation category scope")
)

type Config struct {
	AllowPin        bool `json:"allow_pin"`
	AllowLock       bool `json:"allow_lock"`
	AllowDeletePost bool `json:"allow_delete_post"`
}

func ConfigFromPluginConfig(values map[string]interface{}) Config {
	return Config{
		AllowPin:        boolConfig(values, "allow_pin", true),
		AllowLock:       boolConfig(values, "allow_lock", true),
		AllowDeletePost: boolConfig(values, "allow_delete_post", true),
	}
}

type CategoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ModeratorAssignment struct {
	UserID      string        `json:"user_id"`
	CategoryIDs []string      `json:"category_ids"`
	Categories  []CategoryRef `json:"categories"`
}

type ActionSet struct {
	Pin        bool `json:"pin"`
	Lock       bool `json:"lock"`
	DeletePost bool `json:"delete_post"`
}

type Access struct {
	PluginEnabled bool         `json:"plugin_enabled"`
	IsModerator   bool         `json:"is_moderator"`
	CanModerate   bool         `json:"can_moderate"`
	Category      *CategoryRef `json:"category,omitempty"`
	CategoryIDs   []string     `json:"category_ids"`
	Actions       ActionSet    `json:"actions"`
	Config        Config       `json:"config"`
}

type OperationContext struct {
	TraceID   string
	IPAddress string
}

type Service struct {
	permissions identityport.ModerationPolicy
	community   communityport.ModerationGateway
	audit       AuditStore
	config      Config
	configFn    func() Config
	enabled     func() bool
}

func NewService(
	permissions identityport.ModerationPolicy,
	community communityport.ModerationGateway,
	audit AuditStore,
	config Config,
) *Service {
	if audit == nil {
		audit = NewMemoryAuditStore()
	}
	return &Service{
		permissions: permissions,
		community:   community,
		audit:       audit,
		config:      config,
		enabled:     func() bool { return true },
	}
}

func (s *Service) SetEnabledChecker(checker func() bool) {
	if checker != nil {
		s.enabled = checker
	}
}

// SetConfigProvider enables hot updates for action switches while preserving
// the restart-based lifecycle of the system-level plugin itself.
func (s *Service) SetConfigProvider(provider func() Config) {
	if provider != nil {
		s.configFn = provider
	}
}

func (s *Service) currentConfig() Config {
	if s.configFn != nil {
		return s.configFn()
	}
	return s.config
}

func (s *Service) Status() map[string]interface{} {
	return map[string]interface{}{
		"module":     ModuleID,
		"plugin":     PluginName,
		"enabled":    s.enabled(),
		"config":     s.currentConfig(),
		"scope_type": "category",
	}
}

func (s *Service) ListModerators(ctx context.Context) ([]ModeratorAssignment, error) {
	assignments, err := s.permissions.ListRoleAssignments(ctx, "", "moderator")
	if err != nil {
		return nil, err
	}
	grouped := make(map[string][]int64)
	for _, assignment := range assignments {
		if assignment.ScopeType != "category" || assignment.ScopeID == nil {
			continue
		}
		grouped[assignment.UserID] = append(grouped[assignment.UserID], *assignment.ScopeID)
	}
	result := make([]ModeratorAssignment, 0, len(grouped))
	for userID, categoryIDs := range grouped {
		assignment, err := s.assignmentFromIDs(ctx, userID, categoryIDs)
		if err != nil {
			return nil, err
		}
		result = append(result, assignment)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
	return result, nil
}

func (s *Service) GetModerator(ctx context.Context, userID string) (ModeratorAssignment, error) {
	assignments, err := s.permissions.ListRoleAssignments(ctx, userID, "moderator")
	if err != nil {
		return ModeratorAssignment{}, err
	}
	categoryIDs := make([]int64, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.ScopeType == "category" && assignment.ScopeID != nil {
			categoryIDs = append(categoryIDs, *assignment.ScopeID)
		}
	}
	return s.assignmentFromIDs(ctx, userID, categoryIDs)
}

func (s *Service) SetModeratorCategories(ctx context.Context, actorID, userID string, categoryIDs []string, operation OperationContext) (ModeratorAssignment, error) {
	parsedIDs, refs, err := s.validateCategories(ctx, categoryIDs)
	if err != nil {
		return ModeratorAssignment{}, err
	}
	before, err := s.GetModerator(ctx, userID)
	if err != nil {
		return ModeratorAssignment{}, err
	}
	changed, err := s.permissions.ReplaceCategoryRoleScopes(ctx, userID, "moderator", parsedIDs)
	if err != nil {
		return ModeratorAssignment{}, err
	}
	after := ModeratorAssignment{UserID: userID, CategoryIDs: normalizedCategoryStrings(parsedIDs), Categories: refs}
	if changed {
		s.writeAudit(ctx, AuditRecord{
			TraceID: operation.TraceID, ActorID: actorID, Action: "moderator.scope.update",
			Resource: "user", ResourceID: userID, Before: before, After: after,
			Metadata: map[string]interface{}{"scope_type": "category"}, IPAddress: operation.IPAddress,
		})
	}
	return after, nil
}

func (s *Service) AccessForThread(ctx context.Context, userID, threadID string) (Access, error) {
	config := s.currentConfig()
	access := Access{PluginEnabled: s.enabled(), Config: config, CategoryIDs: []string{}}
	assignments, err := s.permissions.ListRoleAssignments(ctx, userID, "moderator")
	if err != nil {
		return access, err
	}
	for _, assignment := range assignments {
		if assignment.ScopeType == "category" && assignment.ScopeID != nil {
			access.CategoryIDs = append(access.CategoryIDs, strconv.FormatInt(*assignment.ScopeID, 10))
		}
	}
	sort.Strings(access.CategoryIDs)
	access.IsModerator = len(access.CategoryIDs) > 0
	if !access.PluginEnabled {
		return access, nil
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return access, err
	}
	categoryID, err := strconv.ParseInt(thread.CategoryID, 10, 64)
	if err != nil || categoryID <= 0 {
		return access, ErrInvalidScope
	}
	category, err := s.community.GetCategory(ctx, thread.CategoryID)
	if err != nil {
		return access, err
	}
	access.Category = &CategoryRef{ID: category.ID, Name: category.Name}
	access.Actions.Pin, err = s.allowed(ctx, userID, "thread", "pin", categoryID, config.AllowPin)
	if err != nil {
		return access, err
	}
	access.Actions.Lock, err = s.allowed(ctx, userID, "thread", "lock", categoryID, config.AllowLock)
	if err != nil {
		return access, err
	}
	access.Actions.DeletePost, err = s.allowed(ctx, userID, "post", "delete", categoryID, config.AllowDeletePost)
	if err != nil {
		return access, err
	}
	access.CanModerate = access.Actions.Pin || access.Actions.Lock || access.Actions.DeletePost
	return access, nil
}

func (s *Service) SetPinned(ctx context.Context, actorID, threadID string, pinned bool, operation OperationContext) (*communitydomain.Thread, error) {
	thread, err := s.authorizeThread(ctx, actorID, threadID, "thread", "pin", s.currentConfig().AllowPin)
	if err != nil {
		return nil, err
	}
	before := map[string]interface{}{"is_pinned": thread.IsPinned}
	updated, err := s.community.SetPinned(ctx, threadID, pinned)
	if err != nil {
		return nil, err
	}
	s.writeAudit(ctx, AuditRecord{
		TraceID: operation.TraceID, ActorID: actorID, Action: actionName("thread", "pin", pinned),
		Resource: "thread", ResourceID: threadID, Before: before,
		After:    map[string]interface{}{"is_pinned": updated.IsPinned},
		Metadata: map[string]interface{}{"category_id": updated.CategoryID}, IPAddress: operation.IPAddress,
	})
	return updated, nil
}

func (s *Service) SetLocked(ctx context.Context, actorID, threadID string, locked bool, operation OperationContext) (*communitydomain.Thread, error) {
	thread, err := s.authorizeThread(ctx, actorID, threadID, "thread", "lock", s.currentConfig().AllowLock)
	if err != nil {
		return nil, err
	}
	before := map[string]interface{}{"is_locked": thread.IsLocked}
	updated, err := s.community.SetLocked(ctx, threadID, locked)
	if err != nil {
		return nil, err
	}
	s.writeAudit(ctx, AuditRecord{
		TraceID: operation.TraceID, ActorID: actorID, Action: actionName("thread", "lock", locked),
		Resource: "thread", ResourceID: threadID, Before: before,
		After:    map[string]interface{}{"is_locked": updated.IsLocked},
		Metadata: map[string]interface{}{"category_id": updated.CategoryID}, IPAddress: operation.IPAddress,
	})
	return updated, nil
}

func (s *Service) DeletePost(ctx context.Context, actorID, threadID, postID string, operation OperationContext) error {
	thread, err := s.authorizeThread(ctx, actorID, threadID, "post", "delete", s.currentConfig().AllowDeletePost)
	if err != nil {
		return err
	}
	post, err := s.community.GetPost(ctx, postID)
	if err != nil {
		return err
	}
	if post.ThreadID != threadID {
		return ErrInvalidScope
	}
	if err := s.community.DeletePostForModeration(ctx, postID); err != nil {
		return err
	}
	s.writeAudit(ctx, AuditRecord{
		TraceID: operation.TraceID, ActorID: actorID, Action: "post.delete",
		Resource: "post", ResourceID: postID,
		Before:   map[string]interface{}{"thread_id": post.ThreadID, "author_id": post.AuthorID, "floor_number": post.FloorNumber},
		After:    map[string]interface{}{"deleted": true},
		Metadata: map[string]interface{}{"category_id": thread.CategoryID, "thread_id": threadID}, IPAddress: operation.IPAddress,
	})
	return nil
}

func (s *Service) authorizeThread(ctx context.Context, userID, threadID, resource, action string, configEnabled bool) (*communitydomain.Thread, error) {
	if !s.enabled() {
		return nil, ErrPluginDisabled
	}
	if !configEnabled {
		return nil, ErrActionDisabled
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	categoryID, err := strconv.ParseInt(thread.CategoryID, 10, 64)
	if err != nil || categoryID <= 0 {
		return nil, ErrInvalidScope
	}
	allowed, err := s.permissions.CheckScoped(ctx, userID, resource, action, "category", categoryID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	return thread, nil
}

func (s *Service) allowed(ctx context.Context, userID, resource, action string, categoryID int64, configEnabled bool) (bool, error) {
	if !configEnabled {
		return false, nil
	}
	return s.permissions.CheckScoped(ctx, userID, resource, action, "category", categoryID)
}

func (s *Service) validateCategories(ctx context.Context, categoryIDs []string) ([]int64, []CategoryRef, error) {
	seen := make(map[int64]bool, len(categoryIDs))
	parsed := make([]int64, 0, len(categoryIDs))
	refs := make([]CategoryRef, 0, len(categoryIDs))
	for _, value := range categoryIDs {
		categoryID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || categoryID <= 0 {
			return nil, nil, ErrInvalidScope
		}
		if seen[categoryID] {
			continue
		}
		category, err := s.community.GetCategory(ctx, value)
		if err != nil {
			return nil, nil, fmt.Errorf("get category %s: %w", value, err)
		}
		seen[categoryID] = true
		parsed = append(parsed, categoryID)
		refs = append(refs, CategoryRef{ID: category.ID, Name: category.Name})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i] < parsed[j] })
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return parsed, refs, nil
}

func (s *Service) assignmentFromIDs(ctx context.Context, userID string, categoryIDs []int64) (ModeratorAssignment, error) {
	ids := uniqueIDs(categoryIDs)
	result := ModeratorAssignment{UserID: userID, CategoryIDs: normalizedCategoryStrings(ids), Categories: []CategoryRef{}}
	for _, categoryID := range ids {
		category, err := s.community.GetCategory(ctx, strconv.FormatInt(categoryID, 10))
		if err != nil {
			if errors.Is(err, communityport.ErrCategoryNotFound) {
				continue
			}
			return ModeratorAssignment{}, err
		}
		result.Categories = append(result.Categories, CategoryRef{ID: category.ID, Name: category.Name})
	}
	return result, nil
}

func (s *Service) writeAudit(ctx context.Context, record AuditRecord) {
	if err := s.audit.Log(ctx, record); err != nil {
		log.Printf("moderation audit write failed: action=%s resource=%s/%s err=%v", record.Action, record.Resource, record.ResourceID, err)
	}
}

func boolConfig(values map[string]interface{}, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	value, ok := values[key]
	if !ok {
		return fallback
	}
	parsed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return parsed
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func normalizedCategoryStrings(ids []int64) []string {
	ids = uniqueIDs(ids)
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strconv.FormatInt(id, 10))
	}
	return result
}

func actionName(resource, action string, enabled bool) string {
	if enabled {
		return resource + "." + action
	}
	return resource + ".un" + action
}
