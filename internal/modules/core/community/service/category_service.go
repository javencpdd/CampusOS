package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrCategoryHierarchy          = errors.New("category hierarchy is invalid")
	ErrCategoryArchived           = errors.New("category is archived")
	ErrCategoryVersionRequired    = errors.New("category version is required")
	ErrCategoryPostingUnavailable = errors.New("category is not available for new content")
)

var categoryColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$`)

// CategoryService owns the core Community category write boundary. It keeps
// tree validation, version checks, audit/outbox commands and legacy EventBus
// compatibility in one place instead of leaving handlers to compose writes.
type CategoryService struct {
	repo         repository.CategoryRepository
	threadRepo   repository.ThreadRepository
	typePolicies repository.ThreadTypePolicyRepository
	bus          eventbus.EventBus
	reliable     *reliability.Service
}

func NewCategoryService(repo repository.CategoryRepository, bus eventbus.EventBus) *CategoryService {
	return &CategoryService{repo: repo, bus: bus}
}

func (s *CategoryService) SetThreadRepository(repo repository.ThreadRepository) {
	s.threadRepo = repo
}

func (s *CategoryService) SetThreadTypePolicyRepository(repo repository.ThreadTypePolicyRepository) {
	s.typePolicies = repo
	if s.reliable != nil {
		if snapshotter, ok := repo.(transaction.Snapshotter); ok {
			s.reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *CategoryService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.repo.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.typePolicies.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// Create remains the trusted in-process compatibility entry point. HTTP
// handlers call CreateForActor so the reliable command carries the operator.
func (s *CategoryService) Create(ctx context.Context, req domain.CreateCategoryRequest) (*domain.Category, error) {
	return s.CreateForActor(ctx, "", req)
}

func (s *CategoryService) CreateForActor(ctx context.Context, actorID string, req domain.CreateCategoryRequest) (*domain.Category, error) {
	cat, err := newCategory(req)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, actorID, "community.category.create", cat.ID, eventbus.EventCategoryCreated, func(commandCtx context.Context) (*domain.Category, error) {
		if err := s.validateParentForCategory(commandCtx, cat, cat.ParentID); err != nil {
			return nil, err
		}
		if err := s.repo.Create(commandCtx, cat); err != nil {
			return nil, fmt.Errorf("create category: %w", err)
		}
		if cat.NodeKind == domain.CategoryNodeBoard && s.typePolicies != nil {
			if err := s.typePolicies.Replace(commandCtx, cat.ID, domain.DefaultCategoryThreadTypes()); err != nil {
				return nil, fmt.Errorf("seed category thread type policy: %w", err)
			}
		}
		return cat, nil
	})
}

// GetByID is the public read path. Archived categories remain available to
// administrators through GetAdminByID but are intentionally invisible to
// ordinary category navigation and posting forms.
func (s *CategoryService) GetByID(ctx context.Context, id string) (*domain.Category, error) {
	cat, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	cat.NormalizeHierarchy()
	if cat.LifecycleStatus != domain.CategoryLifecycleActive {
		return nil, repository.ErrCategoryNotFound
	}
	return cat, nil
}

func (s *CategoryService) GetAdminByID(ctx context.Context, id string) (*domain.Category, error) {
	cat, err := s.repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	cat.NormalizeHierarchy()
	return cat, nil
}

// List retains the historical flat public response contract while filtering
// archived nodes. Tree clients use ListTree instead.
func (s *CategoryService) List(ctx context.Context) ([]*domain.Category, error) {
	return s.list(ctx, false)
}

func (s *CategoryService) ListAdmin(ctx context.Context) ([]*domain.Category, error) {
	return s.list(ctx, true)
}

func (s *CategoryService) list(ctx context.Context, includeArchived bool) ([]*domain.Category, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	visible := make([]*domain.Category, 0, len(items))
	for _, item := range items {
		item.NormalizeHierarchy()
		item.Children = nil
		if !includeArchived && item.LifecycleStatus != domain.CategoryLifecycleActive {
			continue
		}
		visible = append(visible, item)
	}
	return visible, nil
}

func (s *CategoryService) ListTree(ctx context.Context, includeArchived bool) ([]*domain.Category, error) {
	items, err := s.list(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*domain.Category, len(items))
	for _, item := range items {
		item.Children = nil
		byID[item.ID] = item
	}
	roots := make([]*domain.Category, 0, len(items))
	for _, item := range items {
		if item.ParentID == nil {
			roots = append(roots, item)
			continue
		}
		parent, ok := byID[*item.ParentID]
		if !ok || parent.NodeKind != domain.CategoryNodeGroup {
			// A public tree never promotes a board out of an omitted/invalid
			// parent. The database trigger prevents this for new writes.
			continue
		}
		parent.Children = append(parent.Children, item)
	}
	return roots, nil
}

// Update keeps layout and posting settings versioned. Parent changes are
// deliberately rejected here: Move has an explicit request shape that can
// distinguish an omitted value from JSON null (move to root).
func (s *CategoryService) Update(ctx context.Context, id string, req domain.UpdateCategoryRequest) (*domain.Category, error) {
	return s.UpdateForActor(ctx, "", id, req)
}

func (s *CategoryService) UpdateForActor(ctx context.Context, actorID, id string, req domain.UpdateCategoryRequest) (*domain.Category, error) {
	if req.Version < 1 {
		return nil, ErrCategoryVersionRequired
	}
	if req.ParentID != nil {
		return nil, fmt.Errorf("%w: use the category move endpoint for parent_id", ErrCategoryHierarchy)
	}
	return s.execute(ctx, actorID, "community.category.update", id, eventbus.EventCategoryUpdated, func(commandCtx context.Context) (*domain.Category, error) {
		cat, err := s.repo.GetByIDForUpdate(commandCtx, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		cat.NormalizeHierarchy()
		if cat.Version != req.Version {
			return nil, repository.ErrCategoryVersionConflict
		}
		if err := applyCategoryUpdate(cat, req); err != nil {
			return nil, err
		}
		cat.Version++
		cat.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateIfVersion(commandCtx, cat, req.Version); err != nil {
			return nil, err
		}
		return cat, nil
	})
}

func (s *CategoryService) Move(ctx context.Context, id string, req domain.MoveCategoryRequest) (*domain.Category, error) {
	return s.MoveForActor(ctx, "", id, req)
}

func (s *CategoryService) MoveForActor(ctx context.Context, actorID, id string, req domain.MoveCategoryRequest) (*domain.Category, error) {
	if !req.ParentSpecified {
		return nil, fmt.Errorf("%w: parent_id must be supplied", ErrCategoryHierarchy)
	}
	if req.Version < 1 {
		return nil, ErrCategoryVersionRequired
	}
	parentID, err := normalizeParentID(req.ParentID)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, actorID, "community.category.move", id, eventbus.EventCategoryMoved, func(commandCtx context.Context) (*domain.Category, error) {
		cat, err := s.repo.GetByIDForUpdate(commandCtx, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		cat.NormalizeHierarchy()
		if cat.Version != req.Version {
			return nil, repository.ErrCategoryVersionConflict
		}
		if err := s.validateParentForCategory(commandCtx, cat, parentID); err != nil {
			return nil, err
		}
		cat.ParentID = parentID
		cat.Version++
		cat.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateIfVersion(commandCtx, cat, req.Version); err != nil {
			return nil, err
		}
		return cat, nil
	})
}

func (s *CategoryService) ArchiveImpact(ctx context.Context, id string) (*domain.CategoryArchiveImpact, error) {
	cat, err := s.GetAdminByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.archiveImpact(ctx, cat)
}

// ListThreadTypePolicies returns the explicit policy surface. It is useful to
// an administrator before configuring a board and to trusted built-in posting
// forms; creation still rechecks the policy inside the reliable command.
func (s *CategoryService) ListThreadTypePolicies(ctx context.Context, id string) ([]domain.CategoryThreadTypePolicy, error) {
	cat, err := s.GetAdminByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cat.NodeKind != domain.CategoryNodeBoard {
		return nil, fmt.Errorf("%w: only a board has thread type policies", ErrCategoryHierarchy)
	}
	if s.typePolicies == nil {
		return nil, errors.New("category thread type policy repository is unavailable")
	}
	return s.typePolicies.List(ctx, cat.ID)
}

func (s *CategoryService) UpdateThreadTypePoliciesForActor(ctx context.Context, actorID, id string, req domain.UpdateCategoryThreadTypePolicyRequest) (*domain.Category, []domain.CategoryThreadTypePolicy, error) {
	if req.Version < 1 {
		return nil, nil, ErrCategoryVersionRequired
	}
	allowed, err := normalizeAllowedThreadTypes(req.AllowedTypes)
	if err != nil {
		return nil, nil, err
	}
	if s.typePolicies == nil {
		return nil, nil, errors.New("category thread type policy repository is unavailable")
	}
	var policies []domain.CategoryThreadTypePolicy
	cat, err := s.execute(ctx, actorID, "community.category.configure_thread_types", id, eventbus.EventCategoryUpdated, func(commandCtx context.Context) (*domain.Category, error) {
		cat, commandErr := s.repo.GetByIDForUpdate(commandCtx, strings.TrimSpace(id))
		if commandErr != nil {
			return nil, commandErr
		}
		cat.NormalizeHierarchy()
		if cat.NodeKind != domain.CategoryNodeBoard {
			return nil, fmt.Errorf("%w: only a board has thread type policies", ErrCategoryHierarchy)
		}
		if cat.Version != req.Version {
			return nil, repository.ErrCategoryVersionConflict
		}
		if commandErr = s.typePolicies.Replace(commandCtx, cat.ID, allowed); commandErr != nil {
			return nil, fmt.Errorf("replace category thread type policy: %w", commandErr)
		}
		policies, commandErr = s.typePolicies.List(commandCtx, cat.ID)
		if commandErr != nil {
			return nil, fmt.Errorf("read category thread type policy: %w", commandErr)
		}
		cat.Version++
		cat.UpdatedAt = time.Now().UTC()
		if commandErr = s.repo.UpdateIfVersion(commandCtx, cat, req.Version); commandErr != nil {
			return nil, commandErr
		}
		return cat, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return cat, policies, nil
}

func (s *CategoryService) Archive(ctx context.Context, id string, version int64) (*domain.Category, error) {
	return s.ArchiveForActor(ctx, "", id, version)
}

func (s *CategoryService) ArchiveForActor(ctx context.Context, actorID, id string, version int64) (*domain.Category, error) {
	if version < 1 {
		return nil, ErrCategoryVersionRequired
	}
	return s.execute(ctx, actorID, "community.category.archive", id, eventbus.EventCategoryArchived, func(commandCtx context.Context) (*domain.Category, error) {
		cat, err := s.repo.GetByIDForUpdate(commandCtx, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		cat.NormalizeHierarchy()
		if cat.Version != version {
			return nil, repository.ErrCategoryVersionConflict
		}
		if cat.LifecycleStatus == domain.CategoryLifecycleArchived {
			return nil, ErrCategoryArchived
		}
		impact, err := s.archiveImpact(commandCtx, cat)
		if err != nil {
			return nil, err
		}
		if impact.ActiveChildBoards > 0 {
			return nil, fmt.Errorf("%w: archive or move %d active child board(s) first", ErrCategoryHierarchy, impact.ActiveChildBoards)
		}
		cat.LifecycleStatus = domain.CategoryLifecycleArchived
		cat.Version++
		cat.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateIfVersion(commandCtx, cat, version); err != nil {
			return nil, err
		}
		return cat, nil
	})
}

// ArchiveLegacyForActor preserves the DELETE compatibility window without
// pretending deletion is still physical. The usage record gives operators a
// concrete signal before the deprecated route is removed in a later release.
func (s *CategoryService) ArchiveLegacyForActor(ctx context.Context, actorID, id string, version int64) (*domain.Category, error) {
	cat, err := s.ArchiveForActor(ctx, actorID, id, version)
	if err == nil && s.reliable != nil {
		_ = s.reliable.RecordCompatibility(ctx, "community.category.delete.archive", "deprecated_route", map[string]string{
			"category_id": cat.ID,
			"actor_id":    strings.TrimSpace(actorID),
		})
	}
	return cat, err
}

func (s *CategoryService) Restore(ctx context.Context, id string, version int64) (*domain.Category, error) {
	return s.RestoreForActor(ctx, "", id, version)
}

func (s *CategoryService) RestoreForActor(ctx context.Context, actorID, id string, version int64) (*domain.Category, error) {
	if version < 1 {
		return nil, ErrCategoryVersionRequired
	}
	return s.execute(ctx, actorID, "community.category.restore", id, eventbus.EventCategoryRestored, func(commandCtx context.Context) (*domain.Category, error) {
		cat, err := s.repo.GetByIDForUpdate(commandCtx, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		cat.NormalizeHierarchy()
		if cat.Version != version {
			return nil, repository.ErrCategoryVersionConflict
		}
		if cat.LifecycleStatus != domain.CategoryLifecycleArchived {
			return nil, fmt.Errorf("%w: category is already active", ErrCategoryHierarchy)
		}
		if err := s.validateParentForCategory(commandCtx, cat, cat.ParentID); err != nil {
			return nil, err
		}
		cat.LifecycleStatus = domain.CategoryLifecycleActive
		cat.Version++
		cat.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateIfVersion(commandCtx, cat, version); err != nil {
			return nil, err
		}
		return cat, nil
	})
}

// Delete is retained for direct legacy callers. HTTP DELETE is mapped to
// ArchiveForActor and requires an explicit version so browser clients cannot
// accidentally overwrite a concurrent move or configuration change.
func (s *CategoryService) Delete(ctx context.Context, id string) error {
	cat, err := s.GetAdminByID(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.ArchiveForActor(ctx, "", id, cat.Version)
	return err
}

// ValidatePostingCategory is shared by Thread and reply creation. It checks
// the same facts exposed in the category API plus the active parent group
// invariant, so an archived or closed node cannot accept new author content.
func (s *CategoryService) ValidatePostingCategory(ctx context.Context, id string) (*domain.Category, error) {
	return validatePostingCategory(ctx, s.repo, id)
}

func validatePostingCategory(ctx context.Context, repo repository.CategoryRepository, id string) (*domain.Category, error) {
	if repo == nil {
		return nil, errors.New("category repository is unavailable")
	}
	cat, err := repo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	cat.NormalizeHierarchy()
	if !cat.CanAcceptThreads() {
		return nil, ErrCategoryPostingUnavailable
	}
	if cat.ParentID == nil {
		return cat, nil
	}
	parent, err := repo.GetByID(ctx, *cat.ParentID)
	if err != nil {
		return nil, err
	}
	parent.NormalizeHierarchy()
	if !parent.IsActiveGroup() {
		return nil, ErrCategoryPostingUnavailable
	}
	return cat, nil
}

func normalizeAllowedThreadTypes(values []domain.ThreadType) ([]domain.ThreadType, error) {
	seen := map[domain.ThreadType]bool{}
	result := make([]domain.ThreadType, 0, len(values))
	for _, value := range values {
		threadType := domain.NormalizeThreadType(value)
		if !domain.IsKnownThreadType(threadType) {
			return nil, fmt.Errorf("invalid thread type %q", value)
		}
		if seen[threadType] {
			return nil, fmt.Errorf("duplicate thread type %q", threadType)
		}
		seen[threadType] = true
		result = append(result, threadType)
	}
	if len(result) == 0 {
		return nil, errors.New("at least one thread type is required")
	}
	return result, nil
}

func (s *CategoryService) execute(ctx context.Context, actorID, code, categoryID, eventType string, action func(context.Context) (*domain.Category, error)) (*domain.Category, error) {
	if s.reliable == nil || transaction.Active(ctx) {
		cat, err := action(ctx)
		if err == nil && s.bus != nil && !transaction.Active(ctx) {
			_ = s.bus.Publish(ctx, eventbus.NewEvent(eventType, "campusos.community", "category."+cat.ID, cat))
		}
		return cat, err
	}
	var result *domain.Category
	err := s.reliable.Execute(ctx, reliability.Command{
		Code: code, ActorID: strings.TrimSpace(actorID), ActorType: "user",
		ResourceType: "category", ResourceID: strings.TrimSpace(categoryID),
		OperationCode: code, PermissionCode: code,
		EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent(eventType, "category", categoryID, result)
		},
	}, func(commandCtx context.Context) error {
		var actionErr error
		result, actionErr = action(commandCtx)
		return actionErr
	})
	return result, err
}

func (s *CategoryService) validateParentForCategory(ctx context.Context, cat *domain.Category, parentID *string) error {
	if cat == nil {
		return ErrCategoryHierarchy
	}
	cat.NormalizeHierarchy()
	if cat.NodeKind == domain.CategoryNodeGroup {
		if parentID != nil {
			return fmt.Errorf("%w: a group must stay at the root", ErrCategoryHierarchy)
		}
		return nil
	}
	if cat.NodeKind != domain.CategoryNodeBoard {
		return fmt.Errorf("%w: unsupported node kind %q", ErrCategoryHierarchy, cat.NodeKind)
	}
	if parentID == nil {
		return nil
	}
	if *parentID == cat.ID {
		return fmt.Errorf("%w: a category cannot be its own parent", ErrCategoryHierarchy)
	}
	parent, err := s.repo.GetByIDForUpdate(ctx, *parentID)
	if err != nil {
		return fmt.Errorf("get category parent: %w", err)
	}
	parent.NormalizeHierarchy()
	if !parent.IsActiveGroup() {
		return fmt.Errorf("%w: parent must be an active root group", ErrCategoryHierarchy)
	}
	return nil
}

func (s *CategoryService) archiveImpact(ctx context.Context, cat *domain.Category) (*domain.CategoryArchiveImpact, error) {
	cat.NormalizeHierarchy()
	impact := &domain.CategoryArchiveImpact{
		CategoryID:          cat.ID,
		NodeKind:            string(cat.NodeKind),
		WillBlockNewPosting: cat.NodeKind == domain.CategoryNodeBoard,
	}
	children, err := s.repo.ListChildren(ctx, cat.ID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		child.NormalizeHierarchy()
		if child.NodeKind == domain.CategoryNodeBoard && child.LifecycleStatus == domain.CategoryLifecycleActive {
			impact.ActiveChildBoards++
		}
	}
	if s.threadRepo != nil {
		_, total, err := s.threadRepo.List(ctx, domain.ThreadListFilter{
			CategoryID: cat.ID, IncludeTrashed: true, Page: 1, PageSize: 1,
		})
		if err != nil {
			return nil, fmt.Errorf("count category threads: %w", err)
		}
		impact.AssociatedThreads = total
	}
	return impact, nil
}

func newCategory(req domain.CreateCategoryRequest) (*domain.Category, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("category name is required")
	}
	parentID, err := normalizeParentID(req.ParentID)
	if err != nil {
		return nil, err
	}
	color, err := normalizeCategoryColor(req.Color)
	if err != nil {
		return nil, err
	}
	id := strconv.FormatInt(idgen.New(), 10)
	slug := normalizeCategorySlug(req.Slug)
	if slug == "" {
		slug = fallbackCategorySlug(name, id)
	}
	nodeKind := req.NodeKind
	if nodeKind == "" {
		nodeKind = domain.CategoryNodeBoard
	}
	if nodeKind != domain.CategoryNodeBoard && nodeKind != domain.CategoryNodeGroup {
		return nil, fmt.Errorf("%w: unsupported node kind %q", ErrCategoryHierarchy, nodeKind)
	}
	if nodeKind == domain.CategoryNodeGroup && parentID != nil {
		return nil, fmt.Errorf("%w: a group must be a root node", ErrCategoryHierarchy)
	}
	now := time.Now().UTC()
	return &domain.Category{
		ID: id, Name: name, Slug: slug, Description: strings.TrimSpace(req.Description), Icon: strings.TrimSpace(req.Icon),
		Color: color, DefaultTags: normalizeTags(req.DefaultTags), ParentID: parentID, NodeKind: nodeKind,
		LifecycleStatus: domain.CategoryLifecycleActive, Version: 1, SortOrder: req.SortOrder, IsClosed: req.IsClosed,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func applyCategoryUpdate(cat *domain.Category, req domain.UpdateCategoryRequest) error {
	if cat == nil {
		return repository.ErrCategoryNotFound
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return errors.New("category name is required")
		}
		cat.Name = name
	}
	if req.Slug != nil {
		slug := normalizeCategorySlug(*req.Slug)
		if slug != "" {
			cat.Slug = slug
		}
	}
	if req.Description != nil {
		cat.Description = strings.TrimSpace(*req.Description)
	}
	if req.Icon != nil {
		cat.Icon = strings.TrimSpace(*req.Icon)
	}
	if req.Color != nil {
		color, err := normalizeCategoryColor(*req.Color)
		if err != nil {
			return err
		}
		cat.Color = color
	}
	if req.DefaultTags != nil {
		cat.DefaultTags = normalizeTags(req.DefaultTags)
	}
	if req.IsClosed != nil {
		cat.IsClosed = *req.IsClosed
	}
	if req.SortOrder != nil {
		cat.SortOrder = *req.SortOrder
	}
	return nil
}

func normalizeParentID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, fmt.Errorf("%w: parent_id must be a non-empty identifier or null", ErrCategoryHierarchy)
	}
	return &normalized, nil
}

func normalizeCategoryColor(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !categoryColorPattern.MatchString(value) {
		return "", errors.New("category color must use #RRGGBB or #RRGGBBAA")
	}
	return strings.ToUpper(value), nil
}

func fallbackCategorySlug(name, id string) string {
	slug := normalizeCategorySlug(name)
	if slug != "" {
		return slug
	}
	return "category-" + id
}

func normalizeCategorySlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-")
	}
	return slug
}
