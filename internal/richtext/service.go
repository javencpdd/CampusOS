package richtext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/community/domain"
	communityport "github.com/campusos/CampusOS/internal/community/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type Service struct {
	store     Store
	community communityport.ContentGateway
	assets    *LocalAssetStore
	enabled   func() bool
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
	thread, err := s.community.CreateThread(ctx, authorID, authorName, domain.CreateThreadRequest{
		Title:      strings.TrimSpace(req.Title),
		Content:    excerpt(sanitized.Text, 500),
		CategoryID: strings.TrimSpace(req.CategoryID),
		Tags:       req.Tags,
	}, communityport.ThreadCreateOptions{
		Status:        domain.ThreadStatusDraft,
		ContentFormat: ContentFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("create richtext thread: %w", err)
	}
	now := time.Now().UTC()
	article := &Article{
		ID:            fmt.Sprintf("%d", idgen.New()),
		ThreadID:      thread.ID,
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
	if err := s.store.CreateArticle(ctx, article); err != nil {
		_ = s.community.TrashThread(ctx, thread.ID, authorID, "richtext_create_rollback", "richtext article creation failed")
		return nil, fmt.Errorf("create richtext article: %w", err)
	}
	return articleResult(article), nil
}

func (s *Service) UpdateDraft(ctx context.Context, threadID, userID string, req SaveArticleRequest) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, thread, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	sanitized, err := sanitizeRequest(req)
	if err != nil {
		return nil, err
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
	savedThread, err := s.community.SaveFeatureThread(ctx, thread, userID, "richtext_save_draft")
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, savedThread)
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return articleResult(article), nil
}

func (s *Service) Publish(ctx context.Context, threadID, userID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, thread, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	sanitized, err := Sanitize(article.ContentHTML)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidArticle)
	}
	now := time.Now().UTC()
	article.SanitizedHTML = sanitized.HTML
	article.RenderHTML = RenderArticleHTML(sanitized.HTML)
	thread.Title = article.Title
	thread.Content = excerpt(sanitized.Text, 500)
	thread.ContentFormat = ContentFormat
	thread.PublicationStatus = domain.PublicationStatusPublished
	savedThread, err := s.community.SaveFeatureThread(ctx, thread, userID, "richtext_publish")
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, savedThread)
	if article.Status == StatusPublished {
		article.PublishedAt = &now
	}
	article.UpdatedBy = userID
	article.UpdatedAt = now
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return articleResult(article), nil
}

func (s *Service) Offline(ctx context.Context, threadID, userID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, thread, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	thread.PublicationStatus = domain.PublicationStatusDraft
	savedThread, err := s.community.SaveFeatureThread(ctx, thread, userID, "richtext_offline")
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, savedThread)
	article.UpdatedBy = userID
	article.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return articleResult(article), nil
}

func (s *Service) Delete(ctx context.Context, threadID, userID string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return err
	}
	if err := s.community.TrashThread(ctx, threadID, userID, "richtext_author_delete", "author moved richtext article to trash"); err != nil {
		return err
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	applyArticleThreadState(article, thread)
	article.UpdatedBy = userID
	article.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return err
	}
	s.invalidateList(ctx)
	return nil
}

func (s *Service) AdminOffline(ctx context.Context, threadID, adminID string) (*ArticleResult, error) {
	return s.AdminOfflineWithReason(ctx, threadID, adminID, "administrator marked richtext article offline")
}

func (s *Service) AdminOfflineWithReason(ctx context.Context, threadID, adminID, reason string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, _, err := s.managedArticle(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("moderation reason is required")
	}
	savedThread, err := s.community.TakeDownThread(ctx, threadID, adminID, reason)
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, savedThread)
	article.UpdatedBy = adminID
	article.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return articleResult(article), nil
}

func (s *Service) AdminRestore(ctx context.Context, threadID, adminID string) (*ArticleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, _, err := s.managedArticle(ctx, threadID)
	if err != nil {
		return nil, err
	}
	sanitized, err := Sanitize(article.ContentHTML)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidArticle)
	}
	article.SanitizedHTML = sanitized.HTML
	article.RenderHTML = RenderArticleHTML(sanitized.HTML)
	savedThread, err := s.community.RestoreThreadDirectly(ctx, threadID, adminID, "administrator restored richtext article")
	if err != nil {
		return nil, err
	}
	applyArticleThreadState(article, savedThread)
	if article.Status == StatusPublished {
		now := time.Now().UTC()
		article.PublishedAt = &now
	}
	article.UpdatedBy = adminID
	article.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return articleResult(article), nil
}

func (s *Service) AdminDelete(ctx context.Context, threadID, adminID string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	article, _, err := s.managedArticle(ctx, threadID)
	if err != nil {
		return err
	}
	if err := s.community.TrashThread(ctx, threadID, adminID, "richtext_admin_delete", "administrator moved richtext article to trash"); err != nil {
		return err
	}
	thread, err := s.community.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	applyArticleThreadState(article, thread)
	article.UpdatedBy = adminID
	article.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArticle(ctx, article); err != nil {
		return err
	}
	s.invalidateList(ctx)
	return nil
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
