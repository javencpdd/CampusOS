package richtext

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/gin-gonic/gin"
)

type testObjectPort struct {
	items map[string]struct {
		object corestorage.Object
		body   []byte
	}
	next int
}

func TestPDFInvocationIDsRemainPositiveBigints(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newPDFInvocationID()
		if err != nil {
			t.Fatal(err)
		}
		value, err := strconv.ParseInt(id, 10, 64)
		if err != nil || value <= 0 || seen[id] {
			t.Fatalf("invalid or repeated invocation ID %q: %v", id, err)
		}
		seen[id] = true
	}
}

type seekReadCloser struct{ *bytes.Reader }

func (seekReadCloser) Close() error { return nil }

type testPersonalDocumentPDFReader struct {
	owner string
	id    string
	name  string
	body  []byte
}

func (r testPersonalDocumentPDFReader) OpenOwnPDFDocument(_ context.Context, owner, id string) (PersonalDocumentPDF, error) {
	if owner != r.owner || id != r.id {
		return PersonalDocumentPDF{}, ErrAssetNotFound
	}
	now := time.Now().UTC()
	return PersonalDocumentPDF{
		ID:     r.id,
		Name:   r.name,
		Format: "pdf",
		Object: corestorage.ObjectReader{
			Object: corestorage.Object{ID: "document-object", OwnerID: owner, MimeType: "application/pdf", SizeBytes: int64(len(r.body)), SHA256: "document-test", UpdatedAt: now},
			Reader: seekReadCloser{bytes.NewReader(r.body)},
		},
	}, nil
}

func newTestObjectPort() *testObjectPort {
	return &testObjectPort{items: map[string]struct {
		object corestorage.Object
		body   []byte
	}{}}
}

func (p *testObjectPort) Put(_ context.Context, owner string, request corestorage.PutRequest) (corestorage.Object, error) {
	body, err := io.ReadAll(request.Reader)
	if err != nil {
		return corestorage.Object{}, err
	}
	if request.SizeHint > 0 && int64(len(body)) > request.SizeHint {
		return corestorage.Object{}, corestorage.ErrObjectQuota
	}
	p.next++
	now := time.Now().UTC()
	object := corestorage.Object{ID: strconv.Itoa(p.next), OwnerID: owner, Namespace: request.Namespace, Purpose: request.Purpose,
		OriginalName: request.OriginalName, MimeType: request.MimeType, SizeBytes: int64(len(body)), SHA256: "test", Status: corestorage.ObjectStatusReady, Version: 1, CreatedAt: now, UpdatedAt: now}
	p.items[object.ID] = struct {
		object corestorage.Object
		body   []byte
	}{object: object, body: body}
	return object, nil
}

func (p *testObjectPort) Open(_ context.Context, owner, id string) (corestorage.ObjectReader, error) {
	item, ok := p.items[id]
	if !ok || item.object.OwnerID != owner {
		return corestorage.ObjectReader{}, corestorage.ErrObjectNotFound
	}
	return corestorage.ObjectReader{Object: item.object, Reader: io.NopCloser(bytes.NewReader(item.body))}, nil
}
func (p *testObjectPort) Stat(_ context.Context, owner, id string) (corestorage.Object, error) {
	item, ok := p.items[id]
	if !ok || item.object.OwnerID != owner {
		return corestorage.Object{}, corestorage.ErrObjectNotFound
	}
	return item.object, nil
}
func (p *testObjectPort) Delete(_ context.Context, owner, id string, _ int64) error {
	item, ok := p.items[id]
	if !ok || item.object.OwnerID != owner {
		return corestorage.ErrObjectNotFound
	}
	delete(p.items, id)
	return nil
}
func (p *testObjectPort) List(context.Context, string, corestorage.ObjectFilter, corestorage.PageRequest) (corestorage.ObjectPage, error) {
	return corestorage.ObjectPage{}, nil
}

func newAttachmentTestService(t *testing.T) *Service {
	t.Helper()
	svc, _ := newTestService(true)
	svc.SetObjectPort(newTestObjectPort())
	return svc
}

func createAttachmentDraft(t *testing.T, svc *Service) *ArticleResult {
	t.Helper()
	result, err := svc.CreateDraft(context.Background(), "1001", "alice", SaveArticleRequest{
		Title: "附件文章", CategoryID: "1", ContentHTML: "<p>正文</p>",
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	return result
}

func TestArticleAttachmentUploadAndVisibilityLifecycle(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "guide.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	if attachment.Asset.Kind != AssetKindAttachment || attachment.Asset.StorageObjectID == "" {
		t.Fatalf("attachment did not receive an owner asset identity: %#v", attachment)
	}
	if _, err := svc.OpenAttachment(context.Background(), "1002", draft.ThreadID, attachment.ID); !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("draft attachment leaked to another user: %v", err)
	}
	if _, err := svc.Publish(context.Background(), draft.ThreadID, "1001"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	opened, err := svc.OpenAttachment(context.Background(), "1002", draft.ThreadID, attachment.ID)
	if err != nil {
		t.Fatalf("public attachment open: %v", err)
	}
	defer opened.Reader.Close()
	if got, _ := io.ReadAll(opened.Reader); !bytes.Equal(got, pdf) {
		t.Fatalf("attachment content mismatch: %q", got)
	}
	if _, err := svc.AdminOffline(context.Background(), draft.ThreadID, "9001"); err != nil {
		t.Fatalf("offline article: %v", err)
	}
	if _, err := svc.OpenAttachment(context.Background(), "1002", draft.ThreadID, attachment.ID); !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("offlined attachment remained readable: %v", err)
	}
}

func TestArticleAttachmentRejectsUnsafeTypeAndProtectedTrash(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	if _, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "run.exe", "application/octet-stream", 2, bytes.NewReader([]byte("MZ"))); !errors.Is(err, ErrAttachmentType) {
		t.Fatalf("executable must be rejected, got %v", err)
	}
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "safe.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload pdf: %v", err)
	}
	if err := svc.TrashUserAsset(context.Background(), "1001", attachment.AssetID); !errors.Is(err, ErrAssetReferenced) {
		t.Fatalf("referenced asset must not be trashed, got %v", err)
	}
	if err := svc.RemoveAttachment(context.Background(), "1001", draft.ThreadID, attachment.ID); err != nil {
		t.Fatalf("remove attachment: %v", err)
	}
	if err := svc.TrashUserAsset(context.Background(), "1001", attachment.AssetID); err != nil {
		t.Fatalf("trash unreferenced asset: %v", err)
	}
}

func TestAssetLifecycleRecoveryQuarantineAndPurgeAreReferenceSafe(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "lifecycle.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	if err := svc.AdminQuarantineAsset(context.Background(), "9001", attachment.AssetID, "人工复核"); err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if _, err := svc.OpenAttachment(context.Background(), "1001", draft.ThreadID, attachment.ID); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("quarantined attachment remained readable: %v", err)
	}
	if err := svc.AdminRestoreQuarantinedAsset(context.Background(), "9001", attachment.AssetID, "已完成复核"); err != nil {
		t.Fatalf("restore quarantine: %v", err)
	}
	if err := svc.RemoveAttachment(context.Background(), "1001", draft.ThreadID, attachment.ID); err != nil {
		t.Fatalf("remove binding: %v", err)
	}
	if err := svc.TrashUserAsset(context.Background(), "1001", attachment.AssetID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	trashed, err := svc.ListMyUserAssets(context.Background(), "1001", AssetStatusTrashed)
	if err != nil || len(trashed) != 1 || trashed[0].ID != attachment.AssetID {
		t.Fatalf("recoverable list=%#v err=%v", trashed, err)
	}
	preview, err := svc.PreviewAssetPurge(context.Background(), attachment.AssetID)
	if err != nil || !preview.Eligible || preview.ReferenceCount != 0 {
		t.Fatalf("purge preview=%#v err=%v", preview, err)
	}
	if err := svc.AdminPurgeAsset(context.Background(), "9001", attachment.AssetID, "测试清除"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	asset, err := svc.store.GetUserAsset(context.Background(), attachment.AssetID)
	if err != nil || asset.Status != AssetStatusDeleted {
		t.Fatalf("purged asset=%#v err=%v", asset, err)
	}
	if _, err := svc.objects.Stat(context.Background(), "1001", attachment.Asset.StorageObjectID); !errors.Is(err, corestorage.ErrObjectNotFound) {
		t.Fatalf("physical object survived purge: %v", err)
	}
	summary, err := svc.AssetGovernanceSummary(context.Background())
	if err != nil || len(summary.Actions) < 5 {
		t.Fatalf("low-sensitivity lifecycle summary=%#v err=%v", summary, err)
	}
}

func TestArticleAttachmentDisplayNameCanOnlyBeChangedByDraftAuthor(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "source.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	if err := svc.RenameAttachment(context.Background(), "1002", draft.ThreadID, attachment.ID, "公开资料.pdf"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("other user renamed draft attachment: %v", err)
	}
	if err := svc.RenameAttachment(context.Background(), "1001", draft.ThreadID, attachment.ID, "公开资料.pdf"); err != nil {
		t.Fatalf("author rename attachment: %v", err)
	}
	items, err := svc.ListArticleAttachments(context.Background(), "1001", draft.ThreadID)
	if err != nil || len(items) != 1 || items[0].DisplayName != "公开资料.pdf" {
		t.Fatalf("renamed attachment=%#v err=%v", items, err)
	}
}

func TestPDFInvocationIsOwnerBoundAndExpires(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "safe.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload pdf: %v", err)
	}
	if _, err := svc.Publish(context.Background(), draft.ThreadID, "1001"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	invocation, err := svc.CreatePDFInvocation(context.Background(), "1001", draft.ThreadID, attachment.ID, "modal")
	if err != nil {
		t.Fatalf("create invocation: %v", err)
	}
	if _, _, err := svc.OpenPDFInvocation(context.Background(), "1002", invocation.ID); !errors.Is(err, ErrInvocationNotFound) {
		t.Fatalf("invocation crossed owner boundary: %v", err)
	}
	invocation.ExpiresAt = time.Now().UTC().Add(-time.Second)
	if err := svc.store.CreateInvocation(context.Background(), &PluginUIInvocation{ID: "expired", UserID: "1001", PluginKey: PDFViewerPluginKey, SurfaceID: PDFViewerSurfaceID, ContextKind: InvocationContextArticleAttachment, ArticleContentID: attachment.ArticleContentID, AssetID: attachment.AssetID, AttachmentID: attachment.ID, Presentation: "modal", Purpose: "test", ExpiresAt: invocation.ExpiresAt, CreatedAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatalf("seed expired invocation: %v", err)
	}
	if _, _, err := svc.OpenPDFInvocation(context.Background(), "1001", "expired"); !errors.Is(err, ErrInvocationExpired) {
		t.Fatalf("expired invocation opened: %v", err)
	}
}

func TestPublishedArticlePDFCanBePreviewedByAnotherAuthorizedReader(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "public.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload pdf: %v", err)
	}
	if _, err := svc.Publish(context.Background(), draft.ThreadID, "1001"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var calls []PDFViewerAuthorizationInput
	svc.SetPDFViewerAuthorizer(func(_ context.Context, input PDFViewerAuthorizationInput) error {
		calls = append(calls, input)
		return nil
	})
	invocation, err := svc.CreatePDFInvocation(context.Background(), "1002", draft.ThreadID, attachment.ID, "modal")
	if err != nil {
		t.Fatalf("authorized public reader could not create preview: %v", err)
	}
	if invocation.UserID != "1002" || invocation.ContextKind != InvocationContextArticleAttachment {
		t.Fatalf("preview context must remain bound to reader: %#v", invocation)
	}
	opened, _, err := svc.OpenPDFInvocation(context.Background(), "1002", invocation.ID)
	if err != nil {
		t.Fatalf("authorized public reader could not read preview: %v", err)
	}
	defer opened.Reader.Close()
	if len(calls) != 4 || calls[0].CapabilityCode != "article_attachment.self.preview" || calls[0].ResourceOwnerID != "" || calls[2].OperationCode != "content.read" || calls[3].CapabilityCode != "plugin_ui.surface.open" {
		t.Fatalf("unexpected authorization contexts: %#v", calls)
	}
}

func TestPDFPreviewStopsWhenPluginAuthorizationIsRevoked(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "private.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	allowed := true
	svc.SetPDFViewerAuthorizer(func(_ context.Context, _ PDFViewerAuthorizationInput) error {
		if !allowed {
			return &PDFViewerAuthorizationError{Reason: "DENY_USER_CONSENT_MISSING", Message: "你尚未同意该数据用途，或同意已撤销/过期/用途发生变化。"}
		}
		return nil
	})
	invocation, err := svc.CreatePersonalAssetPDFInvocation(context.Background(), "1001", attachment.AssetID, "modal")
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	if _, _, err := svc.OpenPDFInvocation(context.Background(), "1001", invocation.ID); !errors.Is(err, ErrPluginAuthorization) {
		t.Fatalf("revoked authorization must deny the next read: %v", err)
	}
}

func TestPersonalAssetDownloadAndPDFPreviewAreOwnerBound(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "private.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}

	opened, err := svc.OpenMyUserAsset(context.Background(), "1001", attachment.AssetID)
	if err != nil {
		t.Fatalf("owner personal download was denied: %v", err)
	}
	defer opened.Reader.Close()
	if got, _ := io.ReadAll(opened.Reader); !bytes.Equal(got, pdf) {
		t.Fatalf("owner personal download body = %q", got)
	}
	if _, err := svc.OpenMyUserAsset(context.Background(), "1002", attachment.AssetID); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("another user must not enumerate or download private asset: %v", err)
	}

	invocation, err := svc.CreatePersonalAssetPDFInvocation(context.Background(), "1001", attachment.AssetID, "modal")
	if err != nil {
		t.Fatalf("create personal preview invocation: %v", err)
	}
	if invocation.ContextKind != InvocationContextPersonalAsset || invocation.ArticleContentID != "" || invocation.AttachmentID != "" {
		t.Fatalf("personal preview leaked an article context: %#v", invocation)
	}
	if _, _, err := svc.OpenPDFInvocation(context.Background(), "1002", invocation.ID); !errors.Is(err, ErrInvocationNotFound) {
		t.Fatalf("personal preview invocation crossed owner boundary: %v", err)
	}
	preview, _, err := svc.OpenPDFInvocation(context.Background(), "1001", invocation.ID)
	if err != nil {
		t.Fatalf("owner personal preview was denied: %v", err)
	}
	defer preview.Reader.Close()
	if got, _ := io.ReadAll(preview.Reader); !bytes.Equal(got, pdf) {
		t.Fatalf("owner personal preview body = %q", got)
	}
}

func TestPersonalDocumentPDFPreviewUsesTheSameOwnerScopedPluginContext(t *testing.T) {
	svc := newAttachmentTestService(t)
	pdf := []byte("%PDF-1.7\\n")
	svc.SetPersonalDocumentPDFReader(testPersonalDocumentPDFReader{owner: "1001", id: "document-1", name: "我的文档.pdf", body: pdf})
	var calls []PDFViewerAuthorizationInput
	svc.SetPDFViewerAuthorizer(func(_ context.Context, input PDFViewerAuthorizationInput) error {
		calls = append(calls, input)
		return nil
	})

	invocation, err := svc.CreatePersonalDocumentPDFInvocation(context.Background(), "1001", "document-1", "modal")
	if err != nil {
		t.Fatalf("create personal document invocation: %v", err)
	}
	if invocation.ContextKind != InvocationContextPersonalDocument || invocation.PersonalDocumentID != "document-1" || invocation.AssetID != "" || invocation.AttachmentID != "" {
		t.Fatalf("personal document context leaked an unrelated reference: %#v", invocation)
	}
	if _, _, err := svc.OpenPDFInvocation(context.Background(), "1002", invocation.ID); !errors.Is(err, ErrInvocationNotFound) {
		t.Fatalf("personal document invocation crossed owner boundary: %v", err)
	}
	opened, _, err := svc.OpenPDFInvocation(context.Background(), "1001", invocation.ID)
	if err != nil {
		t.Fatalf("owner personal document preview was denied: %v", err)
	}
	defer opened.Reader.Close()
	if got, _ := io.ReadAll(opened.Reader); !bytes.Equal(got, pdf) {
		t.Fatalf("personal document preview body = %q", got)
	}
	if len(calls) != 4 || calls[0].CapabilityCode != "personal_space_file.self.read" || calls[0].ResourceOwnerID != "1001" || calls[2].OperationCode != "content.read" || calls[3].CapabilityCode != "plugin_ui.surface.open" {
		t.Fatalf("unexpected personal document authorization contexts: %#v", calls)
	}
}

func TestAttachmentContentUsesHTTPRangeAndPrivateHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/attachment", nil)
	request.Header.Set("Range", "bytes=4-7")
	context.Request = request
	context.Set("user_id", "1001")
	now := time.Now().UTC()
	handler := NewHandler(nil)
	handler.serveOpenedAttachment(context, AttachmentOpen{
		Attachment: ArticleAttachment{DisplayName: "preview.pdf", Asset: UserAsset{MimeType: "application/pdf"}},
		Object:     corestorage.Object{UpdatedAt: now, SHA256: "abc123"},
		Reader:     seekReadCloser{bytes.NewReader([]byte("%PDF-1.7"))},
	}, true)
	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want %d", recorder.Code, http.StatusPartialContent)
	}
	if got := recorder.Body.String(); got != "-1.7" {
		t.Fatalf("range body = %q, want %q", got, "-1.7")
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff header = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache control = %q", got)
	}
}

func TestAttachmentContentHonorsIfRangeAndRejectsInvalidRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serve := func(rangeValue, ifRange string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodGet, "/attachment", nil)
		request.Header.Set("Range", rangeValue)
		if ifRange != "" {
			request.Header.Set("If-Range", ifRange)
		}
		context.Request = request
		context.Set("user_id", "1001")
		NewHandler(nil).serveOpenedAttachment(context, AttachmentOpen{
			Attachment: ArticleAttachment{DisplayName: "preview.pdf", Asset: UserAsset{MimeType: "application/pdf"}},
			Object:     corestorage.Object{UpdatedAt: time.Now().UTC(), SHA256: "stable-etag"},
			Reader:     seekReadCloser{bytes.NewReader([]byte("%PDF-1.7"))},
		}, true)
		return recorder
	}

	if recorder := serve("bytes=0-3", `"stable-etag"`); recorder.Code != http.StatusPartialContent || recorder.Body.String() != "%PDF" {
		t.Fatalf("matching If-Range must retain partial response, status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder := serve("bytes=0-3", `"stale-etag"`); recorder.Code != http.StatusOK || recorder.Body.String() != "%PDF-1.7" {
		t.Fatalf("stale If-Range must safely fall back to the complete object, status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder := serve("bytes=99-100", ""); recorder.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("invalid range status=%d, want %d", recorder.Code, http.StatusRequestedRangeNotSatisfiable)
	}
}

func TestValidateAttachmentRequiresCompatibleNameClaimAndSignature(t *testing.T) {
	cases := []struct {
		name, claimed string
		body          []byte
		want          string
	}{
		{"brief.pdf", "application/pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"sound.mp3", "audio/mpeg", []byte("ID3\x04\x00"), "audio/mpeg"},
		{"archive.zip", "application/zip", []byte("PK\x03\x04payload"), "application/zip"},
		{"invoice.pdf", "application/pdf", []byte("not a pdf"), ""},
		{"report.docx", "application/pdf", []byte("PK\x03\x04payload"), ""},
		{"program.exe", "application/octet-stream", []byte("MZ"), ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateAttachment(bufio.NewReader(bytes.NewReader(test.body)), test.name, test.claimed)
			if test.want == "" {
				if err == nil {
					t.Fatalf("unsafe or mismatched attachment was accepted as %q", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("mime=%q err=%v, want %q", got, err, test.want)
			}
		})
	}
}

func TestAttachmentUploadChecksOOXMLInternalEntriesAndCleansRejectedObject(t *testing.T) {
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	valid := testOOXMLPayload(t, "word/document.xml")
	if _, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "letter.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", int64(len(valid)), bytes.NewReader(valid)); err != nil {
		t.Fatalf("valid DOCX was rejected: %v", err)
	}
	objects := svc.objects.(*testObjectPort)
	if got := len(objects.items); got != 1 {
		t.Fatalf("valid attachment object count=%d, want 1", got)
	}
	invalid := testOOXMLPayload(t, "payload.txt")
	if _, err := svc.UploadAttachment(context.Background(), "1001", draft.ThreadID, "renamed.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", int64(len(invalid)), bytes.NewReader(invalid)); !errors.Is(err, ErrAttachmentContent) {
		t.Fatalf("renamed generic ZIP must be rejected, got %v", err)
	}
	if got := len(objects.items); got != 1 {
		t.Fatalf("rejected Office container leaked an object, count=%d", got)
	}
}

func testOOXMLPayload(t *testing.T, requiredEntry string) []byte {
	t.Helper()
	var payload bytes.Buffer
	archive := zip.NewWriter(&payload)
	for _, name := range []string{"[Content_Types].xml", requiredEntry} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatalf("create ZIP entry %q: %v", name, err)
		}
		if _, err := entry.Write([]byte("fixture")); err != nil {
			t.Fatalf("write ZIP entry %q: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close ZIP fixture: %v", err)
	}
	return payload.Bytes()
}
