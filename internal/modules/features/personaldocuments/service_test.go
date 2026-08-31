package personaldocuments

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/observability"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatalf("objects: %v", err)
	}
	service, err := NewService(NewMemoryRepository(), objects)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	return service
}

func newReliableTestService(t *testing.T) (*Service, *reliability.Service) {
	t.Helper()
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatalf("objects: %v", err)
	}
	repo := NewMemoryRepository()
	service, err := NewService(repo, objects)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	durable := reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())
	service.SetReliability(durable)
	return service, durable
}

const safeTestPDF = "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF"

func testDOCX(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for name, content := range entries {
		item, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create DOCX entry: %v", err)
		}
		if _, err = item.Write([]byte(content)); err != nil {
			t.Fatalf("write DOCX entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close DOCX: %v", err)
	}
	return payload.Bytes()
}

func validDOCX(t *testing.T) []byte {
	t.Helper()
	return testDOCX(t, map[string]string{
		"[Content_Types].xml": `<Types><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml" /></Types>`,
		"word/document.xml":   `<document><body><p>CampusOS</p></body></document>`,
	})
}

func TestTextDocumentVersionsArePrivateAndImmutable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	document, err := svc.CreateText(ctx, "1001", CreateRequest{Name: "计划.txt", Format: FormatText, Content: "第一版"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if document.CurrentVersion == nil || document.CurrentVersion.VersionNumber != 1 {
		t.Fatalf("unexpected first version: %#v", document)
	}
	updated, err := svc.Save(ctx, "1001", document.ID, SaveRequest{ExpectedVersion: document.Version, Content: "第二版"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	versions, err := svc.Versions(ctx, "1001", document.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions: %#v, %v", versions, err)
	}
	if versions[0].VersionNumber != 2 || versions[1].VersionNumber != 1 {
		t.Fatalf("version ordering: %#v", versions)
	}
	restored, err := svc.RestoreVersion(ctx, "1001", document.ID, versions[1].ID, updated.Version)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	_, content, err := svc.TextContent(ctx, "1001", document.ID)
	if err != nil || content != "第一版" {
		t.Fatalf("restored content=%q err=%v", content, err)
	}
	if restored.CurrentVersion == nil || restored.CurrentVersion.VersionNumber != 3 {
		t.Fatalf("restore must create a third immutable version: %#v", restored)
	}
	_, err = svc.Get(ctx, "1002", document.ID)
	var public *apperror.AppError
	if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentNotFound {
		t.Fatalf("cross-user access must be public 404, got %v", err)
	}
}

func TestDocumentVersionCommitEnqueuesMinimalDurablePreviewRequest(t *testing.T) {
	svc, durable := newReliableTestService(t)
	ctx := context.Background()
	document, err := svc.CreateText(ctx, "1001", CreateRequest{Name: "隐私计划.md", Format: FormatMarkdown, Content: "# 第一版"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Save(ctx, "1001", document.ID, SaveRequest{ExpectedVersion: document.Version, Content: "# 第二版"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	events, err := durable.List(ctx, reliability.EventFilter{Type: previewRequestedEvent, Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("preview events = %#v, %v", events, err)
	}
	for _, event := range events {
		if event.AggregateType != "personal_document_version" || event.AggregateID == "" {
			t.Fatalf("event aggregate = %#v", event)
		}
		var payload previewRequestPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.DocumentID != document.ID || payload.DocumentVersionID == "" || payload.SourceObjectID == "" || payload.TargetPreviewType != "native" {
			t.Fatalf("unsafe or incomplete payload: %#v", payload)
		}
		if strings.Contains(string(event.Payload), "1001") || strings.Contains(string(event.Payload), "隐私计划") || strings.Contains(string(event.Payload), "第一版") || strings.Contains(string(event.Payload), "第二版") {
			t.Fatalf("outbox payload leaked private owner/name/content: %s", event.Payload)
		}
	}
	if _, err := svc.Save(ctx, "1001", document.ID, SaveRequest{ExpectedVersion: document.Version, Content: "冲突版本"}); err == nil {
		t.Fatal("stale save must fail")
	}
	events, err = durable.List(ctx, reliability.EventFilter{Type: previewRequestedEvent, Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("failed command must not enqueue a preview request: %#v, %v", events, err)
	}
	if updated.CurrentVersion == nil || updated.CurrentVersion.ID == "" {
		t.Fatalf("updated version was not persisted: %#v", updated)
	}
}

func TestBinaryDocumentPreviewRequestTargetsIsolatedConverter(t *testing.T) {
	svc, durable := newReliableTestService(t)
	item, err := svc.Upload(context.Background(), "1001", "课程说明.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
	if err != nil {
		t.Fatalf("upload PDF: %v", err)
	}
	events, err := durable.List(context.Background(), reliability.EventFilter{Type: previewRequestedEvent, Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("converter preview event = %#v, %v", events, err)
	}
	var payload previewRequestPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DocumentID != item.ID || payload.TargetPreviewType != "converter" {
		t.Fatalf("binary preview request = %#v", payload)
	}
	summary, err := svc.repository.(interface {
		PreviewSummary(context.Context) (map[PreviewMetricKey]int64, error)
	}).PreviewSummary(context.Background())
	if err != nil || summary[PreviewMetricKey{Status: "unsupported", Format: FormatPDF}] != 1 {
		t.Fatalf("safe converter-degradation state = %#v, %v", summary, err)
	}
}

func TestPreviewRequestConsumerAcknowledgesSafeFallback(t *testing.T) {
	svc, durable := newReliableTestService(t)
	durable.RegisterConsumer(previewRequestedEvent, previewRequestConsumer, svc.AcknowledgePreviewRequest)
	ctx := context.Background()
	if _, err := svc.Upload(ctx, "1001", "课程说明.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF)); err != nil {
		t.Fatalf("upload PDF: %v", err)
	}
	events, err := durable.List(ctx, reliability.EventFilter{Type: previewRequestedEvent, Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("preview events = %#v, %v", events, err)
	}
	if processed, err := durable.ProcessOnce(ctx); err != nil || processed != 1 {
		t.Fatalf("process safe fallback: processed=%d err=%v", processed, err)
	}
	events, err = durable.List(ctx, reliability.EventFilter{Type: previewRequestedEvent, Limit: 10})
	if err != nil || len(events) != 1 || events[0].Status != reliability.StatusPublished {
		t.Fatalf("safe fallback event = %#v, %v", events, err)
	}
	acknowledged, err := durable.Store().HasConsumerReceipt(ctx, previewRequestConsumer, events[0].ID)
	if err != nil || !acknowledged {
		t.Fatalf("safe fallback receipt = %t, %v", acknowledged, err)
	}
}

func TestPreviewRequestConsumerRejectsMalformedPayload(t *testing.T) {
	svc := newTestService(t)
	event, err := reliability.NewEvent(previewRequestedEvent, "personal_document_version", "version-1", map[string]string{
		"document_id": "document-1", "document_version_id": "version-1", "source_object_id": "object-1", "target_preview_type": "converter", "owner_id": "must-not-be-accepted",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.AcknowledgePreviewRequest(context.Background(), event)
	var deliveryErr *reliability.DeliveryError
	if !errors.As(err, &deliveryErr) || deliveryErr.Retryable || !errors.Is(err, ErrInvalidPreviewRequest) {
		t.Fatalf("malformed preview request error = %v", err)
	}
}

func TestHistoricalVersionCanBeReadWithoutChangingCurrentDocument(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	document, err := svc.CreateText(ctx, "1001", CreateRequest{Name: "历史.txt", Format: FormatText, Content: "第一版"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Save(ctx, "1001", document.ID, SaveRequest{ExpectedVersion: document.Version, Content: "第二版"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	versions, err := svc.Versions(ctx, "1001", document.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions: %#v, %v", versions, err)
	}
	var first DocumentVersion
	for _, version := range versions {
		if version.VersionNumber == 1 {
			first = version
		}
	}
	_, content, err := svc.TextContentVersion(ctx, "1001", document.ID, first.ID)
	if err != nil || content != "第一版" {
		t.Fatalf("historical content=%q err=%v", content, err)
	}
	current, err := svc.Get(ctx, "1001", document.ID)
	if err != nil || current.Version != updated.Version || current.CurrentVersion == nil || current.CurrentVersion.VersionNumber != 2 {
		t.Fatalf("historical read must not change current version: %#v err=%v", current, err)
	}
}

func TestPersonalDocumentPreviewDegradesWithoutConverter(t *testing.T) {
	svc := newTestService(t)
	item, err := svc.Upload(context.Background(), "1001", "课程说明.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
	if err != nil {
		t.Fatalf("upload PDF: %v", err)
	}
	preview, err := svc.Preview(context.Background(), "1001", item.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Status != "converter_unavailable" || !preview.DownloadAvailable {
		t.Fatalf("unexpected safe degradation: %#v", preview)
	}
}

func TestEditablePreviewUsesSharedSafeRenderer(t *testing.T) {
	svc := newTestService(t)
	item, err := svc.CreateText(context.Background(), "1001", CreateRequest{Name: "安全说明.md", Format: FormatMarkdown, Content: "# 标题\n\n<script>alert(1)</script>"})
	if err != nil {
		t.Fatalf("create markdown: %v", err)
	}
	preview, err := svc.Preview(context.Background(), "1001", item.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Status != "native" || !strings.Contains(preview.RenderedHTML, "<h1>标题</h1>") {
		t.Fatalf("unexpected native preview: %#v", preview)
	}
	if strings.Contains(preview.RenderedHTML, "<script>") {
		t.Fatalf("raw markdown html must never execute: %s", preview.RenderedHTML)
	}
}

func TestCampusDocV1RejectsExternalImageReference(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreateText(context.Background(), "1001", CreateRequest{Name: "危险.campusdoc", Format: FormatCampusDoc, Content: `{"version":1,"blocks":[{"type":"image","object_id":"https://example.invalid/image.png"}]}`})
	var public *apperror.AppError
	if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentInvalid {
		t.Fatalf("expected public CampusDoc validation error, got %v", err)
	}
}

func TestReadPortKeepsOwnerScope(t *testing.T) {
	svc := newTestService(t)
	item, err := svc.CreateText(context.Background(), "1001", CreateRequest{Name: "私有.txt", Format: FormatText, Content: "private"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.ReadOnly().GetOwnDocument(context.Background(), "1002", item.ID)
	var public *apperror.AppError
	if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentNotFound {
		t.Fatalf("read port must preserve owner scope, got %v", err)
	}
}

func TestQuotaErrorIncludesRemainingPersonalSpace(t *testing.T) {
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 4)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(NewMemoryRepository(), objects)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateText(context.Background(), "1001", CreateRequest{Name: "a.txt", Format: FormatText, Content: "1234"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateText(context.Background(), "1001", CreateRequest{Name: "b.txt", Format: FormatText, Content: "5"})
	var public *apperror.AppError
	if !errors.As(err, &public) || public.Descriptor() != apperror.UserStorageQuotaExceeded {
		t.Fatalf("expected public quota error, got %v", err)
	}
	details, ok := public.Details().(map[string]any)
	if !ok {
		t.Fatalf("quota details = %#v", public.Details())
	}
	if got, ok := details["remaining_quota_bytes"].(int64); !ok || got != 0 {
		t.Fatalf("remaining quota detail = %#v", details)
	}
}

func TestUploadRejectsDisguisedAndActiveBinaryDocuments(t *testing.T) {
	svc := newTestService(t)
	for name, payload := range map[string]string{
		"伪造.pdf": "plain text",
		"危险.pdf": "%PDF-1.7\n/JavaScript (alert(1))\n%%EOF",
	} {
		_, err := svc.Upload(context.Background(), "1001", name, FormatPDF, "text/plain", int64(len(payload)), strings.NewReader(payload))
		var public *apperror.AppError
		if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentInvalid {
			t.Fatalf("%s should be rejected with safe document error, got %v", name, err)
		}
	}
}

func TestUploadValidatesDOCXStructureAndTrustedMIME(t *testing.T) {
	svc := newTestService(t)
	payload := validDOCX(t)
	item, err := svc.Upload(context.Background(), "1001", "课程.docx", FormatDOCX, "text/plain", int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("valid DOCX upload: %v", err)
	}
	_, object, err := svc.OpenCurrent(context.Background(), "1001", item.ID)
	if err != nil {
		t.Fatalf("open DOCX: %v", err)
	}
	defer object.Reader.Close()
	if object.Object.MimeType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("trusted MIME = %q", object.Object.MimeType)
	}
	external := testDOCX(t, map[string]string{
		"[Content_Types].xml":          `<Types />`,
		"word/document.xml":            `<document />`,
		"word/_rels/document.xml.rels": `<Relationships><Relationship TargetMode="External" Target="https://example.invalid/template" /></Relationships>`,
	})
	_, err = svc.Upload(context.Background(), "1001", "外链.docx", FormatDOCX, "application/zip", int64(len(external)), bytes.NewReader(external))
	var public *apperror.AppError
	if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentInvalid {
		t.Fatalf("external DOCX should be rejected, got %v", err)
	}
}

func TestUploadRejectsUnsafeDocumentNames(t *testing.T) {
	svc := newTestService(t)
	for _, name := range []string{"report.pdf.exe", "CON.pdf", "目录/报告.pdf"} {
		_, err := svc.Upload(context.Background(), "1001", name, FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
		var public *apperror.AppError
		if !errors.As(err, &public) || public.Descriptor() != apperror.PersonalDocumentInvalid {
			t.Fatalf("unsafe name %q should be rejected, got %v", name, err)
		}
	}
}

func TestPreviewMetricsUseBoundedSafeLabels(t *testing.T) {
	svc := newTestService(t)
	collector := observability.NewCollector()
	svc.SetMeter(collector)
	item, err := svc.CreateText(context.Background(), "1001", CreateRequest{Name: "机密计划.md", Format: FormatMarkdown, Content: "# 计划"})
	if err != nil {
		t.Fatalf("create markdown: %v", err)
	}
	if _, err := svc.Preview(context.Background(), "1001", item.ID); err != nil {
		t.Fatalf("preview: %v", err)
	}
	metrics := collector.PrometheusText()
	if !strings.Contains(metrics, `campusos_document_preview_duration_seconds_count{format="markdown",result="native"} 1`) {
		t.Fatalf("native preview metric missing: %s", metrics)
	}
	if strings.Contains(metrics, "机密计划") || strings.Contains(metrics, "1001") {
		t.Fatalf("preview metrics must not expose document or owner data: %s", metrics)
	}
}
