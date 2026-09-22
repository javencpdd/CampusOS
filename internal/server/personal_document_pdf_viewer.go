package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	personaldocuments "github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/pkg/apperror"
)

// personalDocumentPDFReader is a composition-only adapter. It gives the
// RichText-owned invocation gateway one exact owner-scoped PDF stream without
// exposing the Personal Documents repository, object paths or byte source to
// the Plugin Manager or any external plugin runtime.
type personalDocumentPDFReader struct {
	service func() *personaldocuments.Service
}

func (r personalDocumentPDFReader) OpenOwnPDFDocument(ctx context.Context, owner, documentID string) (richtext.PersonalDocumentPDF, error) {
	if r.service == nil || r.service() == nil {
		return richtext.PersonalDocumentPDF{}, richtext.ErrAssetUnavailable
	}
	document, object, err := r.service().OpenCurrent(ctx, owner, documentID)
	if err != nil {
		return richtext.PersonalDocumentPDF{}, err
	}
	return richtext.PersonalDocumentPDF{ID: document.ID, Name: document.Name, Format: document.Format, Object: object}, nil
}

// OpenOwnDocumentForAttachment exposes one active, supported personal
// document as a stream for RichText to copy. It deliberately does not expose
// the underlying repository or object ID, so article attachments remain
// independent snapshots with their own lifecycle and ACL.
func (r personalDocumentPDFReader) OpenOwnDocumentForAttachment(ctx context.Context, owner, documentID string) (richtext.PersonalDocumentAttachment, error) {
	if r.service == nil || r.service() == nil {
		return richtext.PersonalDocumentAttachment{}, richtext.ErrAssetUnavailable
	}
	document, object, err := r.service().OpenCurrent(ctx, owner, documentID)
	if err != nil {
		// Keep the source document owner-scoped and opaque. The caller must not
		// learn whether an arbitrary document ID exists for another user.
		return richtext.PersonalDocumentAttachment{}, richtext.ErrAssetNotFound
	}
	if document.Status != personaldocuments.StatusActive {
		_ = object.Reader.Close()
		return richtext.PersonalDocumentAttachment{}, richtext.ErrAssetNotFound
	}
	if document.Format != personaldocuments.FormatPDF && document.Format != personaldocuments.FormatDOCX {
		_ = object.Reader.Close()
		return richtext.PersonalDocumentAttachment{}, richtext.ErrAttachmentType
	}
	return richtext.PersonalDocumentAttachment{ID: document.ID, Name: document.Name, Format: document.Format, Object: object}, nil
}

func personalDocumentPDFPreviewUnavailable() error {
	return &personaldocuments.PDFPreviewError{
		Status:  http.StatusServiceUnavailable,
		Code:    73001,
		Message: "PDF 预览服务正在启动，请稍后刷新后重试；也可以先下载后使用本地阅读器打开。",
	}
}

func mapPersonalDocumentPDFPreviewError(err error) error {
	if err == nil {
		return nil
	}
	// Personal Documents already turns owner-scoped absence and disabled-module
	// cases into safe public errors. Preserve those descriptors rather than
	// incorrectly reporting a temporary preview-service failure.
	var public *apperror.AppError
	if errors.As(err, &public) {
		return err
	}
	message := "PDF 预览暂不可用，请稍后重试或下载后使用本地阅读器打开。"
	status, code := http.StatusServiceUnavailable, 73001
	switch {
	case errors.Is(err, richtext.ErrAttachmentType):
		status, code, message = http.StatusBadRequest, 73002, "该个人文档不是 PDF，暂不支持在线预览。"
	case errors.Is(err, richtext.ErrInvocationPresentation):
		status, code, message = http.StatusBadRequest, 73002, "预览展示方式无效，请使用弹窗、侧边栏、全屏或新标签页重新打开。"
	case errors.Is(err, richtext.ErrPluginDisabled):
		message = "PDF 预览插件当前未启用。你仍可下载自己的文档后使用本地阅读器打开。"
	case errors.Is(err, richtext.ErrPluginAuthorization):
		status, code, message = http.StatusForbidden, 73003, "PDF 预览尚未获得读取你个人文档的授权。请在插件中心确认授权，或联系管理员授予 PDF Viewer 权限。"
		var authorizationErr *richtext.PDFViewerAuthorizationError
		if errors.As(err, &authorizationErr) && strings.TrimSpace(authorizationErr.Message) != "" {
			message = authorizationErr.Message
		}
	case errors.Is(err, richtext.ErrPermissionDenied), errors.Is(err, richtext.ErrAssetNotFound), errors.Is(err, richtext.ErrAttachmentNotFound):
		status, code, message = http.StatusNotFound, 73004, "个人文档不存在、已删除或当前无权访问。"
	case errors.Is(err, richtext.ErrAssetUnavailable):
		message = "个人文档存储或 PDF 预览服务暂不可用，请稍后重试。"
	}
	return &personaldocuments.PDFPreviewError{Status: status, Code: code, Message: message}
}
