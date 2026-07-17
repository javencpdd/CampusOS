package richtext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type Service struct {
	store     Store
	community communityport.ContentGateway
	assets    *LocalAssetStore
	enabled   func() bool
	reliable  *reliability.Service
}

func NewService(store Store, community communityport.ContentGateway) *Service {
	return &Service{store: store, community: community, enabled: func() bool { return true }}
}

func (s *Service) SetEnabledChecker(checker func() bool) {
	if checker == nil {
		s.enabled = func() bool { return true }
		return
	}
	s.enabled = checker
}

func (s *Service) SetAssetStore(store *LocalAssetStore) {
	s.assets = store
}

// SetReliability makes RichText own one durable command around its detail and
// the Community Thread write. The service still consumes only Community's
// public content gateway; it never receives a Community repository or a DB
// handle.
func (s *Service) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.store.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

func (s *Service) executeArticleCommand(ctx context.Context, actorID, code, resourceID, eventType string, aggregateID func() string, payload func() any, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		err := action(ctx)
		if err == nil && !transaction.Active(ctx) {
			s.invalidateList(ctx)
		}
		return err
	}

	err := s.reliable.Execute(ctx, reliability.Command{
		Code:          code,
		ActorID:       strings.TrimSpace(actorID),
		ActorType:     "user",
		ResourceType:  "richtext_article",
		ResourceID:    strings.TrimSpace(resourceID),
		OperationCode: code,
		EventFactory: func() (reliability.Event, error) {
			threadID := strings.TrimSpace(resourceID)
			if aggregateID != nil {
				threadID = strings.TrimSpace(aggregateID())
			}
			if threadID == "" {
				return reliability.Event{}, fmt.Errorf("richtext command did not resolve a thread id")
			}
			var eventPayload any = map[string]string{"thread_id": threadID, "action": code}
			if payload != nil {
				eventPayload = payload()
			}
			return reliability.NewEvent(eventType, "thread", threadID, eventPayload)
		},
	}, action)
	if err == nil {
		// The Community inner action sees the active TxKernel and defers cache
		// invalidation. Perform the shared list invalidation only after commit.
		s.invalidateList(ctx)
	}
	return err
}

func (s *Service) Status() StatusResult {
	return StatusResult{
		Enabled:       s.enabled == nil || s.enabled(),
		DefaultEditor: "controlled-html",
		PluginName:    PluginName,
	}
}

func (s *Service) CreateDraft(ctx context.Context, authorID, authorName string, req SaveArticleRequest) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CategoryID) == "" {
		return nil, fmt.Errorf("%w: category_id is required", ErrInvalidArticle)
	}
	sanitized, err := sanitizeRequest(req)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	article := &Article{
		ID:            fmt.Sprintf("%d", idgen.New()),
		Title:         strings.TrimSpace(req.Title),
		Summary:       strings.TrimSpace(req.Summary),
		CoverURL:      strings.TrimSpace(req.CoverURL),
		ContentHTML:   strings.TrimSpace(req.ContentHTML),
		ContentJSON:   normalizeJSON(req.ContentJSON),
		SanitizedHTML: sanitized.HTML,
		RenderHTML:    RenderArticleHTML(sanitized.HTML),
		Status:        StatusDraft,
		CreatedBy:     authorID,
		UpdatedBy:     authorID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err = s.community.CreateStructuredThread(ctx, authorID, authorName, domain.CreateThreadRequest{
		Title:      article.Title,
		Content:    excerpt(sanitized.Text, 500),
		CategoryID: strings.TrimSpace(req.CategoryID),
		Tags:       req.Tags,
	}, communityport.ThreadCreateOptions{
		Status:        domain.ThreadStatusDraft,
		ContentFormat: ContentFormat,
		ThreadType:    domain.ThreadTypeArticle,
	}, articleCreateParticipant{store: s.store, article: article})
	if err != nil {
		return nil, err
	}
	return articleResult(article), nil
}

// articleCreateParticipant is compiled together with the RichText Built-in
// Feature. It only writes the local detail row through the Store supplied by
// the feature and is invoked by Community inside the same TxKernel command.
// It has no filesystem, network, Host API, or external-plugin access.
type articleCreateParticipant struct {
	store   Store
	article *Article
}

func (p articleCreateParticipant) ThreadType() domain.ThreadType {
	return domain.ThreadTypeArticle
}

func (p articleCreateParticipant) PersistThreadDetail(ctx context.Context, thread *domain.Thread) error {
	if p.store == nil || p.article == nil || thread == nil || strings.TrimSpace(thread.ID) == "" {
		return fmt.Errorf("richtext article detail participant is unavailable")
	}
	p.article.ThreadID = thread.ID
	if err := p.store.CreateArticle(ctx, p.article); err != nil {
		return fmt.Errorf("create richtext article: %w", err)
	}
	return nil
}

func (s *Service) UpdateDraft(ctx context.Context, threadID, userID string, req SaveArticleRequest) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	sanitized, err := sanitizeRequest(req)
	if err != nil {
		return nil, err
	}
	var result *ArticleResult
	err = s.executeArticleCommand(ctx, userID, "feature.richtext.article.update", threadID, eventbus.EventThreadUpdated, func() string {
		return threadID
	}, func() any {
		return result
	}, func(commandCtx context.Context) error {
		article, thread, commandErr := s.editableArticle(commandCtx, threadID, userID)
		if commandErr != nil {
			return commandErr
		}
		now := time.Now().UTC()
		article.Title = strings.TrimSpace(req.Title)
		article.Summary = strings.TrimSpace(req.Summary)
		article.CoverURL = strings.TrimSpace(req.CoverURL)
		article.ContentHTML = strings.TrimSpace(req.ContentHTML)
		article.ContentJSON = normalizeJSON(req.ContentJSON)
		article.SanitizedHTML = sanitized.HTML
		article.RenderHTML = RenderArticleHTML(sanitized.HTML)
		article.UpdatedBy = userID
		article.UpdatedAt = now
		thread.Title = article.Title
		thread.Content = excerpt(sanitized.Text, 500)
		thread.ContentFormat = ContentFormat
		thread.PublicationStatus = domain.PublicationStatusDraft
		savedThread, commandErr := s.community.SaveFeatureThread(commandCtx, thread, userID, "richtext_save_draft")
		if commandErr != nil {
			return commandErr
		}
		applyArticleThreadState(article, savedThread)
		if commandErr := s.store.UpdateArticle(commandCtx, article); commandErr != nil {
			return commandErr
		}
		result = articleResult(article)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Publish(ctx context.Context, threadID, userID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	var result *ArticleResult
	err := s.executeArticleCommand(ctx, userID, "feature.richtext.article.publish", threadID, eventbus.EventThreadUpdated, func() string {
		return threadID
	}, func() any {
		return result
	}, func(commandCtx context.Context) error {
		article, thread, commandErr := s.editableArticle(commandCtx, threadID, userID)
		if commandErr != nil {
			return commandErr
		}
		sanitized, commandErr := Sanitize(article.ContentHTML)
		if commandErr != nil {
			return commandErr
		}
		if strings.TrimSpace(article.Title) == "" {
			return fmt.Errorf("%w: title is required", ErrInvalidArticle)
		}
		now := time.Now().UTC()
		article.SanitizedHTML = sanitized.HTML
		article.RenderHTML = RenderArticleHTML(sanitized.HTML)
		thread.Title = article.Title
		thread.Content = excerpt(sanitized.Text, 500)
		thread.ContentFormat = ContentFormat
		thread.PublicationStatus = domain.PublicationStatusPublished
		savedThread, commandErr := s.community.SaveFeatureThread(commandCtx, thread, userID, "richtext_publish")
		if commandErr != nil {
			return commandErr
		}
		applyArticleThreadState(article, savedThread)
		if article.Status == StatusPublished {
			article.PublishedAt = &now
		}
		article.UpdatedBy = userID
		article.UpdatedAt = now
		if commandErr := s.store.UpdateArticle(commandCtx, article); commandErr != nil {
			return commandErr
		}
		result = articleResult(article)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Offline(ctx context.Context, threadID, userID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	var result *ArticleResult
	err := s.executeArticleCommand(ctx, userID, "feature.richtext.article.offline", threadID, eventbus.EventThreadUpdated, func() string {
		return threadID
	}, func() any {
		return result
	}, func(commandCtx context.Context) error {
		article, thread, commandErr := s.editableArticle(commandCtx, threadID, userID)
		if commandErr != nil {
			return commandErr
		}
		thread.PublicationStatus = domain.PublicationStatusDraft
		savedThread, commandErr := s.community.SaveFeatureThread(commandCtx, thread, userID, "richtext_offline")
		if commandErr != nil {
			return commandErr
		}
		applyArticleThreadState(article, savedThread)
		article.UpdatedBy = userID
		article.UpdatedAt = time.Now().UTC()
		if commandErr := s.store.UpdateArticle(commandCtx, article); commandErr != nil {
			return commandErr
		}
		result = articleResult(article)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, threadID, userID string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	return s.executeArticleCommand(ctx, userID, "feature.richtext.article.author_trash", threadID, eventbus.EventThreadDeleted, func() string {
		return threadID
	}, nil, func(commandCtx context.Context) error {
		article, _, err := s.editableArticle(commandCtx, threadID, userID)
		if err != nil {
			return err
		}
		if err := s.community.TrashThread(commandCtx, threadID, userID, "richtext_author_delete", "author moved richtext article to trash"); err != nil {
			return err
		}
		thread, err := s.community.GetThread(commandCtx, threadID)
		if err != nil {
			return err
		}
		applyArticleThreadState(article, thread)
		article.UpdatedBy = userID
		article.UpdatedAt = time.Now().UTC()
		return s.store.UpdateArticle(commandCtx, article)
	})
}

func (s *Service) AdminOffline(ctx context.Context, threadID, adminID string) (*ArticleResult, error) {
	return s.AdminOfflineWithReason(ctx, threadID, adminID, "administrator marked richtext article offline")
}

func (s *Service) AdminOfflineWithReason(ctx context.Context, threadID, adminID, reason string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("moderation reason is required")
	}
	var result *ArticleResult
	err := s.executeArticleCommand(ctx, adminID, "feature.richtext.article.admin_offline", threadID, eventbus.EventThreadUpdated, func() string {
		return threadID
	}, func() any {
		return result
	}, func(commandCtx context.Context) error {
		article, _, commandErr := s.managedArticle(commandCtx, threadID)
		if commandErr != nil {
			return commandErr
		}
		savedThread, commandErr := s.community.TakeDownThread(commandCtx, threadID, adminID, reason)
		if commandErr != nil {
			return commandErr
		}
		applyArticleThreadState(article, savedThread)
		article.UpdatedBy = adminID
		article.UpdatedAt = time.Now().UTC()
		if commandErr := s.store.UpdateArticle(commandCtx, article); commandErr != nil {
			return commandErr
		}
		result = articleResult(article)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) AdminRestore(ctx context.Context, threadID, adminID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	var result *ArticleResult
	err := s.executeArticleCommand(ctx, adminID, "feature.richtext.article.admin_restore", threadID, eventbus.EventThreadUpdated, func() string {
		return threadID
	}, func() any {
		return result
	}, func(commandCtx context.Context) error {
		article, _, commandErr := s.managedArticle(commandCtx, threadID)
		if commandErr != nil {
			return commandErr
		}
		sanitized, commandErr := Sanitize(article.ContentHTML)
		if commandErr != nil {
			return commandErr
		}
		if strings.TrimSpace(article.Title) == "" {
			return fmt.Errorf("%w: title is required", ErrInvalidArticle)
		}
		article.SanitizedHTML = sanitized.HTML
		article.RenderHTML = RenderArticleHTML(sanitized.HTML)
		savedThread, commandErr := s.community.RestoreThreadDirectly(commandCtx, threadID, adminID, "administrator restored richtext article")
		if commandErr != nil {
			return commandErr
		}
		applyArticleThreadState(article, savedThread)
		if article.Status == StatusPublished {
			now := time.Now().UTC()
			article.PublishedAt = &now
		}
		article.UpdatedBy = adminID
		article.UpdatedAt = time.Now().UTC()
		if commandErr := s.store.UpdateArticle(commandCtx, article); commandErr != nil {
			return commandErr
		}
		result = articleResult(article)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) AdminDelete(ctx context.Context, threadID, adminID string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	return s.executeArticleCommand(ctx, adminID, "feature.richtext.article.admin_trash", threadID, eventbus.EventThreadDeleted, func() string {
		return threadID
	}, nil, func(commandCtx context.Context) error {
		article, _, err := s.managedArticle(commandCtx, threadID)
		if err != nil {
			return err
		}
		if err := s.community.TrashThread(commandCtx, threadID, adminID, "richtext_admin_delete", "administrator moved richtext article to trash"); err != nil {
			return err
		}
		thread, err := s.community.GetThread(commandCtx, threadID)
		if err != nil {
			return err
		}
		applyArticleThreadState(article, thread)
		article.UpdatedBy = adminID
		article.UpdatedAt = time.Now().UTC()
		return s.store.UpdateArticle(commandCtx, article)
	})
}

func (s *Service) Preview(_ context.Context, html string) (*PreviewResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	sanitized, err := Sanitize(html)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{SanitizedHTML: sanitized.HTML, RenderHTML: RenderArticleHTML(sanitized.HTML)}, nil
}

func (s *Service) GetArticle(ctx context.Context, threadID, viewerID string) (*Article, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, err := s.store.GetArticleByThreadID(ctx, threadID)
	if err != nil {
		return nil, err
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, thread)
	if thread.DeletionStatus != domain.DeletionStatusActive {
		return nil, ErrArticleNotFound
	}
	if thread.IsPublic() {
		return article, nil
	}
	if viewerID != "" && article.CreatedBy == viewerID && thread.IsAuthorVisible(viewerID) {
		return article, nil
	}
	return nil, ErrArticleNotFound
}

func (s *Service) UploadAsset(ctx context.Context, userID, originalName string, reader io.Reader, threadID, articleID string) (*Asset, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.assets == nil {
		return nil, ErrAssetUnavailable
	}
	asset, err := s.assets.Save(userID, originalName, reader)
	if err != nil {
		return nil, err
	}
	asset.ID = fmt.Sprintf("%d", idgen.New())
	asset.ThreadID = strings.TrimSpace(threadID)
	asset.ArticleContentID = strings.TrimSpace(articleID)
	asset.UploaderID = userID
	if err := s.store.SaveAsset(ctx, asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Service) AssetPath(userID, fileName string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	if s.assets == nil {
		return "", ErrAssetUnavailable
	}
	return s.assets.Path(userID, fileName)
}

func (s *Service) editableArticle(ctx context.Context, threadID, userID string) (*Article, *domain.Thread, error) {
	article, err := s.store.GetArticleByThreadID(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if article.CreatedBy != userID {
		return nil, nil, ErrPermissionDenied
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if thread.DeletionStatus == domain.DeletionStatusTrashed || thread.DeletionStatus == domain.DeletionStatusPurged {
		return nil, nil, ErrArticleNotFound
	}
	applyArticleThreadState(article, thread)
	return article, thread, nil
}

func (s *Service) managedArticle(ctx context.Context, threadID string) (*Article, *domain.Thread, error) {
	article, err := s.store.GetArticleByThreadID(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if thread.DeletionStatus == domain.DeletionStatusTrashed || thread.DeletionStatus == domain.DeletionStatusPurged {
		return nil, nil, ErrArticleNotFound
	}
	applyArticleThreadState(article, thread)
	return article, thread, nil
}

func (s *Service) ensureEnabled() error {
	if s.enabled != nil && !s.enabled() {
		return ErrPluginDisabled
	}
	return nil
}

func (s *Service) invalidateList(ctx context.Context) {
	if s.community != nil {
		s.community.InvalidateThreadList(ctx)
	}
}

func sanitizeRequest(req SaveArticleRequest) (SanitizeResult, error) {
	if strings.TrimSpace(req.Title) == "" {
		return SanitizeResult{}, fmt.Errorf("%w: title is required", ErrInvalidArticle)
	}
	if len(req.ContentJSON) > 0 && !json.Valid(req.ContentJSON) {
		return SanitizeResult{}, fmt.Errorf("%w: content_json must be valid JSON", ErrInvalidArticle)
	}
	return Sanitize(req.ContentHTML)
}

func articleResult(article *Article) *ArticleResult {
	return &ArticleResult{
		ThreadID:         article.ThreadID,
		ArticleContentID: article.ID,
		Status:           article.Status,
		Article:          article,
	}
}

// applyArticleThreadState makes Community's three-dimensional state the
// canonical visibility source. RichText keeps a display-friendly mirror, but
// it never decides whether content is public on its own.
func applyArticleThreadState(article *Article, thread *domain.Thread) {
	if article == nil || thread == nil {
		return
	}
	thread.NormalizeContentState()
	switch {
	case thread.DeletionStatus == domain.DeletionStatusPurged:
		article.Status = StatusDeleted
		article.PublishedAt = nil
	case thread.DeletionStatus == domain.DeletionStatusTrashed:
		article.Status = StatusTrashed
		article.PublishedAt = nil
	case thread.ModerationStatus == domain.ModerationStatusPending:
		article.Status = StatusPendingReview
		article.PublishedAt = nil
	case thread.ModerationStatus == domain.ModerationStatusRejected || thread.ModerationStatus == domain.ModerationStatusTakenDown:
		article.Status = StatusOffline
		article.PublishedAt = nil
	case thread.PublicationStatus == domain.PublicationStatusPublished && thread.ModerationStatus == domain.ModerationStatusClear:
		article.Status = StatusPublished
	default:
		article.Status = StatusDraft
		article.PublishedAt = nil
	}
}

func excerpt(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:max])) + "..."
}
