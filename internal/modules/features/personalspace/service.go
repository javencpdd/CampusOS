package space

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrInvalidVisibility     = errors.New("invalid space visibility")
	ErrInvalidStyleExport    = errors.New("invalid style export")
	ErrSpaceNotPublic        = errors.New("space is not public")
	ErrStyleSnapshotNotFound = errors.New("style snapshot not found")
	ErrSpacePluginDisabled   = errors.New("personal-space plugin is disabled")
)

type UserLookup interface {
	GetByID(ctx context.Context, id string) (*identitydomain.User, error)
	GetByUsername(ctx context.Context, username string) (*identitydomain.User, error)
}

// CompatibilityReporter is deliberately narrow so Personal Space can report
// rolling-upgrade reads without importing the platform reliability package.
// Core composition supplies it; legacy embedders can leave it unset.
type CompatibilityReporter interface {
	RecordCompatibility(context.Context, string, string, any) error
}

type Service struct {
	repo          Repository
	contentRepo   ContentRepository
	threadRepo    ThreadRepository
	contentQuery  communityport.ContentQuery
	users         UserLookup
	fileStore     *LocalFileStore
	enabled       func() bool
	compatibility CompatibilityReporter
}

func NewService(repo Repository, users UserLookup, contentRepos ...ContentRepository) *Service {
	var contentRepo ContentRepository
	if len(contentRepos) > 0 {
		contentRepo = contentRepos[0]
	} else if repo, ok := repo.(ContentRepository); ok {
		contentRepo = repo
	}
	return &Service{repo: repo, contentRepo: contentRepo, users: users}
}

func (s *Service) SetThreadRepository(repo ThreadRepository) {
	s.threadRepo = repo
}

// SetContentQuery switches public profile reads to Community's canonical
// visibility query. The legacy thread repository remains only for projection
// backfill and shadow comparison during the deprecation period.
func (s *Service) SetContentQuery(query communityport.ContentQuery) {
	s.contentQuery = query
}

func (s *Service) SetFileStore(store *LocalFileStore) {
	s.fileStore = store
}

func (s *Service) SetPluginEnabledChecker(checker func() bool) {
	s.enabled = checker
}

func (s *Service) SetCompatibilityReporter(reporter CompatibilityReporter) {
	s.compatibility = reporter
}

func (s *Service) recordLegacyContentProjectionUse(ctx context.Context, operation string) {
	if s.compatibility == nil {
		return
	}
	// Telemetry is advisory only. It must not make a public profile unreadable
	// while a rolling-upgrade instance still uses user_space_contents.
	_ = s.compatibility.RecordCompatibility(ctx, "legacy-user-space-contents", "personal-space", map[string]string{
		"operation": operation,
	})
}

func (s *Service) GetPublicByUserID(ctx context.Context, userID string) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return s.getPublicSpace(ctx, user)
}

func (s *Service) GetPublicByUsername(ctx context.Context, username string) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return s.getPublicSpace(ctx, user)
}

func (s *Service) GetOwnSpace(ctx context.Context, userID string) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}

	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrSpaceNotFound) {
			return buildPublicSpace(user, defaultSpace(user)), nil
		}
		return nil, fmt.Errorf("get space: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) UpsertOwnSpace(ctx context.Context, userID string, req UpsertSpaceRequest) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}

	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, fmt.Errorf("get space: %w", err)
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
		space.IsDefault = false
	}

	if err := applyUpdate(space, req); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if space.CreatedAt.IsZero() {
		space.CreatedAt = now
	}
	space.UpdatedAt = now
	space.UserID = user.ID
	space.IsDefault = false

	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("save space: %w", err)
	}

	return buildPublicSpace(user, space), nil
}

func (s *Service) ValidateSpaceStylePackage(_ context.Context, _ string, pkg StylePackage) (StyleValidationResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return StyleValidationResult{}, err
	}
	return ValidateStylePackage(pkg), nil
}

func (s *Service) PreviewStylePackage(ctx context.Context, userID string, pkg StylePackage) (*StylePreview, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	preview := BuildStylePreview(current.Owner, current.Space, pkg)
	return &preview, nil
}

func (s *Service) ExportStylePackage(ctx context.Context, userID string, req StyleExportRequest) (*StyleExportResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := BuildStyleExport(current.Owner, current.Space, req)
	if !result.Validation.Valid {
		return &result, fmt.Errorf("%w: %s", ErrInvalidStyleExport, strings.Join(result.Validation.Errors, "; "))
	}
	return &result, nil
}

func (s *Service) ApplyStylePackage(ctx context.Context, userID string, pkg StylePackage) (*StyleApplyResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := BuildStyleApply(current.Owner, current.Space, pkg)
	if !result.Validation.Valid {
		return &result, fmt.Errorf("%w: %s", ErrInvalidStyleExport, strings.Join(result.Validation.Errors, "; "))
	}

	manifest := result.Applied
	space := cloneSpace(current.Space)
	if space == nil {
		space = defaultSpace(&identitydomain.User{
			ID:       current.Owner.ID,
			Username: current.Owner.Username,
			Nickname: current.Owner.Nickname,
			Avatar:   current.Owner.Avatar,
			Bio:      current.Owner.Bio,
		})
	}

	now := time.Now().UTC()
	if space.ID == "" {
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	if space.CreatedAt.IsZero() {
		space.CreatedAt = now
	}
	if err := s.saveStyleSnapshot(ctx, space, "before_apply"); err != nil {
		return nil, err
	}
	space.UserID = current.Owner.ID
	space.Theme = manifest.Name
	space.Layout = manifest.Layout
	space.StyleName = manifest.Name
	space.StyleVersion = manifest.Version
	space.StyleManifest = manifest
	space.IsDefault = false
	space.UpdatedAt = now
	ensureDefaults(space)

	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("save space style: %w", err)
	}

	result.Space = cloneSpace(space)
	return &result, nil
}

func (s *Service) ValidateCustomHTML(ctx context.Context, userID string, html string) (*StyleValidationResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	result := ValidateCustomHTMLSnippet(html)
	return &result, nil
}

func (s *Service) CustomHTMLExample(ctx context.Context, userID string) (*StyleHTMLExampleResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := BuildStyleHTMLExample(current.Owner, current.Space)
	return &result, nil
}

func (s *Service) ApplyCustomHTML(ctx context.Context, userID string, html string) (*StyleApplyResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	pkg := BuildStyleHTMLPackage(current.Owner, current.Space, html)
	return s.ApplyStylePackage(ctx, userID, pkg)
}

func (s *Service) ValidateStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*StylePackResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	pack, validation := stylepack.LoadZip(reader, size)
	if validation.Valid {
		targetValidation := ensureStylePackTarget(pack, "personal-space")
		if !targetValidation.Valid {
			validation = targetValidation
		}
	}
	return &StylePackResult{Validation: validation, Package: pack}, nil
}

func (s *Service) StylePackExample(ctx context.Context, userID string) (*stylepack.FileBundle, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	example := BuildStylePackExample(current.Owner, current.Space)
	return &example, nil
}

func (s *Service) ListSourceStylePacks(ctx context.Context, userID string) (*stylepack.SourcePackList, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return listPersonalSourceStylePacks()
}

func (s *Service) ApplyStylePackZip(ctx context.Context, userID string, reader io.ReaderAt, size int64) (*StyleApplyResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	pack, validation := stylepack.LoadZip(reader, size)
	if validation.Valid {
		targetValidation := ensureStylePackTarget(pack, "personal-space")
		if !targetValidation.Valid {
			validation = targetValidation
		}
	}
	if !validation.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStyleExport, strings.Join(validation.Errors, "; "))
	}
	return s.ApplyStylePackage(ctx, userID, BuildStylePackPackage(current.Owner, current.Space, pack))
}

func (s *Service) ApplySourceStylePack(ctx context.Context, userID, name string) (*StyleApplyResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	current, err := s.GetOwnSpace(ctx, userID)
	if err != nil {
		return nil, err
	}
	pack, validation := loadPersonalSourceStylePack(name)
	if validation.Valid {
		targetValidation := ensureStylePackTarget(pack, "personal-space")
		if !targetValidation.Valid {
			validation = targetValidation
		}
	}
	if !validation.Valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStyleExport, strings.Join(validation.Errors, "; "))
	}
	return s.ApplyStylePackage(ctx, userID, BuildStylePackPackage(current.Owner, current.Space, pack))
}

func (s *Service) RollbackStyle(ctx context.Context, userID string) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	snapshotRepo, ok := s.repo.(StyleSnapshotRepository)
	if !ok {
		return nil, ErrStyleSnapshotNotFound
	}
	snapshot, err := snapshotRepo.GetLatestStyleSnapshot(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrSpaceNotFound) {
			return nil, ErrStyleSnapshotNotFound
		}
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	space, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, err
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	space.Theme = snapshot.Theme
	space.Layout = snapshot.Layout
	space.StyleName = snapshot.StyleName
	space.StyleVersion = snapshot.StyleVersion
	space.StyleManifest = cloneManifest(snapshot.StyleManifest)
	space.IsDefault = false
	space.UpdatedAt = time.Now().UTC()
	ensureDefaults(space)
	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("rollback style: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) RestoreDefaultStyle(ctx context.Context, userID string) (*PublicSpace, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	space, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, err
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	if err := s.saveStyleSnapshot(ctx, space, "before_restore_default"); err != nil {
		return nil, err
	}
	space.Theme = "default"
	space.Layout = "blog"
	clearAppliedStyle(space)
	space.IsDefault = false
	space.UpdatedAt = time.Now().UTC()
	ensureDefaults(space)
	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("restore default style: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) GetSyncStatus(ctx context.Context, userID string) (*SpaceSyncStatus, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	space, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, err
		}
		user, userErr := s.users.GetByID(ctx, userID)
		if userErr != nil {
			return nil, userErr
		}
		space = defaultSpace(user)
	}
	var total int64
	if ops, ok := s.repo.(OperationalRepository); ok {
		total, _ = ops.CountContentsByUserID(ctx, userID)
	}
	return &SpaceSyncStatus{
		UserID:        userID,
		SyncEnabled:   space.SyncEnabled,
		LastSyncAt:    space.LastSyncAt,
		LastSyncError: space.LastSyncError,
		ContentTotal:  total,
		Disabled:      space.DisabledAt != nil,
	}, nil
}

func (s *Service) DisableSpace(ctx context.Context, targetUserID, actorUserID, reason string) (*PublicSpace, error) {
	user, err := s.users.GetByID(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	space, err := s.repo.GetByUserID(ctx, targetUserID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, err
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	now := time.Now().UTC()
	space.DisabledAt = &now
	space.DisabledBy = strings.TrimSpace(actorUserID)
	space.DisabledReason = strings.TrimSpace(reason)
	space.UpdatedAt = now
	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("disable space: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) EnableSpace(ctx context.Context, targetUserID string) (*PublicSpace, error) {
	user, err := s.users.GetByID(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	space, err := s.repo.GetByUserID(ctx, targetUserID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, err
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	space.DisabledAt = nil
	space.DisabledBy = ""
	space.DisabledReason = ""
	space.UpdatedAt = time.Now().UTC()
	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("enable space: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) AdminSummary(ctx context.Context) (*SpaceAdminSummary, error) {
	ops, ok := s.repo.(OperationalRepository)
	if !ok {
		return &SpaceAdminSummary{}, nil
	}
	return ops.AdminSummary(ctx)
}

func (s *Service) StorageStatus(_ context.Context, userID string) (*SpaceStorageStatus, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.fileStore == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	return s.fileStore.Status(userID)
}

func (s *Service) UploadAvatar(ctx context.Context, userID, originalName string, reader io.Reader) (*AvatarUploadResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.fileStore == nil {
		return nil, ErrSpaceFileStoreUnavailable
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, fmt.Errorf("get space: %w", err)
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
	}
	fileName, size, err := s.fileStore.SaveAvatar(user.ID, originalName, reader)
	if err != nil {
		return nil, err
	}
	space.UserID = user.ID
	space.Avatar = s.fileStore.AvatarURL(user.ID, fileName)
	space.IsDefault = false
	if space.CreatedAt.IsZero() {
		space.CreatedAt = time.Now().UTC()
	}
	space.UpdatedAt = time.Now().UTC()
	ensureDefaults(space)
	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("save space avatar: %w", err)
	}
	status, err := s.fileStore.Status(user.ID)
	if err != nil {
		return nil, err
	}
	public := buildPublicSpace(user, space)
	return &AvatarUploadResult{
		FileName: fileName,
		URL:      space.Avatar,
		Size:     size,
		Storage:  *status,
		Owner:    public.Owner,
		Space:    public.Space,
	}, nil
}

func (s *Service) AvatarFilePath(userID, fileName string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	if s.fileStore == nil {
		return "", ErrSpaceFileStoreUnavailable
	}
	return s.fileStore.AvatarPath(userID, fileName)
}

func (s *Service) ensureEnabled() error {
	if s.enabled != nil && !s.enabled() {
		return ErrSpacePluginDisabled
	}
	return nil
}

func (s *Service) getPublicSpace(ctx context.Context, user *identitydomain.User) (*PublicSpace, error) {
	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrSpaceNotFound) {
			return buildPublicSpace(user, defaultSpace(user)), nil
		}
		return nil, fmt.Errorf("get space: %w", err)
	}
	if space.DisabledAt != nil || space.Visibility != VisibilityPublic {
		return nil, ErrSpaceNotPublic
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) saveStyleSnapshot(ctx context.Context, space *Space, snapshotType string) error {
	snapshotRepo, ok := s.repo.(StyleSnapshotRepository)
	if !ok || space == nil || space.UserID == "" {
		return nil
	}
	if space.ID == "" && space.StyleName == "" && space.Theme == "" && space.Layout == "" {
		return nil
	}
	snapshot := &StyleSnapshot{
		ID:            fmt.Sprintf("%d", idgen.New()),
		UserID:        space.UserID,
		SnapshotType:  snapshotType,
		StyleName:     space.StyleName,
		StyleVersion:  space.StyleVersion,
		Theme:         space.Theme,
		Layout:        space.Layout,
		StyleManifest: cloneManifest(space.StyleManifest),
		CreatedAt:     time.Now().UTC(),
	}
	ensureSnapshotDefaults(snapshot)
	return snapshotRepo.SaveStyleSnapshot(ctx, snapshot)
}

func ensureSnapshotDefaults(snapshot *StyleSnapshot) {
	if snapshot.SnapshotType == "" {
		snapshot.SnapshotType = "before_apply"
	}
	if snapshot.Theme == "" {
		snapshot.Theme = "default"
	}
	if snapshot.Layout == "" {
		snapshot.Layout = "blog"
	}
}

func cloneManifest(manifest *StyleManifest) *StyleManifest {
	if manifest == nil {
		return nil
	}
	clone := *manifest
	clone.SourceStylePack = cloneStylePackRef(manifest.SourceStylePack)
	clone.CompatibleCampusOS = append([]string(nil), manifest.CompatibleCampusOS...)
	clone.Components = append([]StyleComponent(nil), manifest.Components...)
	for i := range clone.Components {
		clone.Components[i].Props = copyInterfaceMap(manifest.Components[i].Props)
	}
	clone.Tokens = copyStringMap(manifest.Tokens)
	clone.Assets = append([]StyleAsset(nil), manifest.Assets...)
	return &clone
}

func applyUpdate(space *Space, req UpsertSpaceRequest) error {
	styleChanged := false
	if req.Title != nil {
		space.Title = strings.TrimSpace(*req.Title)
	}
	if req.Bio != nil {
		space.Bio = strings.TrimSpace(*req.Bio)
	}
	if req.Avatar != nil {
		space.Avatar = strings.TrimSpace(*req.Avatar)
	}
	if req.CoverImage != nil {
		space.CoverImage = strings.TrimSpace(*req.CoverImage)
	}
	if req.Theme != nil {
		theme := strings.TrimSpace(*req.Theme)
		styleChanged = styleChanged || theme != space.Theme
		space.Theme = theme
	}
	if req.Layout != nil {
		layout := strings.TrimSpace(*req.Layout)
		styleChanged = styleChanged || layout != space.Layout
		space.Layout = layout
	}
	if req.Visibility != nil {
		visibility := Visibility(strings.TrimSpace(*req.Visibility))
		if !validVisibility(visibility) {
			return ErrInvalidVisibility
		}
		space.Visibility = visibility
	}
	if req.SyncEnabled != nil {
		space.SyncEnabled = *req.SyncEnabled
	}
	if req.SyncCategories != nil {
		space.SyncCategories = normalizeList(req.SyncCategories, 20)
	}
	if req.SyncTags != nil {
		space.SyncTags = normalizeList(req.SyncTags, 20)
	}
	if styleChanged {
		clearAppliedStyle(space)
	}
	ensureDefaults(space)
	return nil
}

func clearAppliedStyle(space *Space) {
	space.StyleName = ""
	space.StyleVersion = ""
	space.StyleManifest = nil
}

func ensureDefaults(space *Space) {
	if strings.TrimSpace(space.Title) == "" {
		space.Title = "个人主页"
	}
	if strings.TrimSpace(space.Theme) == "" {
		space.Theme = "default"
	}
	if strings.TrimSpace(space.Layout) == "" {
		space.Layout = "blog"
	}
	if space.Visibility == "" {
		space.Visibility = VisibilityPublic
	}
	if space.SyncCategories == nil {
		space.SyncCategories = []string{}
	}
	if space.SyncTags == nil {
		space.SyncTags = []string{}
	}
}

func defaultSpace(user *identitydomain.User) *Space {
	now := time.Now().UTC()
	displayName := strings.TrimSpace(user.Nickname)
	if displayName == "" {
		displayName = user.Username
	}
	return &Space{
		UserID:         user.ID,
		Title:          displayName + "的个人主页",
		Bio:            user.Bio,
		Avatar:         user.Avatar,
		Theme:          "default",
		Layout:         "blog",
		Visibility:     VisibilityPublic,
		SyncEnabled:    true,
		SyncCategories: []string{},
		SyncTags:       []string{},
		IsDefault:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func buildPublicSpace(user *identitydomain.User, space *Space) *PublicSpace {
	avatar := user.Avatar
	if space != nil && strings.TrimSpace(space.Avatar) != "" {
		avatar = space.Avatar
	}
	spacePayload := cloneSpace(space)
	sanitizeCustomHTMLForResponse(spacePayload)
	return &PublicSpace{
		Owner: Owner{
			ID:       user.ID,
			Username: user.Username,
			Nickname: user.Nickname,
			Avatar:   avatar,
			Bio:      user.Bio,
		},
		Space: spacePayload,
	}
}

func sanitizeCustomHTMLForResponse(space *Space) {
	if space == nil || space.StyleManifest == nil {
		return
	}
	if strings.TrimSpace(space.StyleManifest.CustomCSS) != "" {
		if cssResult := stylepack.ValidateCSS(space.StyleManifest.CustomCSS); !cssResult.Valid {
			space.StyleManifest.CustomCSS = ""
		}
	}
	if strings.TrimSpace(space.StyleManifest.CustomHTML) == "" {
		return
	}
	if result := ValidateCustomHTMLSnippet(space.StyleManifest.CustomHTML); result.Valid {
		return
	}
	space.StyleManifest.CustomHTMLEnabled = false
	space.StyleManifest.CustomHTML = ""
	space.StyleManifest.CustomCSS = ""
}

func validVisibility(visibility Visibility) bool {
	switch visibility {
	case VisibilityPublic, VisibilityUnlisted, VisibilityPrivate:
		return true
	default:
		return false
	}
}

func normalizeList(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= limit {
			break
		}
	}
	return normalized
}
