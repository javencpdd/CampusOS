package richtext

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

type countedDocumentReader struct {
	testPersonalDocumentPDFReader
	opens, closes int
}

type countedDocumentStream struct {
	io.ReadCloser
	onClose func()
}

func (r countedDocumentStream) Close() error {
	r.onClose()
	return r.ReadCloser.Close()
}

func (r *countedDocumentReader) OpenOwnPDFDocument(ctx context.Context, owner, id string) (PersonalDocumentPDF, error) {
	document, err := r.testPersonalDocumentPDFReader.OpenOwnPDFDocument(ctx, owner, id)
	if err == nil {
		r.opens++
		document.Object.Reader = countedDocumentStream{ReadCloser: document.Object.Reader, onClose: func() { r.closes++ }}
	}
	return document, err
}

func TestInvocationReturnsCheckedStreamAndClosesOnAuthorizationFailure(t *testing.T) {
	svc := newAttachmentTestService(t)
	reader := &countedDocumentReader{testPersonalDocumentPDFReader: testPersonalDocumentPDFReader{owner: "1001", id: "doc", name: "safe.pdf", body: []byte("%PDF-1.7\n")}}
	svc.SetPersonalDocumentPDFReader(reader)
	v, err := svc.CreatePersonalDocumentPDFInvocation(t.Context(), "1001", "doc", "modal")
	if err != nil {
		t.Fatal(err)
	}
	reader.opens, reader.closes = 0, 0
	opened, _, err := svc.OpenPDFInvocation(t.Context(), "1001", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reader.opens != 1 || reader.closes != 0 {
		t.Fatal("checked stream was reopened or closed before use")
	}
	_ = opened.Reader.Close()
	svc.store.(*MemoryStore).invocations[v.ID].ContextDigest = "stale-version"
	if _, _, err := svc.OpenPDFInvocation(t.Context(), "1001", v.ID); err == nil {
		t.Fatal("stale digest accepted")
	}
	if reader.opens != 2 || reader.closes != 2 {
		t.Fatal("rejected stream leaked")
	}
}

func TestHostDownloadSurvivesPreviewExpiryButRechecksAccess(t *testing.T) {
	ctx := context.Background()
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(ctx, "1001", draft.ThreadID, "safe.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, draft.ThreadID, "1001"); err != nil {
		t.Fatal(err)
	}
	invocation, err := svc.CreatePDFInvocation(ctx, "1002", draft.ThreadID, attachment.ID, "modal")
	if err != nil {
		t.Fatal(err)
	}
	invocation.ExpiresAt = time.Now().Add(-time.Hour)
	svc.store.(*MemoryStore).invocations[invocation.ID].ExpiresAt = invocation.ExpiresAt
	svc.SetPDFViewerEnabledChecker(func() bool { return false })
	if _, _, err := svc.OpenPDFInvocation(ctx, "1002", invocation.ID); err == nil {
		t.Fatal("disabled preview was readable")
	}
	opened, err := svc.DownloadPDFInvocation(ctx, "1002", invocation.ID)
	if err != nil {
		t.Fatalf("authenticated host fallback failed: %v", err)
	}
	opened.Reader.Close()
	for _, user := range []string{"", "1001", "1003"} {
		if opened, err := svc.DownloadPDFInvocation(ctx, user, invocation.ID); err == nil {
			opened.Reader.Close()
			t.Fatalf("invocation leaked to %q", user)
		}
	}
	if _, err := svc.AdminOffline(ctx, draft.ThreadID, "9001"); err != nil {
		t.Fatal(err)
	}
	if opened, err := svc.DownloadPDFInvocation(ctx, "1002", invocation.ID); err == nil {
		opened.Reader.Close()
		t.Fatal("offlined article downloadable")
	}
}

func TestHostDownloadPersonalAssetRemainsOwnerOnly(t *testing.T) {
	ctx := context.Background()
	svc := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, svc)
	pdf := []byte("%PDF-1.7\n")
	attachment, err := svc.UploadAttachment(ctx, "1001", draft.ThreadID, "private.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := svc.CreatePersonalAssetPDFInvocation(ctx, "1001", attachment.AssetID, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := svc.DownloadPDFInvocation(ctx, "1002", invocation.ID); err == nil {
		opened.Reader.Close()
		t.Fatal("foreign asset downloaded")
	}
	if err := svc.AdminQuarantineAsset(ctx, "9001", attachment.AssetID, "测试隔离"); err != nil {
		t.Fatal(err)
	}
	if opened, err := svc.DownloadPDFInvocation(ctx, "1001", invocation.ID); err == nil {
		opened.Reader.Close()
		t.Fatal("quarantined asset downloaded")
	}
}
