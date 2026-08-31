package personaldocuments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
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
	reliable   *reliability.Service
	enabled    func() bool
	now        func() time.Time
	meter      observability.Meter
}

// SetMeter attaches optional aggregate operational telemetry. A nil meter is
// valid for focused tests and isolated, non-server use.
func (s *Service) SetMeter(meter observability.Meter) { s.meter = meter }

// SetReliability makes document-version persistence and the preview request
// one durable command.  The event carries only opaque identifiers and a
// bounded target type; neither document content nor a provider path crosses
// the Outbox boundary.
//
// Standalone unit tests may intentionally omit this port.  The production
// module requires it, so normal deployed writes never fall back to a
// post-commit best-effort enqueue.
func (s *Service) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil || s.repository == nil {
		return
	}
	if snapshotter, ok := s.repository.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

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
	if strings.TrimSpace(req.Name) != "" {
		if err := validateDocumentName(req.Name, format); err != nil {
			return DocumentDetail{}, s.invalidUploadError(format, err.Error())
		}
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
		return DocumentDetail{}, s.tooLargeError(format, limit, size)
	}
	raw, trustedMIME, e := s.validateUploadPayload(name, format, size, reader)
	if e != nil {
		return DocumentDetail{}, e
	}
	// Never trust the multipart Content-Type. The bounded signature/structure
	// validation above supplies the MIME persisted in Object metadata.
	_ = mime
	return s.createObjectAndDocument(ctx, owner, normalizeName(name, format), format, bytes.NewReader(raw), int64(len(raw)), trustedMIME)
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
	if strings.TrimSpace(req.Name) != "" {
		if err := validateDocumentName(req.Name, d.Format); err != nil {
			return DocumentDetail{}, s.invalidUploadError(d.Format, err.Error())
		}
	}
	if e := validateContent(d.Format, req.Content); e != nil {
		return DocumentDetail{}, e
	}
	object, e := s.put(ctx, owner, normalizeName(d.Name, d.Format), d.Format, "text/plain; charset=utf-8", int64(len([]byte(req.Content))), strings.NewReader(req.Content))
	if e != nil {
		return DocumentDetail{}, e
	}
	version := DocumentVersion{ID: newID(), DocumentID: id, SourceObjectID: object.ID, Format: d.Format, SizeBytes: object.SizeBytes, SHA256: object.SHA256, CreatedAt: s.now().UTC()}
	result, e := s.commitVersion(ctx, owner, id, version, func(commandCtx context.Context) (DocumentDetail, error) {
		return s.repository.AppendVersion(commandCtx, owner, id, req.ExpectedVersion, version, strings.TrimSpace(req.Name))
	})
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
	result, e := s.commitVersion(ctx, owner, id, next, func(commandCtx context.Context) (DocumentDetail, error) {
		return s.repository.AppendVersion(commandCtx, owner, id, expected, next, "")
	})
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

// OpenVersion opens the current version when versionID is empty, or an
// immutable historical version when it is supplied. Owner scoping happens
// before resolving either object ID, so cross-user document/version probes are
// indistinguishable from a missing document.
func (s *Service) OpenVersion(ctx context.Context, owner, id, versionID string) (DocumentDetail, corestorage.ObjectReader, error) {
	d, e := s.Get(ctx, owner, id)
	if e != nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, e
	}
	version := d.CurrentVersion
	if strings.TrimSpace(versionID) != "" {
		resolved, versionErr := s.repository.Version(ctx, owner, id, strings.TrimSpace(versionID))
		if versionErr != nil {
			return DocumentDetail{}, corestorage.ObjectReader{}, s.translateError(versionErr)
		}
		version = &resolved
	}
	if version == nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, s.public(ErrNotFound, apperror.PersonalDocumentNotFound, nil)
	}
	o, e := s.objects.Open(ctx, owner, version.SourceObjectID)
	if e != nil {
		return DocumentDetail{}, corestorage.ObjectReader{}, s.objectError(e, limitFor(d.Format), owner, 0)
	}
	return d, o, nil
}

func (s *Service) OpenCurrent(ctx context.Context, owner, id string) (DocumentDetail, corestorage.ObjectReader, error) {
	return s.OpenVersion(ctx, owner, id, "")
}

func (s *Service) TextContentVersion(ctx context.Context, owner, id, versionID string) (DocumentDetail, string, error) {
	d, o, e := s.OpenVersion(ctx, owner, id, versionID)
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

func (s *Service) TextContent(ctx context.Context, owner, id string) (DocumentDetail, string, error) {
	return s.TextContentVersion(ctx, owner, id, "")
}

func (s *Service) PreviewVersion(ctx context.Context, owner, id, versionID string) (PreviewStatus, error) {
	started := time.Now()
	document, e := s.Get(ctx, owner, id)
	if e != nil {
		s.recordPreview(document.Format, "error", started)
		return PreviewStatus{}, e
	}
	if editable(document.Format) {
		_, content, contentErr := s.TextContentVersion(ctx, owner, id, versionID)
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

func (s *Service) Preview(ctx context.Context, owner, id string) (PreviewStatus, error) {
	return s.PreviewVersion(ctx, owner, id, "")
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
	result, e := s.commitVersion(ctx, owner, d.ID, v, func(commandCtx context.Context) (DocumentDetail, error) {
		return s.repository.Create(commandCtx, d, v)
	})
	if e != nil {
		_ = s.objects.Delete(ctx, owner, object.ID, object.Version)
		return DocumentDetail{}, s.translateError(e)
	}
	return result, nil
}

const (
	previewRequestedEvent  = "document.preview.requested.v1"
	previewRequestConsumer = "feature.personal-documents.preview-gate"
)

var ErrInvalidPreviewRequest = errors.New("document preview request is invalid")

// previewRequestPayload is deliberately an explicit allow-list.  In
// particular, it contains no owner ID, name, URL, source bytes, storage key,
// absolute path, JWT, or temporary download capability.
type previewRequestPayload struct {
	DocumentID        string `json:"document_id"`
	DocumentVersionID string `json:"document_version_id"`
	SourceObjectID    string `json:"source_object_id"`
	TargetPreviewType string `json:"target_preview_type"`
}

// AcknowledgePreviewRequest is the v0.14 safe-fallback Outbox consumer.  It
// intentionally does not fetch a document, source Object, or user data, and
// it never invokes a converter in the API process.  The durable command has
// already recorded the bounded PDF/DOCX "unsupported" state before this
// event is visible.  A successful acknowledgement therefore means that a
// deployment without a reviewed isolated Runner has applied its explicit,
// safe fallback rather than accumulating retry/dead-letter noise.
//
// A future Converter Runner must use a different reviewed consumer and fetch
// source content only through a narrowly scoped, authenticated read contract.
func (s *Service) AcknowledgePreviewRequest(_ context.Context, event reliability.Event) error {
	if s == nil || event.Type != previewRequestedEvent || event.AggregateType != "personal_document_version" || strings.TrimSpace(event.AggregateID) == "" {
		return reliability.Permanent(ErrInvalidPreviewRequest)
	}
	var payload previewRequestPayload
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return reliability.Permanent(ErrInvalidPreviewRequest)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF ||
		strings.TrimSpace(payload.DocumentID) == "" ||
		strings.TrimSpace(payload.DocumentVersionID) == "" ||
		strings.TrimSpace(payload.SourceObjectID) == "" ||
		strings.TrimSpace(payload.DocumentVersionID) != strings.TrimSpace(event.AggregateID) ||
		(payload.TargetPreviewType != "native" && payload.TargetPreviewType != "converter") {
		return reliability.Permanent(ErrInvalidPreviewRequest)
	}
	return nil
}

// commitVersion keeps a document row/version and its preview request in one
// database transaction.  A failed outbox enqueue rolls the version change
// back; the caller then deletes the newly created immutable source object as
// normal compensation.  The object itself must remain outside the SQL
// transaction because the Local Provider is a separate durable medium.
func (s *Service) commitVersion(ctx context.Context, owner, documentID string, version DocumentVersion, action func(context.Context) (DocumentDetail, error)) (DocumentDetail, error) {
	if s.reliable == nil {
		return action(ctx)
	}
	if action == nil {
		return DocumentDetail{}, errors.New("document version command action is required")
	}
	var result DocumentDetail
	err := s.reliable.Execute(ctx, reliability.Command{
		Code:           "personal_documents.preview.request",
		ActorID:        owner,
		ActorType:      "user",
		ResourceType:   "personal_document",
		ResourceID:     documentID,
		OperationCode:  "document.preview.request",
		IdempotencyKey: "document.preview.requested:" + version.ID,
		EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent(previewRequestedEvent, "personal_document_version", version.ID, previewRequestPayload{
				DocumentID:        documentID,
				DocumentVersionID: version.ID,
				SourceObjectID:    version.SourceObjectID,
				TargetPreviewType: previewTarget(version.Format),
			})
		},
	}, func(commandCtx context.Context) error {
		var commandErr error
		result, commandErr = action(commandCtx)
		if commandErr != nil {
			return commandErr
		}
		return s.recordPreviewState(commandCtx, version)
	})
	if err != nil {
		return DocumentDetail{}, err
	}
	return result, nil
}

func previewTarget(format string) string {
	if normalizeFormat(format) == FormatPDF || normalizeFormat(format) == FormatDOCX {
		return "converter"
	}
	return "native"
}

// recordPreviewState makes the current safe-degradation state explicit for
// PDF/DOCX. Native text formats do not need a queued conversion record. A
// future isolated Runner can transition this bounded row through pending /
// processing / ready without changing document-version immutability.
func (s *Service) recordPreviewState(ctx context.Context, version DocumentVersion) error {
	if previewTarget(version.Format) != "converter" {
		return nil
	}
	return s.repository.RecordPreview(ctx, PreviewRecord{
		ID:                newID(),
		DocumentVersionID: version.ID,
		Status:            "unsupported",
		ErrorCode:         "converter_unavailable",
		Format:            normalizeFormat(version.Format),
	})
}

func (s *Service) put(ctx context.Context, owner, name, format, mime string, size int64, reader io.Reader) (corestorage.Object, error) {
	if strings.TrimSpace(owner) == "" {
		return corestorage.Object{}, s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{"field": "owner"})
	}
	limit := limitFor(format)
	if size < 0 || size > limit {
		return corestorage.Object{}, s.tooLargeError(format, limit, size)
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
