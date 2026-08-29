package personaldocuments

import (
	"context"
	"errors"
	"strings"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/apperror"
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

func TestPersonalDocumentPreviewDegradesWithoutConverter(t *testing.T) {
	svc := newTestService(t)
	item, err := svc.Upload(context.Background(), "1001", "课程说明.pdf", FormatPDF, "application/pdf", int64(len("pdf")), strings.NewReader("pdf"))
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
	_, err = svc.Upload(context.Background(), "1001", "a.pdf", FormatPDF, "application/pdf", 4, strings.NewReader("1234"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Upload(context.Background(), "1001", "b.pdf", FormatPDF, "application/pdf", 1, strings.NewReader("5"))
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
