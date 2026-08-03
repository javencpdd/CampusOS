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
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrThreadStateConflict      = errors.New("thread state does not allow this operation")
	ErrThreadPurged             = errors.New("thread has been permanently removed")
	ErrModerationReasonRequired = errors.New("moderation reason is required")
	ErrThreadTypeInvalid        = errors.New("thread type is invalid")
	ErrThreadTypeUnavailable    = errors.New("thread type feature is disabled")
	ErrThreadTypeNotAllowed     = errors.New("thread type is not enabled for this category")
)

// ThreadService 帖子服务
type ThreadService struct {
	repo          repository.ThreadRepository
	categoryRepo  repository.CategoryRepository
	typePolicies  repository.ThreadTypePolicyRepository
	governance    repository.ContentGovernanceRepository
	authorization ContentAuthorization
	bus           eventbus.EventBus
	cache         cache.Cache
	reliable      *reliability.Service
	typeEnabled   func(domain.ThreadType) bool
	notifications ThreadNotificationWriter
}

// ContentAuthorization is the narrow server-side policy port used for
// governance transitions. It receives a category derived from the stored
// thread, never from a client-provided scope field.
type ContentAuthorization interface {
	CheckCodeScoped(context.Context, string, string, string, int64) (bool, error)
}

type ThreadNotificationWriter interface {
	NotifyThreadTrashed(context.Context, string, string, string, string) error
	NotifyThreadTakenDown(context.Context, string, string, string, string) error
}

// ContentAuthorizationAuditor is optional so standalone Community tests and
// legacy composition retain a narrow policy contract. Production Identity
// supplies it to persist resource-derived allow and deny decisions.
type ContentAuthorizationAuditor interface {
	RecordContentAuthorizationDecision(context.Context, string, string, int64, string, string) error
}

// contentAuthorizationFailure retains enough server-derived context to write a
// deny/error audit after a command transaction rolls back. Persisting that
// audit inside the failed transaction would discard the evidence together
// with the rejected command.
type contentAuthorizationFailure struct {
	err        error
	actorID    string
	code       string
	categoryID int64
	outcome    string
	reason     string
}

func (e *contentAuthorizationFailure) Error() string { return e.err.Error() }
func (e *contentAuthorizationFailure) Unwrap() error { return e.err }

type CreateThreadOptions struct {
	Status        domain.ThreadStatus
	ContentFormat string
	ThreadType    domain.ThreadType
	CommandCode   string
	EventType     string
}

// StructuredThreadParticipant is intentionally an internal application
// contract. Only compiled-in Built-in Feature modules can import Community's
// internal package and participate in the same database command. It is not a
// Host API, Plugin SDK, MCP, or Agent contract.
type StructuredThreadParticipant interface {
	ThreadType() domain.ThreadType
	PersistThreadDetail(context.Context, *domain.Thread) error
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

func (s *ThreadService) SetThreadTypePolicyRepository(repo repository.ThreadTypePolicyRepository) {
	s.typePolicies = repo
	if s.reliable != nil {
		if snapshotter, ok := repo.(transaction.Snapshotter); ok {
			s.reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *ThreadService) SetThreadTypeEnabledChecker(checker func(domain.ThreadType) bool) {
	s.typeEnabled = checker
}

func (s *ThreadService) SetGovernanceRepository(repo repository.ContentGovernanceRepository) {
	s.governance = repo
	if s.reliable != nil {
		if snapshotter, ok := repo.(transaction.Snapshotter); ok {
			s.reliable.RegisterMemorySnapshotters(snapshotter)
		}
	}
}

func (s *ThreadService) SetContentAuthorization(authorization ContentAuthorization) {
	s.authorization = authorization
}

func (s *ThreadService) SetNotificationWriter(writer ThreadNotificationWriter) {
	s.notifications = writer
}

func (s *ThreadService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.repo.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.governance.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
	if snapshotter, ok := s.typePolicies.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// executeAuthorCommand is the reliable boundary for author-originated Thread
// mutations. Built-in features may call the same service while already inside
// their own reliable command; in that case the inner write participates in the
// active transaction and the outer feature command owns the audit and outbox.
func (s *ThreadService) executeAuthorCommand(ctx context.Context, actorID, code, threadID, eventType string, action func(context.Context) (*domain.Thread, error)) (*domain.Thread, error) {
	if s.reliable == nil || transaction.Active(ctx) {
		result, err := action(ctx)
		if err == nil && !transaction.Active(ctx) {
			s.invalidateListCache(ctx)
			s.publishThreadEvent(ctx, eventType, result)
		}
		return result, err
	}

	var result *domain.Thread
	err := s.reliable.Execute(ctx, reliability.Command{
		Code:          code,
		ActorID:       strings.TrimSpace(actorID),
		ActorType:     "user",
		ResourceType:  "thread",
		ResourceID:    threadID,
		OperationCode: code,
		EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent(eventType, "thread", threadID, result)
		},
	}, func(commandCtx context.Context) error {
		var commandErr error
		result, commandErr = action(commandCtx)
		return commandErr
	})
	if err == nil {
		// Cache deletion is an external side effect. The action ran inside the
		// command transaction, so perform it only after a successful commit.
		s.invalidateListCache(ctx)
	}
	return result, err
}

func (s *ThreadService) executeGovernanceCommand(ctx context.Context, actorID, code, threadID, eventType string, action func(context.Context) (*domain.Thread, error)) (*domain.Thread, error) {
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	var result *domain.Thread
	err := s.reliable.Execute(ctx, reliability.Command{
		Code: code, ActorID: actorID, ResourceType: "thread", ResourceID: threadID,
		OperationCode: code, PermissionCode: code,
		EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent(eventType, "thread", threadID, result)
		},
	}, func(commandCtx context.Context) error {
		var commandErr error
		result, commandErr = action(commandCtx)
		return commandErr
	})
	s.recordRolledBackAuthorizationDecision(ctx, err)
	if err == nil {
		// Cache deletion is an external side effect. The inner action skips it
		// while TxKernel is active; invalidate only after the command commits.
		s.invalidateListCache(ctx)
	}
	return result, err
}

func (s *ThreadService) executeGovernanceDelete(ctx context.Context, actorID, code, threadID string, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	err := s.reliable.Execute(ctx, reliability.Command{
		Code: code, ActorID: actorID, ResourceType: "thread", ResourceID: threadID,
		OperationCode: code, PermissionCode: code,
		EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent(eventbus.EventThreadDeleted, "thread", threadID, map[string]string{"thread_id": threadID, "action": code})
		},
	}, action)
	s.recordRolledBackAuthorizationDecision(ctx, err)
	if err == nil {
		s.invalidateListCache(ctx)
	}
	return err
}

func (s *ThreadService) updateGoverned(ctx context.Context, thread *domain.Thread) error {
	if guarded, ok := s.repo.(repository.GovernedThreadRepository); ok {
		return guarded.UpdateIfRevision(ctx, thread, thread.CurrentRevision-1)
	}
	return s.repo.Update(ctx, thread)
}

func (s *ThreadService) purgeGoverned(ctx context.Context, thread *domain.Thread) error {
	if guarded, ok := s.repo.(repository.GovernedThreadRepository); ok {
		return guarded.PurgeIfRevision(ctx, thread.ID, thread.CurrentRevision)
	}
	return s.repo.Purge(ctx, thread.ID)
}

// CreateThread 创建帖子
func (s *ThreadService) CreateThread(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest) (*domain.Thread, error) {
	return s.CreateThreadWithOptions(ctx, authorID, authorName, req, CreateThreadOptions{
		Status:        domain.ThreadStatusPublished,
		ContentFormat: "markdown",
		ThreadType:    domain.ThreadTypeDiscussion,
	})
}

func (s *ThreadService) CreateThreadWithOptions(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest, opts CreateThreadOptions) (*domain.Thread, error) {
	return s.createThreadWithOptions(ctx, authorID, authorName, req, opts, nil)
}

// CreateStructuredThreadWithOptions executes the Community base Thread,
// Revision and a compiled-in feature detail participant as one reliable
// command. The participant runs after the base facts exist and before the
// required audit/outbox records are committed.
func (s *ThreadService) CreateStructuredThreadWithOptions(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest, opts CreateThreadOptions, participant StructuredThreadParticipant) (*domain.Thread, error) {
	if participant == nil {
		return nil, errors.New("structured thread participant is required")
	}
	if opts.ThreadType == "" {
		opts.ThreadType = participant.ThreadType()
	}
	if domain.NormalizeThreadType(opts.ThreadType) != domain.NormalizeThreadType(participant.ThreadType()) {
		return nil, fmt.Errorf("%w: participant type mismatch", ErrThreadTypeInvalid)
	}
	return s.createThreadWithOptions(ctx, authorID, authorName, req, opts, participant)
}

func (s *ThreadService) createThreadWithOptions(ctx context.Context, authorID, authorName string, req domain.CreateThreadRequest, opts CreateThreadOptions, participant StructuredThreadParticipant) (*domain.Thread, error) {
	now := time.Now().UTC()
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
	threadType := domain.NormalizeThreadType(opts.ThreadType)
	thread := &domain.Thread{
		ID:            strconv.FormatInt(idgen.New(), 10),
		ThreadType:    threadType,
		Title:         req.Title,
		Content:       req.Content,
		ContentFormat: contentFormat,
		AuthorID:      authorID,
		AuthorName:    authorName,
		CategoryID:    req.CategoryID,
		Status:        status,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	thread.NormalizeContentState()
	thread.CurrentRevision = 1

	commandCode := strings.TrimSpace(opts.CommandCode)
	if commandCode == "" {
		commandCode = "community.thread.create"
	}
	if participant != nil && commandCode == "community.thread.create" {
		commandCode = "community.thread.structured_create"
	}
	eventType := strings.TrimSpace(opts.EventType)
	if eventType == "" {
		eventType = eventbus.EventThreadCreated
	}
	return s.executeAuthorCommand(ctx, authorID, commandCode, thread.ID, eventType, func(commandCtx context.Context) (*domain.Thread, error) {
		defaultTags := []string{}
		if s.categoryRepo != nil {
			category, err := validatePostingCategory(commandCtx, s.categoryRepo, req.CategoryID)
			if err != nil {
				return nil, fmt.Errorf("validate posting category: %w", err)
			}
			defaultTags = category.DefaultTags
		}
		if err := s.validateThreadTypePolicy(commandCtx, thread.CategoryID, thread.ThreadType); err != nil {
			return nil, err
		}
		thread.Tags = mergeTags(defaultTags, req.Tags)
		if err := s.repo.Create(commandCtx, thread); err != nil {
			return nil, fmt.Errorf("create thread: %w", err)
		}
		if err := s.recordRevision(commandCtx, thread, authorID, "create", ""); err != nil {
			return nil, fmt.Errorf("record content revision: %w", err)
		}
		if participant != nil {
			if err := participant.PersistThreadDetail(commandCtx, thread); err != nil {
				return nil, fmt.Errorf("persist structured thread detail: %w", err)
			}
		}
		return thread, nil
	})
}

func (s *ThreadService) validateThreadTypePolicy(ctx context.Context, categoryID string, requested domain.ThreadType) error {
	threadType := domain.NormalizeThreadType(requested)
	if !domain.IsKnownThreadType(threadType) {
		return fmt.Errorf("%w: %s", ErrThreadTypeInvalid, requested)
	}
	if s.typeEnabled != nil && !s.typeEnabled(threadType) {
		return fmt.Errorf("%w: %s", ErrThreadTypeUnavailable, threadType)
	}
	// Standalone legacy tests can omit the policy adapter. They retain the
	// conservative historical default only; mutual aid and secondhand require
	// a real policy repository in production.
	if s.typePolicies == nil {
		if threadType == domain.ThreadTypeDiscussion || threadType == domain.ThreadTypeArticle {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrThreadTypeNotAllowed, threadType)
	}
	policies, err := s.typePolicies.List(ctx, categoryID)
	if err != nil {
		return fmt.Errorf("load category thread type policy: %w", err)
	}
	for _, policy := range policies {
		if policy.Enabled && domain.NormalizeThreadType(policy.ThreadType) == threadType {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrThreadTypeNotAllowed, threadType)
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
	if err := s.expandGroupCategoryFilter(ctx, &filter); err != nil {
		return nil, 0, fmt.Errorf("resolve thread category filter: %w", err)
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
	cacheKey := fmt.Sprintf("threads:list:%d:%d:%s:%s:%s", filter.Page, filter.PageSize, filter.Status, filter.ContentFormat, filter.ThreadType)
	cacheablePublicList := filter.Status == string(domain.ThreadStatusPublished) &&
		filter.Keyword == "" &&
		filter.CategoryID == "" &&
		filter.CategoryIDs == nil &&
		filter.AuthorID == "" &&
		filter.ContentFormat == "" &&
		filter.ThreadType == "" &&
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

// expandGroupCategoryFilter keeps the category_id HTTP contract stable while
// giving group nodes their intended read semantics. Threads can only belong to
// boards, so selecting a group becomes an explicit set filter over every
// active child board. A non-nil empty set deliberately matches no rows.
func (s *ThreadService) expandGroupCategoryFilter(ctx context.Context, filter *domain.ThreadListFilter) error {
	if filter == nil || strings.TrimSpace(filter.CategoryID) == "" || s.categoryRepo == nil {
		return nil
	}
	category, err := s.categoryRepo.GetByID(ctx, strings.TrimSpace(filter.CategoryID))
	if err != nil {
		return err
	}
	category.NormalizeHierarchy()
	if category.NodeKind != domain.CategoryNodeGroup {
		return nil
	}
	children, err := s.categoryRepo.ListChildren(ctx, category.ID)
	if err != nil {
		return err
	}
	filter.CategoryID = ""
	filter.CategoryIDs = make([]string, 0, len(children))
	for _, child := range children {
		child.NormalizeHierarchy()
		if child.NodeKind == domain.CategoryNodeBoard && child.LifecycleStatus == domain.CategoryLifecycleActive {
			filter.CategoryIDs = append(filter.CategoryIDs, child.ID)
		}
	}
	return nil
}

// UpdateThread 更新帖子
func (s *ThreadService) UpdateThread(ctx context.Context, id, authorID string, req domain.UpdateThreadRequest) (*domain.Thread, error) {
	return s.executeAuthorCommand(ctx, authorID, "community.thread.author_update", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
		return s.updateThread(commandCtx, id, authorID, req)
	})
}

func (s *ThreadService) updateThread(ctx context.Context, id, authorID string, req domain.UpdateThreadRequest) (*domain.Thread, error) {
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
		if thread.ThreadType != domain.ThreadTypeDiscussion {
			return nil, fmt.Errorf("%s status must be managed through its typed APIs", thread.ThreadType)
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

	if err := s.updateGoverned(ctx, thread); err != nil {
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
	return thread, nil
}

// SaveFeatureThread is the sole write path exposed to built-in content
// features. The caller can update content and express a publication intent,
// but Community keeps moderation and deletion state under its own control.
func (s *ThreadService) SaveFeatureThread(ctx context.Context, candidate *domain.Thread, actorID, action string) (*domain.Thread, error) {
	if candidate == nil || candidate.ID == "" {
		return nil, errors.New("thread content is required")
	}
	return s.executeAuthorCommand(ctx, actorID, "community.thread.feature_update", candidate.ID, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
		return s.saveFeatureThread(commandCtx, candidate, actorID, action)
	})
}

func (s *ThreadService) saveFeatureThread(ctx context.Context, candidate *domain.Thread, actorID, action string) (*domain.Thread, error) {
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	return s.TrashThread(ctx, id, authorID, "author_delete", "author moved content to trash")
}

// TrashThread is the internal command used by built-in content features. It
// still validates author ownership for author-originated actions; callers that
// act as moderators must have passed the HTTP/service authorization gate.
func (s *ThreadService) TrashThread(ctx context.Context, id, actorID, action, reason string) error {
	if actorID == "" {
		return errors.New("content trash actor is required")
	}
	if action == "" {
		action = "content_trash"
	}
	if reason == "" {
		reason = "content moved to trash"
	}
	commandCode := "community.thread.trash"
	if isAuthorTrashAction(action) {
		commandCode = "community.thread.author_trash"
	}
	_, err := s.executeAuthorCommand(ctx, actorID, commandCode, id, eventbus.EventThreadDeleted, func(commandCtx context.Context) (*domain.Thread, error) {
		thread, err := s.repo.GetByID(commandCtx, id)
		if err != nil {
			return nil, fmt.Errorf("get thread: %w", err)
		}
		if isAuthorTrashAction(action) && thread.AuthorID != actorID {
			return nil, fmt.Errorf("permission denied: you can only delete your own threads")
		}
		if err := s.trashThread(commandCtx, thread, actorID, action, reason); err != nil {
			return nil, err
		}
		return thread, nil
	})
	return err
}

func isAuthorTrashAction(action string) bool {
	return strings.HasPrefix(action, "author_") || strings.HasPrefix(action, "richtext_author_") || action == "richtext_create_rollback"
}

func (s *ThreadService) AdminDeleteThread(ctx context.Context, id, actorID string) error {
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceDelete(ctx, actorID, "community.thread.trash", id, func(commandCtx context.Context) error {
			return s.AdminDeleteThread(commandCtx, id, actorID)
		})
	}
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceCommand(ctx, actorID, "community.thread.take_down", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
			return s.TakeDown(commandCtx, id, actorID, reason)
		})
	}
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
	if err := s.updateGoverned(ctx, thread); err != nil {
		return nil, err
	}
	if err := s.recordTransition(ctx, thread, actorID, "take_down", reason, before, true); err != nil {
		return nil, err
	}
	if s.notifications != nil && thread.AuthorID != actorID {
		if err := s.notifications.NotifyThreadTakenDown(ctx, thread.AuthorID, thread.ID, thread.Title, reason); err != nil {
			return nil, err
		}
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceCommand(ctx, actorID, "community.thread.review", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
			return s.Approve(commandCtx, id, actorID, reason)
		})
	}
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceCommand(ctx, actorID, "community.thread.review", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
			return s.Reject(commandCtx, id, actorID, reason)
		})
	}
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceCommand(ctx, actorID, "community.thread.direct_restore", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
			return s.DirectRestore(commandCtx, id, actorID, reason)
		})
	}
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceCommand(ctx, actorID, "community.thread.restore", id, eventbus.EventThreadUpdated, func(commandCtx context.Context) (*domain.Thread, error) {
			return s.AdminRestoreFromTrash(commandCtx, id, actorID, reason)
		})
	}
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	if s.reliable != nil && !transaction.Active(ctx) {
		return s.executeGovernanceDelete(ctx, actorID, "community.thread.purge", id, func(commandCtx context.Context) error {
			return s.PurgeThread(commandCtx, id, actorID, reason)
		})
	}
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
	if err := s.purgeGoverned(ctx, thread); err != nil {
		return err
	}
	if err := s.recordModerationAction(ctx, thread, actorID, "purge", reason, before); err != nil {
		return err
	}
	s.invalidateListCache(ctx)
	if s.bus != nil && !transaction.Active(ctx) {
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
		return s.contentAuthorizationFailure(ctx, err, actorID, code, categoryID, "error", "scoped permission check failed")
	}
	if !allowed {
		return s.contentAuthorizationFailure(ctx, errors.New("permission denied: content governance scope"), actorID, code, categoryID, "deny", "content governance scope denied")
	}
	return s.recordContentAuthorizationDecision(ctx, actorID, code, categoryID, "allow", "")
}

func (s *ThreadService) contentAuthorizationFailure(ctx context.Context, err error, actorID, code string, categoryID int64, outcome, reason string) error {
	if !transaction.Active(ctx) {
		// A non-transactional legacy path can persist its decision immediately.
		_ = s.recordContentAuthorizationDecision(ctx, actorID, code, categoryID, outcome, reason)
		return err
	}
	return &contentAuthorizationFailure{
		err: err, actorID: actorID, code: code, categoryID: categoryID, outcome: outcome, reason: reason,
	}
}

func (s *ThreadService) recordRolledBackAuthorizationDecision(ctx context.Context, err error) {
	var decision *contentAuthorizationFailure
	if !errors.As(err, &decision) || decision == nil {
		return
	}
	// A deny/error audit is evidence for a request that made no business
	// change. It is intentionally best effort: a failed audit must never turn
	// the original denied request into an allowed one.
	_ = s.recordContentAuthorizationDecision(ctx, decision.actorID, decision.code, decision.categoryID, decision.outcome, decision.reason)
}

func (s *ThreadService) recordContentAuthorizationDecision(ctx context.Context, actorID, code string, categoryID int64, outcome, reason string) error {
	auditor, ok := s.authorization.(ContentAuthorizationAuditor)
	if !ok {
		return nil
	}
	return auditor.RecordContentAuthorizationDecision(ctx, actorID, code, categoryID, outcome, reason)
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
	if err := s.updateGoverned(ctx, thread); err != nil {
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
	if err := s.updateGoverned(ctx, thread); err != nil {
		return err
	}
	if err := s.recordTransition(ctx, thread, actorID, action, reason, before, false); err != nil {
		return err
	}
	if s.notifications != nil && !isAuthorTrashAction(action) && thread.AuthorID != actorID {
		return s.notifications.NotifyThreadTrashed(ctx, thread.AuthorID, thread.ID, thread.Title, reason)
	}
	return nil
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

func (s *ThreadService) publishThreadEvent(ctx context.Context, eventType string, thread *domain.Thread) {
	if s.bus == nil || thread == nil || transaction.Active(ctx) {
		return
	}
	_ = s.bus.Publish(ctx, eventbus.NewEvent(
		eventType, "campusos.community", "thread."+thread.ID, thread,
	))
}

func (s *ThreadService) publishThreadUpdated(ctx context.Context, thread *domain.Thread) {
	s.publishThreadEvent(ctx, eventbus.EventThreadUpdated, thread)
}

func ptrTime(value time.Time) *time.Time { return &value }

// invalidateListCache 清除帖子列表缓存
func (s *ThreadService) invalidateListCache(ctx context.Context) {
	if s.cache == nil || transaction.Active(ctx) {
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
