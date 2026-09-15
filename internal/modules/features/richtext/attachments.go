package richtext

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"strings"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	MaxArticleAttachmentBytes      int64 = 20 * 1024 * 1024
	MaxArticleAttachmentTotalBytes int64 = 40 * 1024 * 1024
	MaxArticleAttachmentCount            = 10
	pluginUIInvocationTTL                = 10 * time.Minute
)

// Keep the existing positive BIGINT contract, but do not expose timestamp-based
// sequence IDs in preview URLs. This ID is still not an authorization credential.
func newPDFInvocationID() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(9223372036854775807))
	if err != nil {
		return "", ErrAssetUnavailable
	}
	return value.Add(value, big.NewInt(1)).String(), nil
}

type AttachmentOpen struct {
	Attachment ArticleAttachment
	Object     corestorage.Object
	Reader     io.ReadCloser
}

// UploadAttachment stores one non-inline attachment through ObjectPort, then
// atomically records the owner-scoped Asset and article Binding where the
// configured store supports the active transaction.  The best-effort object
// cleanup protects the common post-write database failure path.
func (s *Service) UploadAttachment(ctx context.Context, userID, threadID, originalName, claimedMIME string, size int64, reader io.Reader) (*ArticleAttachment, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.objects == nil || reader == nil || size <= 0 || size > MaxArticleAttachmentBytes {
		if size > MaxArticleAttachmentBytes {
			return nil, ErrAssetTooLarge
		}
		return nil, ErrAttachmentContent
	}
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	attachments, err := s.store.ListAttachments(ctx, article.ID)
	if err != nil {
		return nil, err
	}
	if len(attachments) >= MaxArticleAttachmentCount {
		return nil, ErrAttachmentLimit
	}
	var total int64
	for _, item := range attachments {
		total += item.Asset.SizeBytes
	}
	if total+size > MaxArticleAttachmentTotalBytes {
		return nil, ErrAttachmentTotalSize
	}

	buffered := bufio.NewReaderSize(reader, 8192)
	mimeType, err := validateAttachment(buffered, originalName, claimedMIME)
	if err != nil {
		return nil, err
	}

	object, err := s.objects.Put(ctx, userID, corestorage.PutRequest{
		Namespace: "richtext", Purpose: "article_attachment", OriginalName: filepath.Base(strings.TrimSpace(originalName)),
		MimeType: mimeType, SizeHint: size, Reader: buffered,
	})
	if err != nil {
		if errors.Is(err, corestorage.ErrObjectQuota) {
			return nil, ErrAssetQuotaExceeded
		}
		return nil, err
	}
	cleanup := true
	assetCreated := false
	var asset *UserAsset
	defer func() {
		if cleanup {
			if assetCreated {
				_ = s.store.DeleteUserAsset(context.Background(), asset.ID, userID)
			}
			_ = s.objects.Delete(context.Background(), userID, object.ID, object.Version)
		}
	}()
	// DOCX/XLSX are ZIP containers. A magic check alone would accept an
	// arbitrary archive renamed to an Office document. Re-open it only through
	// ObjectPort, verify the required internal entries, and remove the
	// temporary object before it becomes a user-visible Asset on failure.
	if err := s.validateOOXMLObject(ctx, userID, object, mimeType); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	asset = &UserAsset{ID: fmt.Sprintf("%d", idgen.New()), OwnerID: userID, Kind: AssetKindAttachment,
		OriginalName: object.OriginalName, StorageObjectID: object.ID, MimeType: object.MimeType, SizeBytes: object.SizeBytes,
		Status: AssetStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateUserAsset(ctx, asset); err != nil {
		return nil, err
	}
	assetCreated = true
	attachment := &ArticleAttachment{ID: fmt.Sprintf("%d", idgen.New()), ArticleContentID: article.ID, AssetID: asset.ID,
		DisplayName: asset.OriginalName, DisplayOrder: len(attachments), CreatedAt: now, UpdatedAt: now, Asset: *asset}
	if err := s.store.CreateAttachment(ctx, attachment); err != nil {
		return nil, err
	}
	cleanup = false
	return attachment, nil
}

func (s *Service) AttachExisting(ctx context.Context, userID, threadID, assetID, displayName string) (*ArticleAttachment, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return nil, err
	}
	asset, err := s.store.GetUserAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if asset.OwnerID != userID || asset.Status != AssetStatusActive || asset.Kind != AssetKindAttachment {
		return nil, ErrPermissionDenied
	}
	attachments, err := s.store.ListAttachments(ctx, article.ID)
	if err != nil {
		return nil, err
	}
	if len(attachments) >= MaxArticleAttachmentCount {
		return nil, ErrAttachmentLimit
	}
	var total int64
	for _, item := range attachments {
		total += item.Asset.SizeBytes
	}
	if total+asset.SizeBytes > MaxArticleAttachmentTotalBytes {
		return nil, ErrAttachmentTotalSize
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = asset.OriginalName
	}
	if len([]rune(name)) > 255 {
		return nil, ErrAttachmentContent
	}
	now := time.Now().UTC()
	attachment := &ArticleAttachment{ID: fmt.Sprintf("%d", idgen.New()), ArticleContentID: article.ID, AssetID: asset.ID,
		DisplayName: name, DisplayOrder: len(attachments), CreatedAt: now, UpdatedAt: now, Asset: *asset}
	if err := s.store.CreateAttachment(ctx, attachment); err != nil {
		return nil, err
	}
	return attachment, nil
}

func (s *Service) RemoveAttachment(ctx context.Context, userID, threadID, attachmentID string) error {
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return err
	}
	return s.store.RemoveAttachment(ctx, article.ID, attachmentID)
}

func (s *Service) RenameAttachment(ctx context.Context, userID, threadID, attachmentID, displayName string) error {
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(displayName)
	if name == "" || len([]rune(name)) > 255 || strings.ContainsAny(name, "\r\n") {
		return ErrAttachmentContent
	}
	return s.store.UpdateAttachmentDisplayName(ctx, article.ID, attachmentID, name)
}

func (s *Service) ReorderAttachments(ctx context.Context, userID, threadID string, ids []string) error {
	article, _, err := s.editableArticle(ctx, threadID, userID)
	if err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) > MaxArticleAttachmentCount {
		return ErrAttachmentLimit
	}
	current, err := s.store.ListAttachments(ctx, article.ID)
	if err != nil {
		return err
	}
	if len(current) != len(ids) {
		return ErrAttachmentNotFound
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return ErrAttachmentNotFound
		}
		seen[id] = true
	}
	return s.store.ReorderAttachments(ctx, article.ID, ids)
}

func (s *Service) ListArticleAttachments(ctx context.Context, viewerID, threadID string) ([]ArticleAttachment, error) {
	if strings.TrimSpace(viewerID) == "" {
		return nil, ErrPermissionDenied
	}
	article, err := s.GetArticle(ctx, threadID, viewerID)
	if err != nil {
		return nil, err
	}
	return append([]ArticleAttachment(nil), article.Attachments...), nil
}

func (s *Service) ListMyUserAssets(ctx context.Context, userID, status string) ([]*UserAsset, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrPermissionDenied
	}
	statuses := []string{AssetStatusActive}
	switch strings.TrimSpace(status) {
	case "", AssetStatusActive:
	case AssetStatusTrashed:
		statuses = []string{AssetStatusTrashed}
	default:
		return nil, ErrAssetInvalid
	}
	return s.store.ListUserAssetsByOwner(ctx, userID, statuses)
}

func (s *Service) TrashUserAsset(ctx context.Context, userID, assetID string) error {
	count, err := s.store.AssetReferenceCount(ctx, assetID)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrAssetReferenced
	}
	return s.transitionUserAsset(ctx, assetID, userID, []string{AssetStatusActive}, AssetStatusTrashed, userID, "user", "trashed", "用户移入回收站")
}

func (s *Service) RestoreUserAsset(ctx context.Context, userID, assetID string) error {
	return s.transitionUserAsset(ctx, assetID, userID, []string{AssetStatusTrashed}, AssetStatusActive, userID, "user", "restored", "用户从回收站恢复")
}

// AssetGovernanceSummary is aggregate-only and intentionally suitable for an
// administrator dashboard: it does not enumerate names, owners, storage keys
// or payloads.
func (s *Service) AssetGovernanceSummary(ctx context.Context) (AssetGovernanceSummary, error) {
	return s.store.AssetGovernanceSummary(ctx, time.Now().UTC())
}

func (s *Service) PreviewAssetPurge(ctx context.Context, assetID string) (AssetPurgePreview, error) {
	asset, err := s.store.GetUserAsset(ctx, strings.TrimSpace(assetID))
	if err != nil {
		return AssetPurgePreview{}, err
	}
	refs, err := s.store.AssetReferenceCount(ctx, asset.ID)
	if err != nil {
		return AssetPurgePreview{}, err
	}
	preview := AssetPurgePreview{AssetID: asset.ID, Status: asset.Status, SizeBytes: asset.SizeBytes, ReferenceCount: refs}
	if asset.Status != AssetStatusTrashed && asset.Status != AssetStatusQuarantined && asset.Status != AssetStatusPurging {
		preview.Reason = "仅回收站或隔离状态的附件可以清除。"
		return preview, nil
	}
	if refs > 0 {
		preview.Reason = "附件仍被文章正文或附件绑定引用，不能清除。"
		return preview, nil
	}
	preview.Eligible = true
	if asset.Status == AssetStatusPurging {
		preview.Reason = "检测到上一次清除未完成；再次确认会安全完成对象或元数据清理，且不可恢复。"
		return preview, nil
	}
	preview.Reason = "可清除：该操作将删除原始对象并释放个人空间，且不可恢复。"
	return preview, nil
}

// AdminQuarantineAsset immediately removes an Asset from all article reads.
// It intentionally does not delete bytes; recovery remains possible until a
// separately confirmed purge succeeds.
func (s *Service) AdminQuarantineAsset(ctx context.Context, adminID, assetID, reason string) error {
	asset, err := s.store.GetUserAsset(ctx, strings.TrimSpace(assetID))
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ErrAssetInvalid
	}
	return s.transitionUserAsset(ctx, asset.ID, asset.OwnerID, []string{AssetStatusActive, AssetStatusTrashed}, AssetStatusQuarantined, adminID, "admin", "quarantined", reason)
}

// AdminRestoreQuarantinedAsset clears only an explicit quarantine. It never
// restores a purging/deleted object and therefore cannot resurrect data after
// physical deletion.
func (s *Service) AdminRestoreQuarantinedAsset(ctx context.Context, adminID, assetID, reason string) error {
	asset, err := s.store.GetUserAsset(ctx, strings.TrimSpace(assetID))
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ErrAssetInvalid
	}
	return s.transitionUserAsset(ctx, asset.ID, asset.OwnerID, []string{AssetStatusQuarantined}, AssetStatusActive, adminID, "admin", "restored", reason)
}

// AdminPurgeAsset is a two-phase destructive action. It first persists the
// unavailable `purging` state, then deletes through ObjectPort, then marks the
// metadata `deleted`. A provider failure moves the asset back to its prior
// state and records a bounded audit reason; it never silently removes a
// referenced asset.
func (s *Service) AdminPurgeAsset(ctx context.Context, adminID, assetID, reason string) error {
	preview, err := s.PreviewAssetPurge(ctx, assetID)
	if err != nil {
		return err
	}
	if !preview.Eligible {
		return ErrAssetPurgeIneligible
	}
	asset, err := s.store.GetUserAsset(ctx, preview.AssetID)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 || s.objects == nil {
		return ErrAssetInvalid
	}
	previous := asset.Status
	startedHere := previous != AssetStatusPurging
	if startedHere {
		if err := s.transitionUserAsset(ctx, asset.ID, asset.OwnerID, []string{previous}, AssetStatusPurging, adminID, "admin", "purge_started", reason); err != nil {
			return err
		}
	}
	object, err := s.objects.Stat(ctx, asset.OwnerID, asset.StorageObjectID)
	if errors.Is(err, corestorage.ErrObjectNotFound) {
		// The object was removed before metadata finalisation completed.  A retry
		// can therefore finish safely without attempting to restore or recreate it.
		err = nil
	} else if err == nil {
		err = s.objects.Delete(ctx, asset.OwnerID, asset.StorageObjectID, object.Version)
	}
	if err != nil {
		// The object was not confirmed deleted, so returning to the previous
		// non-readable state is safe and lets an operator retry after fixing
		// the Provider. Preserve only a short, non-sensitive audit reason.
		if startedHere {
			_ = s.transitionUserAsset(context.Background(), asset.ID, asset.OwnerID, []string{AssetStatusPurging}, previous, adminID, "admin", "purge_failed", "存储对象删除失败，已恢复为原状态")
		}
		return err
	}
	if err := s.transitionUserAsset(ctx, asset.ID, asset.OwnerID, []string{AssetStatusPurging}, AssetStatusDeleted, adminID, "admin", "purged", reason); err != nil {
		// The object has already been removed. Keep purging state unavailable;
		// an operator can safely repeat the metadata completion after diagnosis.
		return err
	}
	return nil
}

func (s *Service) transitionUserAsset(ctx context.Context, assetID, ownerID string, from []string, status, actorID, actorType, action, reason string) error {
	err := s.store.TransitionUserAsset(ctx, assetID, ownerID, from, status, AssetLifecycleAudit{
		ActorID: actorID, ActorType: actorType, Action: action, Reason: reason,
	})
	if err == nil {
		s.refreshAssetMetrics(ctx)
	}
	return err
}

func (s *Service) OpenAttachment(ctx context.Context, viewerID, threadID, attachmentID string) (AttachmentOpen, error) {
	if strings.TrimSpace(viewerID) == "" {
		return AttachmentOpen{}, ErrPermissionDenied
	}
	article, err := s.GetArticle(ctx, threadID, viewerID)
	if err != nil {
		return AttachmentOpen{}, err
	}
	attachment, err := s.store.GetAttachment(ctx, article.ID, attachmentID)
	if err != nil {
		return AttachmentOpen{}, err
	}
	if attachment.Asset.Status != AssetStatusActive {
		return AttachmentOpen{}, ErrAssetNotFound
	}
	if s.objects == nil {
		return AttachmentOpen{}, ErrAssetUnavailable
	}
	object, err := s.objects.Open(ctx, attachment.Asset.OwnerID, attachment.Asset.StorageObjectID)
	if err != nil {
		return AttachmentOpen{}, err
	}
	return AttachmentOpen{Attachment: *attachment, Object: object.Object, Reader: object.Reader}, nil
}

// OpenMyUserAsset is the only direct personal-space byte path for User Assets.
// It intentionally returns not-found for another user's asset so an opaque
// asset ID cannot be used to learn whether a private object exists.
func (s *Service) OpenMyUserAsset(ctx context.Context, userID, assetID string) (AttachmentOpen, error) {
	if strings.TrimSpace(userID) == "" {
		return AttachmentOpen{}, ErrPermissionDenied
	}
	asset, err := s.store.GetUserAsset(ctx, strings.TrimSpace(assetID))
	if err != nil {
		return AttachmentOpen{}, err
	}
	if asset.OwnerID != userID || asset.Status != AssetStatusActive {
		return AttachmentOpen{}, ErrAssetNotFound
	}
	if s.objects == nil {
		return AttachmentOpen{}, ErrAssetUnavailable
	}
	object, err := s.objects.Open(ctx, userID, asset.StorageObjectID)
	if err != nil {
		return AttachmentOpen{}, err
	}
	return AttachmentOpen{
		Attachment: ArticleAttachment{ID: asset.ID, AssetID: asset.ID, DisplayName: asset.OriginalName, Asset: *asset},
		Object:     object.Object,
		Reader:     object.Reader,
	}, nil
}

func (s *Service) CreatePDFInvocation(ctx context.Context, userID, threadID, attachmentID, presentation string) (*PluginUIInvocation, error) {
	if err := s.ensurePDFViewerEnabled(); err != nil {
		return nil, err
	}
	if !allowedPresentation(presentation) {
		return nil, ErrInvocationPresentation
	}
	opened, err := s.OpenAttachment(ctx, userID, threadID, attachmentID)
	if err != nil {
		return nil, err
	}
	_ = opened.Reader.Close()
	if opened.Attachment.Asset.MimeType != "application/pdf" {
		return nil, ErrAttachmentType
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, CapabilityCode: "article_attachment.self.preview", OperationCode: "invocation.create", Purpose: "article_attachment_pdf_preview"}); err != nil {
		return nil, err
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "plugin_ui.surface.open", OperationCode: "surface.open", Purpose: "article_attachment_pdf_preview"}); err != nil {
		return nil, err
	}
	invocationID, err := newPDFInvocationID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	invocation := &PluginUIInvocation{ID: invocationID, UserID: userID, PluginKey: PDFViewerPluginKey,
		SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextArticleAttachment, ArticleContentID: opened.Attachment.ArticleContentID, AssetID: opened.Attachment.AssetID,
		AttachmentID: opened.Attachment.ID, Presentation: presentation, Purpose: "article_attachment_pdf_preview", ExpiresAt: now.Add(pluginUIInvocationTTL), CreatedAt: now}
	if err := s.store.CreateInvocation(ctx, invocation); err != nil {
		return nil, err
	}
	return invocation, nil
}

// CreatePersonalAssetPDFInvocation opens the exact same Plugin UI v2 Surface
// as article attachments, but creates an owner-only context with no article
// reference.  It does not expose a storage key or mint a public URL.
func (s *Service) CreatePersonalAssetPDFInvocation(ctx context.Context, userID, assetID, presentation string) (*PluginUIInvocation, error) {
	if err := s.ensurePDFViewerEnabled(); err != nil {
		return nil, err
	}
	if !allowedPresentation(presentation) {
		return nil, ErrInvocationPresentation
	}
	opened, err := s.OpenMyUserAsset(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	_ = opened.Reader.Close()
	if opened.Attachment.Asset.MimeType != "application/pdf" {
		return nil, ErrAttachmentType
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "personal_space_file.self.read", OperationCode: "invocation.create", Purpose: "personal_asset_pdf_preview"}); err != nil {
		return nil, err
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "plugin_ui.surface.open", OperationCode: "surface.open", Purpose: "personal_asset_pdf_preview"}); err != nil {
		return nil, err
	}
	invocationID, err := newPDFInvocationID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	invocation := &PluginUIInvocation{ID: invocationID, UserID: userID, PluginKey: PDFViewerPluginKey,
		SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextPersonalAsset, AssetID: opened.Attachment.AssetID,
		Presentation: presentation, Purpose: "personal_asset_pdf_preview", ExpiresAt: now.Add(pluginUIInvocationTTL), CreatedAt: now}
	if err := s.store.CreateInvocation(ctx, invocation); err != nil {
		return nil, err
	}
	return invocation, nil
}

// CreatePersonalDocumentPDFInvocation opens the existing first-party PDF
// Viewer Surface for one owner-scoped Personal Documents PDF. The document
// remains in its original module and Object Port namespace; this method only
// records the short-lived plugin context and never creates a duplicate Asset.
func (s *Service) CreatePersonalDocumentPDFInvocation(ctx context.Context, userID, documentID, presentation string) (*PluginUIInvocation, error) {
	if err := s.ensurePDFViewerEnabled(); err != nil {
		return nil, err
	}
	if !allowedPresentation(presentation) {
		return nil, ErrInvocationPresentation
	}
	document, err := s.openPersonalDocumentPDF(ctx, userID, documentID)
	if err != nil {
		return nil, err
	}
	_ = document.Object.Reader.Close()
	if document.Format != "pdf" || document.Object.Object.MimeType != "application/pdf" {
		return nil, ErrAttachmentType
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "personal_space_file.self.read", OperationCode: "invocation.create", Purpose: "personal_document_pdf_preview"}); err != nil {
		return nil, err
	}
	if err := s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "plugin_ui.surface.open", OperationCode: "surface.open", Purpose: "personal_document_pdf_preview"}); err != nil {
		return nil, err
	}
	invocationID, err := newPDFInvocationID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	invocation := &PluginUIInvocation{ID: invocationID, UserID: userID, PluginKey: PDFViewerPluginKey,
		SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextPersonalDocument, PersonalDocumentID: document.ID,
		Presentation: presentation, Purpose: "personal_document_pdf_preview", ExpiresAt: now.Add(pluginUIInvocationTTL), CreatedAt: now}
	if err := s.store.CreateInvocation(ctx, invocation); err != nil {
		return nil, err
	}
	return invocation, nil
}

func (s *Service) openPersonalDocumentPDF(ctx context.Context, userID, documentID string) (PersonalDocumentPDF, error) {
	if s.personalDocumentReader == nil {
		return PersonalDocumentPDF{}, ErrAssetUnavailable
	}
	return s.personalDocumentReader.OpenOwnPDFDocument(ctx, userID, documentID)
}

func (s *Service) OpenPDFInvocation(ctx context.Context, userID, invocationID string) (AttachmentOpen, *PluginUIInvocation, error) {
	if err := s.ensurePDFViewerEnabled(); err != nil {
		return AttachmentOpen{}, nil, err
	}
	if strings.TrimSpace(userID) == "" {
		return AttachmentOpen{}, nil, ErrPermissionDenied
	}
	invocation, err := s.store.GetInvocation(ctx, invocationID)
	if err != nil {
		return AttachmentOpen{}, nil, err
	}
	if invocation.UserID != userID || invocation.PluginKey != PDFViewerPluginKey || invocation.SurfaceID != PDFViewerSurfaceID ||
		invocation.RevokedAt != nil || !invocation.ExpiresAt.After(time.Now().UTC()) {
		return AttachmentOpen{}, nil, ErrInvocationExpired
	}
	if err := s.store.MarkInvocationOpened(ctx, invocationID, userID); err != nil {
		return AttachmentOpen{}, nil, err
	}
	var opened AttachmentOpen
	switch invocation.ContextKind {
	case InvocationContextArticleAttachment:
		article, articleErr := s.store.GetArticleByContentID(ctx, invocation.ArticleContentID)
		if articleErr != nil {
			return AttachmentOpen{}, nil, articleErr
		}
		opened, err = s.OpenAttachment(ctx, userID, article.ThreadID, invocation.AttachmentID)
	case InvocationContextPersonalAsset:
		opened, err = s.OpenMyUserAsset(ctx, userID, invocation.AssetID)
	case InvocationContextPersonalDocument:
		var document PersonalDocumentPDF
		document, err = s.openPersonalDocumentPDF(ctx, userID, invocation.PersonalDocumentID)
		if err == nil {
			opened = AttachmentOpen{
				Attachment: ArticleAttachment{ID: document.ID, DisplayName: document.Name, Asset: UserAsset{
					OwnerID: userID, OriginalName: document.Name, MimeType: document.Object.Object.MimeType,
					SizeBytes: document.Object.Object.SizeBytes, StorageObjectID: document.Object.Object.ID,
				}},
				Object: document.Object.Object,
				Reader: document.Object.Reader,
			}
		}
	default:
		return AttachmentOpen{}, nil, ErrInvocationNotFound
	}
	if err != nil {
		return AttachmentOpen{}, nil, err
	}
	if opened.Attachment.Asset.MimeType != "application/pdf" ||
		(invocation.ContextKind == InvocationContextPersonalDocument && opened.Attachment.ID != invocation.PersonalDocumentID) ||
		(invocation.ContextKind != InvocationContextPersonalDocument && opened.Attachment.AssetID != invocation.AssetID) {
		_ = opened.Reader.Close()
		return AttachmentOpen{}, nil, ErrInvocationNotFound
	}
	if invocation.ContextKind == InvocationContextArticleAttachment {
		err = s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, CapabilityCode: "article_attachment.self.preview", OperationCode: "content.read", Purpose: invocation.Purpose})
	} else {
		err = s.authorizePDFViewer(ctx, PDFViewerAuthorizationInput{UserID: userID, ResourceOwnerID: userID, CapabilityCode: "personal_space_file.self.read", OperationCode: "content.read", Purpose: invocation.Purpose})
	}
	if err != nil {
		_ = opened.Reader.Close()
		return AttachmentOpen{}, nil, err
	}
	return opened, invocation, nil
}

func (s *Service) DescribePDFInvocation(ctx context.Context, userID, invocationID string) (*ArticleAttachment, *PluginUIInvocation, error) {
	opened, invocation, err := s.OpenPDFInvocation(ctx, userID, invocationID)
	if err != nil {
		return nil, nil, err
	}
	_ = opened.Reader.Close()
	return &opened.Attachment, invocation, nil
}

func allowedPresentation(value string) bool {
	switch value {
	case "modal", "drawer", "fullscreen", "new-tab":
		return true
	default:
		return false
	}
}

func validateAttachment(reader *bufio.Reader, originalName, claimedMIME string) (string, error) {
	name := filepath.Base(strings.TrimSpace(originalName))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || len([]rune(name)) > 255 {
		return "", ErrAttachmentContent
	}
	ext := strings.ToLower(filepath.Ext(name))
	head, err := reader.Peek(8192)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", ErrAttachmentContent
	}
	if len(head) == 0 {
		return "", ErrAttachmentContent
	}
	claimed := strings.ToLower(strings.TrimSpace(strings.Split(claimedMIME, ";")[0]))
	valid := func(mime string, matches bool, allowedClaims ...string) (string, error) {
		if !matches {
			return "", ErrAttachmentContent
		}
		if claimed != "" && claimed != "application/octet-stream" {
			ok := false
			for _, value := range allowedClaims {
				if claimed == value {
					ok = true
					break
				}
			}
			if !ok {
				return "", ErrAttachmentContent
			}
		}
		return mime, nil
	}
	zip := bytes.HasPrefix(head, []byte("PK\x03\x04")) || bytes.HasPrefix(head, []byte("PK\x05\x06"))
	ole := bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	switch ext {
	case ".pdf":
		return valid("application/pdf", bytes.HasPrefix(head, []byte("%PDF-")), "application/pdf")
	case ".mp3":
		return valid("audio/mpeg", bytes.HasPrefix(head, []byte("ID3")) || (len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0), "audio/mpeg", "audio/mp3")
	case ".mp4":
		return valid("video/mp4", len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")), "video/mp4")
	case ".docx":
		return valid("application/vnd.openxmlformats-officedocument.wordprocessingml.document", zip, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".xlsx":
		return valid("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", zip, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	case ".doc":
		return valid("application/msword", ole, "application/msword")
	case ".xls":
		return valid("application/vnd.ms-excel", ole, "application/vnd.ms-excel")
	case ".zip":
		return valid("application/zip", zip, "application/zip", "application/x-zip-compressed")
	case ".rar":
		return valid("application/vnd.rar", bytes.HasPrefix(head, []byte("Rar!\x1A\x07\x00")) || bytes.HasPrefix(head, []byte("Rar!\x1A\x07\x01\x00")), "application/vnd.rar", "application/x-rar-compressed")
	default:
		return "", ErrAttachmentType
	}
}

func (s *Service) validateOOXMLObject(ctx context.Context, ownerID string, object corestorage.Object, mimeType string) error {
	var required string
	switch mimeType {
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		required = "word/document.xml"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		required = "xl/workbook.xml"
	default:
		return nil
	}
	opened, err := s.objects.Open(ctx, ownerID, object.ID)
	if err != nil {
		return ErrAttachmentContent
	}
	defer opened.Reader.Close()
	// The upload handler already has a strict 20 MiB request cap. Limit this
	// validation read as a second defense so provider metadata drift cannot
	// turn ZIP inspection into an unbounded allocation.
	payload, err := io.ReadAll(io.LimitReader(opened.Reader, MaxArticleAttachmentBytes+1))
	if err != nil || int64(len(payload)) > MaxArticleAttachmentBytes {
		return ErrAttachmentContent
	}
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return ErrAttachmentContent
	}
	if len(archive.File) > 10_000 {
		return ErrAttachmentContent
	}
	entries := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		entries[entry.Name] = true
	}
	if !entries["[Content_Types].xml"] || !entries[required] {
		return ErrAttachmentContent
	}
	return nil
}
