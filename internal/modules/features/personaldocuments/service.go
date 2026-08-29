package personaldocuments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/campusos/CampusOS/pkg/observability"
)

const (
	maxTextBytes   = int64(5 * 1024 * 1024)
	maxBinaryBytes = int64(25 * 1024 * 1024)
)

type Service struct {
	repository Repository
	objects    corestorage.ObjectPort
	enabled    func() bool
	now        func() time.Time
	meter      observability.Meter
}

// SetMeter attaches optional aggregate operational telemetry. A nil meter is
// valid for focused tests and isolated, non-server use.
func (s *Service) SetMeter(meter observability.Meter) { s.meter = meter }

func NewService(repo Repository, objects corestorage.ObjectPort) (*Service, error) {
	if repo == nil || objects == nil {
		return nil, errors.New("personal documents dependencies are required")
	}
	return &Service{repository: repo, objects: objects, enabled: func() bool { return true }, now: time.Now}, nil
}
func (s *Service) SetEnabledChecker(checker func() bool) {
	if checker == nil {
		s.enabled = func() bool { return true }
		return
	}
	s.enabled = checker
}
func (s *Service) List(ctx context.Context, owner, status string) ([]DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return nil, e
	}
	if status != "" && status != StatusActive && status != StatusTrashed {
		return nil, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "status"})
	}
	return s.repository.List(ctx, owner, ListFilter{Status: status})
}
func (s *Service) Get(ctx context.Context, owner, id string) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	value, e := s.repository.Get(ctx, owner, id)
	return value, s.translateError(e)
}
func (s *Service) CreateText(ctx context.Context, owner string, req CreateRequest) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	format := normalizeFormat(req.Format)
	if !editable(format) {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "format", "allowed": []string{FormatText, FormatMarkdown, FormatCampusDoc}})
	}
	if e := validateContent(format, req.Content); e != nil {
		return DocumentDetail{}, e
	}
	return s.createObjectAndDocument(ctx, owner, normalizeName(req.Name, format), format, strings.NewReader(req.Content), int64(len([]byte(req.Content))), "")
}
func (s *Service) Upload(ctx context.Context, owner, name, format, mime string, size int64, reader io.Reader) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	if reader == nil {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "file"})
	}
	format = normalizeFormat(format)
	if !supported(format) {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "format"})
	}
	limit := limitFor(format)
	if size < 0 || size > limit {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentTooLarge, map[string]any{"max_file_bytes": limit, "provided_bytes": size, "format": format})
	}
	if editable(format) {
		raw, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
		if readErr != nil {
			return DocumentDetail{}, readErr
		}
		if int64(len(raw)) > limit {
			return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentTooLarge, map[string]any{"max_file_bytes": limit, "provided_bytes": len(raw), "format": format})
		}
		if contentErr := validateContent(format, string(raw)); contentErr != nil {
			return DocumentDetail{}, contentErr
		}
		size = int64(len(raw))
		reader = strings.NewReader(string(raw))
	}
	return s.createObjectAndDocument(ctx, owner, normalizeName(name, format), format, reader, size, mime)
}
func (s *Service) Save(ctx context.Context, owner, id string, req SaveRequest) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	d, e := s.repository.Get(ctx, owner, id)
	if e != nil {
		return DocumentDetail{}, s.translateError(e)
	}
	if !editable(d.Format) {
		return DocumentDetail{}, s.public(ErrNotEditable, apperror.PersonalDocumentNotEditable, map[string]any{"format": d.Format})
	}
	if req.ExpectedVersion < 1 {
		return DocumentDetail{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "expected_version"})
	}
	if e := validateContent(d.Format, req.Content); e != nil {
		return DocumentDetail{}, e
	}
	object, e := s.put(ctx, owner, normalizeName(d.Name, d.Format), d.Format, "text/plain; charset=utf-8", int64(len([]byte(req.Content))), strings.NewReader(req.Content))
	if e != nil {
		return DocumentDetail{}, e
	}
	version := DocumentVersion{ID: newID(), DocumentID: id, SourceObjectID: object.ID, Format: d.Format, SizeBytes: object.SizeBytes, SHA256: object.SHA256, CreatedAt: s.now().UTC()}
	result, e := s.repository.AppendVersion(ctx, owner, id, req.ExpectedVersion, version, strings.TrimSpace(req.Name))
	if e != nil {
		_ = s.objects.Delete(ctx, owner, object.ID, object.Version)
		return DocumentDetail{}, s.translateError(e)
	}
	return result, nil
}
func (s *Service) Versions(ctx context.Context, owner, id string) ([]DocumentVersion, error) {
	if e := s.enabledError(); e != nil {
		return nil, e
	}
	v, e := s.repository.Versions(ctx, owner, id)
	if e != nil {
		return nil, s.translateError(e)
	}
	return v, nil
}
func (s *Service) RestoreVersion(ctx context.Context, owner, id, versionID string, expected int64) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	d, e := s.repository.Get(ctx, owner, id)
	if e != nil {
		return DocumentDetail{}, s.translateError(e)
	}
	if !editable(d.Format) {
		return DocumentDetail{}, s.public(ErrNotEditable, apperror.PersonalDocumentNotEditable, map[string]any{"format": d.Format})
	}
	v, e := s.repository.Version(ctx, owner, id, versionID)
	if e != nil {
		return DocumentDetail{}, s.translateError(e)
	}
	source, e := s.objects.Open(ctx, owner, v.SourceObjectID)
	if e != nil {
		return DocumentDetail{}, s.objectError(e, limitFor(d.Format), owner, v.SizeBytes)
	}
	defer source.Reader.Close()
	object, e := s.put(ctx, owner, normalizeName(d.Name, d.Format), d.Format, "text/plain; charset=utf-8", v.SizeBytes, source.Reader)
	if e != nil {
		return DocumentDetail{}, e
	}
	next := DocumentVersion{ID: newID(), DocumentID: id, SourceObjectID: object.ID, Format: d.Format, SizeBytes: object.SizeBytes, SHA256: object.SHA256, RestoredFromVersionID: v.ID, CreatedAt: s.now().UTC()}
	result, e := s.repository.AppendVersion(ctx, owner, id, expected, next, "")
	if e != nil {
		_ = s.objects.Delete(ctx, owner, object.ID, object.Version)
		return DocumentDetail{}, s.translateError(e)
	}
	return result, nil
}
func (s *Service) SetStatus(ctx context.Context, owner, id string, expected int64, status string) (DocumentDetail, error) {
	if e := s.enabledError(); e != nil {
		return DocumentDetail{}, e
	}
	x, e := s.repository.SetStatus(ctx, owner, id, expected, status)
	if e != nil {
		return DocumentDetail{}, s.translateError(e)
	}
	return x, nil
}
func (s *Service) OpenCurrent(ctx context.Context, owner, id string) (DocumentDetail, corestorage.ObjectReader, error) {
	d, e := s.Get(ctx, owner, id)
	if e != nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, e
	}
	if d.CurrentVersion == nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, s.public(ErrNotFound, apperror.PersonalDocumentNotFound, nil)
	}
	o, e := s.objects.Open(ctx, owner, d.CurrentVersion.SourceObjectID)
	if e != nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, s.objectError(e, limitFor(d.Format), owner, 0)
	}
	return d, o, nil
}
func (s *Service) TextContent(ctx context.Context, owner, id string) (DocumentDetail, string, error) {
	d, o, e := s.OpenCurrent(ctx, owner, id)
	if e != nil {
		return DocumentDetail{}, "", e
	}
	defer o.Reader.Close()
	if !editable(d.Format) {
		return DocumentDetail{}, "", s.public(ErrNotEditable, apperror.PersonalDocumentNotEditable, map[string]any{"format": d.Format})
	}
	raw, e := io.ReadAll(io.LimitReader(o.Reader, maxTextBytes+1))
	if e != nil {
		return DocumentDetail{}, "", e
	}
	if int64(len(raw)) > maxTextBytes {
		return DocumentDetail{}, "", s.public(ErrInvalid, apperror.PersonalDocumentTooLarge, map[string]any{"max_file_bytes": maxTextBytes})
	}
	return d, string(raw), nil
}
func (s *Service) Preview(ctx context.Context, owner, id string) (PreviewStatus, error) {
	started := time.Now()
	document, e := s.Get(ctx, owner, id)
	if e != nil {
		s.recordPreview(document.Format, "error", started)
		return PreviewStatus{}, e
	}
	if editable(document.Format) {
		_, content, contentErr := s.TextContent(ctx, owner, id)
		if contentErr != nil {
			s.recordPreview(document.Format, "error", started)
			return PreviewStatus{}, contentErr
		}
		rendered, renderErr := coreeditor.RenderDocument(document.Format, content)
		if renderErr != nil {
			s.recordPreview(document.Format, "invalid", started)
			return PreviewStatus{}, s.public(renderErr, apperror.PersonalDocumentInvalid, map[string]any{"field": "content", "reason": "文档内容不符合安全渲染规则"})
		}
		s.recordPreview(document.Format, "native", started)
		_ = s.RefreshPreviewMetrics(ctx)
		return PreviewStatus{Document: document, Status: "native", DownloadAvailable: true, Message: "此格式可在“我的文档”中安全编辑和查看。", RenderedHTML: rendered.HTML, Warnings: rendered.Warnings}, nil
	}
	s.recordPreview(document.Format, "converter_unavailable", started)
	_ = s.RefreshPreviewMetrics(ctx)
	return PreviewStatus{Document: document, Status: "converter_unavailable", DownloadAvailable: true, Message: "当前部署未启用满足隔离要求的文档转换服务；原文件已安全保存，可下载后查看。"}, nil
}

type previewSummaryReader interface {
	PreviewSummary(context.Context) (map[PreviewMetricKey]int64, error)
}

// RefreshPreviewMetrics exports bounded aggregate converter-job states. It is
// intentionally a best-effort operation: document editing must not depend on
// observability or on a future converter worker.
func (s *Service) RefreshPreviewMetrics(ctx context.Context) error {
	if s == nil || s.meter == nil || s.repository == nil {
		return nil
	}
	reader, ok := s.repository.(previewSummaryReader)
	if !ok {
		return nil
	}
	summary, err := reader.PreviewSummary(ctx)
	if err != nil {
		return err
	}
	for _, status := range []string{"pending", "processing", "ready", "failed", "unsupported"} {
		for _, format := range []string{FormatText, FormatMarkdown, FormatCampusDoc, FormatPDF, FormatDOCX, "unknown"} {
			_ = s.meter.SetGauge("campusos_document_preview_jobs", observability.Labels{"status": status, "format": format}, float64(summary[PreviewMetricKey{Status: status, Format: format}]))
		}
	}
	return nil
}

func (s *Service) recordPreview(format, result string, started time.Time) {
	if s == nil || s.meter == nil {
		return
	}
	if !supported(format) {
		format = "unknown"
	}
	if result != "native" && result != "converter_unavailable" && result != "invalid" && result != "error" {
		result = "error"
	}
	_ = s.meter.Observe("campusos_document_preview_duration_seconds", observability.Labels{"format": format, "result": result}, time.Since(started).Seconds())
}
func (s *Service) createObjectAndDocument(ctx context.Context, owner, name, format string, reader io.Reader, size int64, mime string) (DocumentDetail, error) {
	object, e := s.put(ctx, owner, name, format, mime, size, reader)
	if e != nil {
		return DocumentDetail{}, e
	}
	now := s.now().UTC()
	d := Document{ID: newID(), OwnerID: owner, Name: name, Format: format, Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	v := DocumentVersion{ID: newID(), DocumentID: d.ID, VersionNumber: 1, SourceObjectID: object.ID, Format: format, SizeBytes: object.SizeBytes, SHA256: object.SHA256, CreatedAt: now}
	result, e := s.repository.Create(ctx, d, v)
	if e != nil {
		_ = s.objects.Delete(ctx, owner, object.ID, object.Version)
		return DocumentDetail{}, s.translateError(e)
	}
	return result, nil
}
func (s *Service) put(ctx context.Context, owner, name, format, mime string, size int64, reader io.Reader) (corestorage.Object, error) {
	if strings.TrimSpace(owner) == "" {
		return corestorage.Object{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "owner"})
	}
	limit := limitFor(format)
	if size < 0 || size > limit {
		return corestorage.Object{}, s.public(ErrInvalid, apperror.PersonalDocumentTooLarge, map[string]any{"max_file_bytes": limit, "provided_bytes": size, "format": format})
	}
	object, e := s.objects.Put(ctx, owner, corestorage.PutRequest{Namespace: "personal-documents", Purpose: "source." + format, OriginalName: name, MimeType: normalizeMime(mime, format), SizeHint: size, Reader: io.LimitReader(reader, limit+1)})
	if e != nil {
		return corestorage.Object{}, s.objectError(e, limit, owner, size)
	}
	return object, nil
}
func (s *Service) enabledError() error {
	if s.enabled != nil && !s.enabled() {
		return s.public(ErrInvalid, apperror.PersonalDocumentDisabled, nil)
	}
	return nil
}
func (s *Service) objectError(e error, limit int64, owner string, provided int64) error {
	if errors.Is(e, corestorage.ErrObjectQuota) {
		details := map[string]any{"max_file_bytes": limit, "provided_bytes": provided, "message": "个人空间不足，请删除不需要的文件或联系管理员调整配额"}
		if usage, usageErr := objectUsage(s.objects, owner); usageErr == nil {
			details["used_bytes"] = usage.UsedBytes
			details["quota_bytes"] = usage.QuotaBytes
			details["remaining_quota_bytes"] = usage.RemainingBytes
		}
		return s.public(e, apperror.UserStorageQuotaExceeded, details)
	}
	if errors.Is(e, corestorage.ErrObjectNotFound) {
		return s.public(e, apperror.PersonalDocumentNotFound, nil)
	}
	return e
}

type objectUsageReader interface {
	Usage(context.Context, string) (corestorage.ObjectUsage, error)
}

func objectUsage(objects corestorage.ObjectPort, owner string) (corestorage.ObjectUsage, error) {
	reader, ok := objects.(objectUsageReader)
	if !ok {
		return corestorage.ObjectUsage{}, errors.New("object usage is unavailable")
	}
	return reader.Usage(context.Background(), owner)
}
func (s *Service) translateError(e error) error {
	if e == nil {
		return nil
	}
	switch {
	case errors.Is(e, ErrNotFound):
		return s.public(e, apperror.PersonalDocumentNotFound, nil)
	case errors.Is(e, ErrConflict):
		return s.public(e, apperror.PersonalDocumentVersionConflict, map[string]any{"action": "请刷新文档后重试"})
	case errors.Is(e, ErrNotEditable):
		return s.public(e, apperror.PersonalDocumentNotEditable, nil)
	default:
		return e
	}
}
func (s *Service) public(cause error, d apperror.Descriptor, details map[string]any) error {
	return apperror.Wrap(cause, d, details)
}
func newID() string                   { return fmt.Sprintf("%d", idgen.New()) }
func normalizeFormat(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
func supported(v string) bool         { return editable(v) || v == FormatPDF || v == FormatDOCX }
func editable(v string) bool          { return v == FormatText || v == FormatMarkdown || v == FormatCampusDoc }
func limitFor(v string) int64 {
	if editable(v) {
		return maxTextBytes
	}
	return maxBinaryBytes
}
func normalizeName(name, format string) string {
	n := strings.TrimSpace(filepath.Base(name))
	n = strings.Map(func(r rune) rune {
		if r < ' ' || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, n)
	if n == "" {
		n = "未命名文档." + format
	}
	if len([]rune(n)) > 255 {
		n = string([]rune(n)[:255])
	}
	return n
}
func normalizeMime(mime, format string) string {
	if strings.TrimSpace(mime) != "" {
		return strings.Split(mime, ";")[0]
	}
	switch format {
	case FormatPDF:
		return "application/pdf"
	case FormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "text/plain"
	}
}
func validateContent(format, content string) error {
	if int64(len([]byte(content))) > maxTextBytes {
		return apperror.New(apperror.PersonalDocumentTooLarge, map[string]any{"max_file_bytes": maxTextBytes, "provided_bytes": len([]byte(content))})
	}
	if e := coreeditor.ValidateDocument(format, content); e != nil {
		reason := "文档内容不符合安全渲染规则"
		if errors.Is(e, coreeditor.ErrInvalidCampusDoc) {
			reason = "CampusDoc 必须是非空 JSON 对象，v1 文档需包含 version=1 和 blocks"
		}
		return apperror.New(apperror.PersonalDocumentInvalid, map[string]any{"field": "content", "reason": reason})
	}
	return nil
}
