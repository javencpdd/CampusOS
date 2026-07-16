package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrThreadStateConflict      = errors.New("thread state does not allow this operation")
	ErrThreadPurged             = errors.New("thread has been permanently removed")
	ErrModerationReasonRequired = errors.New("moderation reason is required")
)

// ThreadService 帖子服务
type ThreadService struct {
	repo          repository.ThreadRepository
	categoryRepo  repository.CategoryRepository
	governance    repository.ContentGovernanceRepository
	authorization ContentAuthorization
	bus           eventbus.EventBus
	cache         cache.Cache
}

// ContentAuthorization is the narrow server-side policy port used for
// governance transitions. It receives a category derived from the stored
// thread, never from a client-provided scope field.
type ContentAuthorization interface {
	CheckCodeScoped(context.Context, string, string, string, int64) (bool, error)
}

// ContentAuthorizationAuditor is optional so standalone Community tests and
// legacy composition retain a narrow policy contract. Production Identity
// supplies it to persist resource-derived allow and deny decisions.
type ContentAuthorizationAuditor interface {
	RecordContentAuthorizationDecision(context.Context, string, string, int64, string, string)
}

type CreateThreadOptions struct {
	Status        domain.ThreadStatus
	ContentFormat string
}

// NewThreadService 创建帖子服务
func NewThreadService(repo repository.ThreadRepository, bus eventbus.EventBus) *ThreadService {
	return &ThreadService{repo: repo, bus: bus}
}

// SetCache 设置缓存实例
func (s *ThreadService) SetCache(c cache.Cache) {
	s.cache = c
}

func (s *ThreadService) SetCategoryRepository(repo repository.CategoryRepository) {
	s.categoryRepo = repo
}

func (s *ThreadService) SetGovernanceRepository(repo repository.ContentGovernanceRepository) {
	s.governance = repo
}

func (s *ThreadService) SetContentAuthorization(authorization ContentAuthorization) {
	s.authorization = authorization
}

// CreateThread 创建帖子
func (s *ThreadService) CreateThread(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest) (*domain.Thread, error) {
	return s.CreateThreadWithOptions(ctx, authorID, authorName, req, CreateThreadOptions{
		Status:        domain.ThreadStatusPublished,
		ContentFormat: "markdown",
	})
}

func (s *ThreadService) CreateThreadWithOptions(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest, opts CreateThreadOptions) (*domain.Thread, error) {
	now := time.Now().UTC()
	defaultTags := []string{}
	if s.categoryRepo != nil {
		category, err := s.categoryRepo.GetByID(ctx, req.CategoryID)
		if err != nil {
			return nil, fmt.Errorf("get category: %w", err)
		}
		defaultTags = category.DefaultTags
	}
	status := opts.Status
	if status == "" {
		status = domain.ThreadStatusPublished
	}
	if req.IsPrivate && status == domain.ThreadStatusPublished {
		status = domain.ThreadStatusPrivate
	}
	contentFormat := opts.ContentFormat
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	thread := &domain.Thread{
		ID:            strconv.FormatInt(idgen.New(), 10),
		Title:         req.Title,
		Content:       req.Content,
		ContentFormat: contentFormat,
		AuthorID:      authorID,
		AuthorName:    authorName,
		CategoryID:    req.CategoryID,
		Status:        status,
		Tags:          mergeTags(defaultTags, req.Tags),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	thread.NormalizeContentState()
	thread.CurrentRevision = 1

	if err := s.repo.Create(ctx, thread); err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	if err := s.recordRevision(ctx, thread, authorID, "create", ""); err != nil {
		return nil, fmt.Errorf("record content revision: %w", err)
	}

	// 清除列表缓存
	s.invalidateListCache(ctx)

	// 发布 thread.created 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadCreated, "campusos.community", "thread."+thread.ID, thread,
		))
	}

	return thread, nil
}

// GetThread 获取帖子详情
func (s *ThreadService) GetThread(ctx context.Context, id string) (*domain.Thread, error) {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if !current.IsPublic() {
		return nil, repository.ErrThreadNotFound
	}
	thread, err := s.repo.IncrementViewCount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	s.invalidateListCache(ctx)
	return thread, nil
}

// GetPublicThread returns the canonical public fact without incrementing a
// view counter. Profile, MCP and Host API reads use this path so integrations
// cannot inflate audience metrics as a side effect of synchronization.
func (s *ThreadService) GetPublicThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if !thread.IsPublic() {
		return nil, repository.ErrThreadNotFound
	}
	return thread, nil
}

func (s *ThreadService) GetThreadForViewer(ctx context.Context, id, viewerID string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if thread.IsPublic() {
		thread, err = s.repo.IncrementViewCount(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get thread: %w", err)
		}
		s.invalidateListCache(ctx)
		return thread, nil
	}
	if !thread.IsAuthorVisible(viewerID) {
		return nil, repository.ErrThreadNotFound
	}
	return thread, nil
}

// ListThreads 获取帖子列表（支持缓存）
func (s *ThreadService) ListThreads(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	includeAllStatuses := filter.Status == "all"
	if includeAllStatuses {
		filter.Status = ""
	}
	// 默认只显示公开内容。旧 status 条件保留给 v0.9 客户端和数据库兼容。
	if filter.Status == "" && !includeAllStatuses {
		filter.Status = string(domain.ThreadStatusPublished)
		filter.PublicationStatus = string(domain.PublicationStatusPublished)
		filter.ModerationStatus = string(domain.ModerationStatusClear)
		filter.DeletionStatus = string(domain.DeletionStatusActive)
	}

	// 尝试从缓存获取（仅缓存第一页无筛选条件的查询）
	cacheKey := fmt.Sprintf("threads:list:%d:%d:%s:%s", filter.Page, filter.PageSize, filter.Status, filter.ContentFormat)
	cacheablePublicList := filter.Status == string(domain.ThreadStatusPublished) &&
		filter.Keyword == "" &&
		filter.CategoryID == "" &&
		len(filter.CategoryIDs) == 0 &&
		filter.AuthorID == "" &&
		filter.ContentFormat == "" &&
		filter.Tag == "" &&
		len(filter.AnyTags) == 0 &&
		filter.PublicationStatus == string(domain.PublicationStatusPublished) &&
		filter.ModerationStatus == string(domain.ModerationStatusClear) &&
		filter.DeletionStatus == string(domain.DeletionStatusActive)
	if s.cache != nil && cacheablePublicList {
		type cachedResult struct {
			Threads []*domain.Thread `json:"threads"`
			Total   int64            `json:"total"`
		}
		var cached cachedResult
		if err := s.cache.Get(ctx, cacheKey, &cached); err == nil {
			log.Printf("📦 缓存命中: %s", cacheKey)
			return cached.Threads, cached.Total, nil
		}
	}

	threads, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 写入缓存（5 分钟 TTL）
	if s.cache != nil && cacheablePublicList {
		type cachedResult struct {
			Threads []*domain.Thread `json:"threads"`
			Total   int64            `json:"total"`
		}
		_ = s.cache.Set(ctx, cacheKey, cachedResult{Threads: threads, Total: total}, 5*time.Minute)
	}

	return threads, total, nil
}

// UpdateThread 更新帖子
func (s *ThreadService) UpdateThread(ctx context.Context, id, authorID string, req domain.UpdateThreadRequest) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}

	if thread.AuthorID != authorID {
		return nil, fmt.Errorf("permission denied: you can only edit your own threads")
	}
	thread.NormalizeContentState()
	if !thread.IsActive() {
		return nil, ErrThreadStateConflict
	}
	before := domain.ContentStateSummary(thread)
	resubmitted := false

	if req.Title != nil {
		thread.Title = *req.Title
	}
	if req.Content != nil {
		thread.Content = *req.Content
	}
	if req.Tags != nil {
		thread.Tags = normalizeTags(req.Tags)
	}
	if req.Status != nil {
		if thread.ContentFormat == "richtext_article" {
			return nil, fmt.Errorf("richtext article status must be managed through richtext article APIs")
		}
		switch *req.Status {
		case domain.ThreadStatusPublished:
			thread.PublicationStatus = domain.PublicationStatusPublished
			if thread.ModerationStatus == domain.ModerationStatusTakenDown || thread.ModerationStatus == domain.ModerationStatusRejected {
				thread.ModerationStatus = domain.ModerationStatusPending
				thread.ModerationAt = ptrTime(time.Now().UTC())
				resubmitted = true
			} else {
				thread.ModerationStatus = domain.ModerationStatusClear
			}
		case domain.ThreadStatusPrivate:
			thread.PublicationStatus = domain.PublicationStatusPrivate
		case domain.ThreadStatusDraft:
			thread.PublicationStatus = domain.PublicationStatusDraft
		default:
			return nil, fmt.Errorf("invalid thread status: %s", *req.Status)
		}
	}
	thread.UpdatedAt = time.Now().UTC()
	thread.CurrentRevision++
	thread.SyncLegacyStatus()

	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("update thread: %w", err)
	}
	if resubmitted {
		err = s.recordTransition(ctx, thread, authorID, "author_resubmit", "author republished moderated content", before, true)
	} else {
		err = s.recordRevision(ctx, thread, authorID, "author_update", "")
	}
	if err != nil {
		return nil, fmt.Errorf("record content revision: %w", err)
	}

	// 清除列表缓存
	s.invalidateListCache(ctx)

	s.publishThreadUpdated(ctx, thread)

	return thread, nil
}

// SaveFeatureThread is the sole write path exposed to built-in content
// features. The caller can update content and express a publication intent,
// but Community keeps moderation and deletion state under its own control.
func (s *ThreadService) SaveFeatureThread(ctx context.Context, candidate *domain.Thread, actorID, action string) (*domain.Thread, error) {
	if candidate == nil || candidate.ID == "" {
		return nil, errors.New("thread content is required")
	}
	thread, err := s.repo.GetByID(ctx, candidate.ID)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if actorID == "" || thread.AuthorID != actorID {
		return nil, fmt.Errorf("permission denied: you can only edit your own threads")
	}
	thread.NormalizeContentState()
	if !thread.IsActive() {
		return nil, ErrThreadStateConflict
	}

	before := domain.ContentStateSummary(thread)
	thread.Title = candidate.Title
	thread.Content = candidate.Content
	thread.Tags = normalizeTags(candidate.Tags)
	if candidate.ContentFormat != "" {
		thread.ContentFormat = candidate.ContentFormat
	}
	intent := candidate.PublicationStatus
	if intent == "" {
		intent = thread.PublicationStatus
	}
	resubmitted := false
	if intent == domain.PublicationStatusPublished && (thread.ModerationStatus == domain.ModerationStatusTakenDown || thread.ModerationStatus == domain.ModerationStatusRejected) {
		thread.ModerationStatus = domain.ModerationStatusPending
		thread.ModerationAt = ptrTime(time.Now().UTC())
		resubmitted = true
	}
	thread.PublicationStatus = intent
	thread.CurrentRevision++
	thread.UpdatedAt = time.Now().UTC()
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("update feature thread: %w", err)
	}
	if action == "" {
		action = "feature_update"
	}
	if resubmitted {
		if err := s.recordTransition(ctx, thread, actorID, action, "author resubmitted moderated content", before, true); err != nil {
			return nil, fmt.Errorf("record content transition: %w", err)
		}
	} else if err := s.recordRevision(ctx, thread, actorID, action, ""); err != nil {
		return nil, fmt.Errorf("record content revision: %w", err)
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

// PinThread 置顶帖子
func (s *ThreadService) PinThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsPinned = true
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("pin thread: %w", err)
	}
	s.invalidateListCache(ctx)
	return thread, nil
}

// UnpinThread 取消置顶
func (s *ThreadService) UnpinThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsPinned = false
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("unpin thread: %w", err)
	}
	s.invalidateListCache(ctx)
	return thread, nil
}

// LockThread 锁定帖子
func (s *ThreadService) LockThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsLocked = true
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("lock thread: %w", err)
	}
	s.invalidateListCache(ctx)
	return thread, nil
}

// UnlockThread 解锁帖子
func (s *ThreadService) UnlockThread(ctx context.Context, id string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.IsLocked = false
	thread.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, fmt.Errorf("unlock thread: %w", err)
	}
	s.invalidateListCache(ctx)
	return thread, nil
}

// DeleteThread 删除帖子
func (s *ThreadService) DeleteThread(ctx context.Context, id, authorID string) error {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}

	if thread.AuthorID != authorID {
		return fmt.Errorf("permission denied: you can only delete your own threads")
	}

	if err := s.trashThread(ctx, thread, authorID, "author_delete", "author moved content to trash"); err != nil {
		return err
	}

	// 清除列表缓存
	s.invalidateListCache(ctx)

	// 发布 thread.deleted 事件
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadDeleted, "campusos.community", "thread."+id, thread,
		))
	}

	return nil
}

// TrashThread is the internal command used by built-in content features. It
// still validates author ownership for author-originated actions; callers that
// act as moderators must have passed the HTTP/service authorization gate.
func (s *ThreadService) TrashThread(ctx context.Context, id, actorID, action, reason string) error {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}
	if actorID == "" {
		return errors.New("content trash actor is required")
	}
	if strings.HasPrefix(action, "author_") || strings.HasPrefix(action, "richtext_author_") || action == "richtext_create_rollback" {
		if thread.AuthorID != actorID {
			return fmt.Errorf("permission denied: you can only delete your own threads")
		}
	}
	if action == "" {
		action = "content_trash"
	}
	if reason == "" {
		reason = "content moved to trash"
	}
	if err := s.trashThread(ctx, thread, actorID, action, reason); err != nil {
		return err
	}
	s.invalidateListCache(ctx)
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadDeleted, "campusos.community", "thread."+id, thread,
		))
	}
	return nil
}

func (s *ThreadService) AdminDeleteThread(ctx context.Context, id, actorID string) error {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.trash", thread); err != nil {
		return err
	}
	return s.TrashThread(ctx, id, actorID, "admin_trash", "administrator moved content to trash")
}

// TakeDown prevents public visibility without changing the author's intended
// publication choice. A subsequent author publish request enters review.
func (s *ThreadService) TakeDown(ctx context.Context, id, actorID, reason string) (*domain.Thread, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrModerationReasonRequired
	}
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if !thread.IsActive() || thread.ModerationStatus == domain.ModerationStatusTakenDown {
		return nil, ErrThreadStateConflict
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.take_down", thread); err != nil {
		return nil, err
	}
	before := domain.ContentStateSummary(thread)
	now := time.Now().UTC()
	thread.ModerationStatus = domain.ModerationStatusTakenDown
	thread.ModerationReason = strings.TrimSpace(reason)
	thread.ModerationBy = actorID
	thread.ModerationAt = &now
	thread.CurrentRevision++
	thread.UpdatedAt = now
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, "take_down", reason, before, true); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) SubmitForReview(ctx context.Context, id, authorID string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if thread.AuthorID != authorID {
		return nil, fmt.Errorf("permission denied: you can only submit your own threads")
	}
	thread.NormalizeContentState()
	if !thread.IsActive() || (thread.ModerationStatus != domain.ModerationStatusTakenDown && thread.ModerationStatus != domain.ModerationStatusRejected) {
		return nil, ErrThreadStateConflict
	}
	before := domain.ContentStateSummary(thread)
	now := time.Now().UTC()
	thread.PublicationStatus = domain.PublicationStatusPublished
	thread.ModerationStatus = domain.ModerationStatusPending
	thread.ModerationAt = &now
	thread.CurrentRevision++
	thread.UpdatedAt = now
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, authorID, "submit_review", "author resubmitted content", before, true); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) Approve(ctx context.Context, id, actorID, reason string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.review", thread); err != nil {
		return nil, err
	}
	return s.resolveModeration(ctx, id, actorID, reason, domain.ModerationStatusClear, "approve")
}

func (s *ThreadService) Reject(ctx context.Context, id, actorID, reason string) (*domain.Thread, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrModerationReasonRequired
	}
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.review", thread); err != nil {
		return nil, err
	}
	return s.resolveModeration(ctx, id, actorID, reason, domain.ModerationStatusRejected, "reject")
}

func (s *ThreadService) DirectRestore(ctx context.Context, id, actorID, reason string) (*domain.Thread, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrModerationReasonRequired
	}
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if !thread.IsActive() || thread.ModerationStatus != domain.ModerationStatusTakenDown {
		return nil, ErrThreadStateConflict
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.direct_restore", thread); err != nil {
		return nil, err
	}
	before := domain.ContentStateSummary(thread)
	now := time.Now().UTC()
	thread.ModerationStatus = domain.ModerationStatusClear
	thread.ModerationReason = strings.TrimSpace(reason)
	thread.ModerationBy = actorID
	thread.ModerationAt = &now
	thread.CurrentRevision++
	thread.UpdatedAt = now
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, "direct_restore", reason, before, false); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) RestoreFromTrash(ctx context.Context, id, actorID string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if thread.DeletionStatus != domain.DeletionStatusTrashed {
		return nil, ErrThreadStateConflict
	}
	if actorID == "" || thread.AuthorID != actorID {
		return nil, fmt.Errorf("permission denied: you can only restore your own threads")
	}
	before := domain.ContentStateSummary(thread)
	thread.DeletionStatus = domain.DeletionStatusActive
	thread.CurrentRevision++
	thread.UpdatedAt = time.Now().UTC()
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, "restore_trash", "", before, false); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) AdminRestoreFromTrash(ctx context.Context, id, actorID, reason string) (*domain.Thread, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrModerationReasonRequired
	}
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if thread.DeletionStatus != domain.DeletionStatusTrashed {
		return nil, ErrThreadStateConflict
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.restore", thread); err != nil {
		return nil, err
	}
	before := domain.ContentStateSummary(thread)
	thread.DeletionStatus = domain.DeletionStatusActive
	thread.CurrentRevision++
	thread.UpdatedAt = time.Now().UTC()
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, "admin_restore_trash", reason, before, false); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) PurgeThread(ctx context.Context, id, actorID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrModerationReasonRequired
	}
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if thread.DeletionStatus != domain.DeletionStatusTrashed {
		return ErrThreadStateConflict
	}
	if err := s.requireContentPermission(ctx, actorID, "community.thread.purge", thread); err != nil {
		return err
	}
	before := domain.ContentStateSummary(thread)
	thread.DeletionStatus = domain.DeletionStatusPurged
	thread.SyncLegacyStatus()
	if err := s.repo.Purge(ctx, id); err != nil {
		return err
	}
	if err := s.recordModerationAction(ctx, thread, actorID, "purge", reason, before); err != nil {
		return err
	}
	s.invalidateListCache(ctx)
	if s.bus != nil {
		_ = s.bus.Publish(ctx, eventbus.NewEvent(
			eventbus.EventThreadDeleted, "campusos.community", "thread."+thread.ID, thread,
		))
	}
	return nil
}

func (s *ThreadService) requireContentPermission(ctx context.Context, actorID, code string, thread *domain.Thread) error {
	if s.authorization == nil {
		// Direct unit-test and legacy composition paths do not install an
		// authorization adapter. Production Community modules always do.
		return nil
	}
	if actorID == "" || thread == nil {
		return errors.New("content governance actor is required")
	}
	categoryID, err := strconv.ParseInt(thread.CategoryID, 10, 64)
	if err != nil || categoryID <= 0 {
		return errors.New("content governance category scope is invalid")
	}
	allowed, err := s.authorization.CheckCodeScoped(ctx, actorID, code, "category", categoryID)
	if err != nil {
		s.recordContentAuthorizationDecision(ctx, actorID, code, categoryID, "error", "scoped permission check failed")
		return err
	}
	if !allowed {
		s.recordContentAuthorizationDecision(ctx, actorID, code, categoryID, "deny", "content governance scope denied")
		return errors.New("permission denied: content governance scope")
	}
	s.recordContentAuthorizationDecision(ctx, actorID, code, categoryID, "allow", "")
	return nil
}

func (s *ThreadService) recordContentAuthorizationDecision(ctx context.Context, actorID, code string, categoryID int64, outcome, reason string) {
	auditor, ok := s.authorization.(ContentAuthorizationAuditor)
	if !ok {
		return
	}
	auditor.RecordContentAuthorizationDecision(ctx, actorID, code, categoryID, outcome, reason)
}

func (s *ThreadService) ListModerationActions(ctx context.Context, id string) ([]*domain.ModerationAction, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	if s.governance == nil {
		return []*domain.ModerationAction{}, nil
	}
	return s.governance.ListModerationActions(ctx, id)
}

func (s *ThreadService) resolveModeration(ctx context.Context, id, actorID, reason string, target domain.ModerationStatus, action string) (*domain.Thread, error) {
	thread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	thread.NormalizeContentState()
	if !thread.IsActive() || thread.ModerationStatus != domain.ModerationStatusPending {
		return nil, ErrThreadStateConflict
	}
	before := domain.ContentStateSummary(thread)
	now := time.Now().UTC()
	thread.ModerationStatus = target
	thread.ModerationReason = strings.TrimSpace(reason)
	thread.ModerationBy = actorID
	thread.ModerationAt = &now
	thread.CurrentRevision++
	thread.UpdatedAt = now
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, action, reason, before, target == domain.ModerationStatusRejected); err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	s.publishThreadUpdated(ctx, thread)
	return thread, nil
}

func (s *ThreadService) trashThread(ctx context.Context, thread *domain.Thread, actorID, action, reason string) error {
	thread.NormalizeContentState()
	if thread.DeletionStatus != domain.DeletionStatusActive {
		return ErrThreadStateConflict
	}
	before := domain.ContentStateSummary(thread)
	thread.DeletionStatus = domain.DeletionStatusTrashed
	thread.CurrentRevision++
	thread.UpdatedAt = time.Now().UTC()
	thread.SyncLegacyStatus()
	if err := s.repo.Update(ctx, thread); err != nil {
		return err
	}
	return s.recordTransition(ctx, thread, actorID, action, reason, before, false)
}

func (s *ThreadService) recordRevision(ctx context.Context, thread *domain.Thread, actorID, action, reason string) error {
	if s.governance == nil {
		return nil
	}
	return s.governance.CreateRevision(ctx, &domain.ContentRevision{
		ID: fmt.Sprintf("%d", idgen.New()), ThreadID: thread.ID, Version: thread.CurrentRevision,
		Title: thread.Title, Content: thread.Content, ContentFormat: thread.ContentFormat, Tags: thread.Tags,
		Action: action, Reason: strings.TrimSpace(reason), CreatedBy: actorID, CreatedAt: time.Now().UTC(),
	})
}

func (s *ThreadService) recordTransition(ctx context.Context, thread *domain.Thread, actorID, action, reason, before string, openCase bool) error {
	if err := s.recordRevision(ctx, thread, actorID, action, reason); err != nil {
		return err
	}
	if s.governance == nil {
		return nil
	}
	openCaseRecord, err := s.governance.LatestOpenCase(ctx, thread.ID)
	if err != nil {
		return err
	}
	caseID := ""
	if openCase {
		if openCaseRecord != nil {
			caseID = openCaseRecord.ID
		} else {
			caseID = fmt.Sprintf("%d", idgen.New())
			if err := s.governance.CreateModerationCase(ctx, &domain.ModerationCase{
				ID: caseID, ThreadID: thread.ID, Status: string(thread.ModerationStatus), Reason: strings.TrimSpace(reason),
				OpenedBy: actorID, OpenedAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
		}
	} else if resolvesModerationCase(action) && openCaseRecord != nil {
		caseID = openCaseRecord.ID
	}
	if err := s.recordModerationActionWithCase(ctx, thread, actorID, action, reason, before, caseID); err != nil {
		return err
	}
	if caseID != "" && resolvesModerationCase(action) {
		return s.governance.ResolveModerationCase(ctx, caseID, "resolved_"+action, actorID, time.Now().UTC())
	}
	return nil
}

func resolvesModerationCase(action string) bool {
	return action == "approve" || action == "direct_restore"
}

func (s *ThreadService) recordModerationAction(ctx context.Context, thread *domain.Thread, actorID, action, reason, before string) error {
	return s.recordModerationActionWithCase(ctx, thread, actorID, action, reason, before, "")
}

func (s *ThreadService) recordModerationActionWithCase(ctx context.Context, thread *domain.Thread, actorID, action, reason, before, caseID string) error {
	if s.governance == nil {
		return nil
	}
	return s.governance.CreateModerationAction(ctx, &domain.ModerationAction{
		ID: fmt.Sprintf("%d", idgen.New()), CaseID: caseID, ThreadID: thread.ID, Action: action,
		Reason: strings.TrimSpace(reason), ActorID: actorID, BeforeState: before, AfterState: domain.ContentStateSummary(thread),
		CreatedAt: time.Now().UTC(),
	})
}

func (s *ThreadService) publishThreadUpdated(ctx context.Context, thread *domain.Thread) {
	if s.bus == nil || thread == nil {
		return
	}
	_ = s.bus.Publish(ctx, eventbus.NewEvent(
		eventbus.EventThreadUpdated, "campusos.community", "thread."+thread.ID, thread,
	))
}

func ptrTime(value time.Time) *time.Time { return &value }

// invalidateListCache 清除帖子列表缓存
func (s *ThreadService) invalidateListCache(ctx context.Context) {
	if s.cache == nil {
		return
	}
	// 清除常见的列表缓存 key
	keys := []string{
		"threads:list:1:20:published",
		"threads:list:1:20:published:",
		"threads:list:1:10:published",
		"threads:list:1:10:published:",
		"threads:list:1:5:published",
		"threads:list:1:5:published:",
		"threads:list:1:20:private",
		"threads:list:1:20:private:",
	}
	for _, key := range keys {
		_ = s.cache.Delete(ctx, key)
	}
}

func (s *ThreadService) InvalidateListCache(ctx context.Context) {
	s.invalidateListCache(ctx)
}
