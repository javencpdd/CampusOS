package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func TestMarketServiceScopesRecordsAndRequiresGrant(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	service := newPublishedMarketService(t, manifest, storage)
	if _, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{Data: map[string]interface{}{"title": "private"}}); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("ungranted record write = %v, want denied", err)
	}
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	record, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{RecordKey: "first", Data: map[string]interface{}{"title": "private"}})
	if err != nil {
		t.Fatal(err)
	}
	if record.OwnerID != "user-a" || record.Version != 1 {
		t.Fatalf("unexpected record: %#v", record)
	}
	page, err := service.SearchMyRecords(ctx, manifest.Name, "user-a", "notes", "private", 1, 20)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].RecordKey != "first" {
		t.Fatalf("declared search = %#v, %v", page, err)
	}
	if _, err := service.GetUserRecord(ctx, manifest.Name, "user-b", "notes", "first"); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("cross-user read = %v, want denied", err)
	}
	updated, err := service.UpdateUserRecord(ctx, manifest.Name, "user-a", "notes", "first", RecordInput{Version: record.Version, Data: map[string]interface{}{"title": "edited"}})
	if err != nil || updated.Version != 2 {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	if err := service.DeleteUserRecord(ctx, manifest.Name, "user-a", "notes", "first", 1); !errors.Is(err, ErrMarketVersionMismatch) {
		t.Fatalf("stale delete = %v, want version mismatch", err)
	}
}

func TestMarketServiceStoresFilesUnderUserPluginNamespace(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	service := newPublishedMarketService(t, manifest, storage)
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	file, err := service.UploadUserFile(ctx, manifest.Name, "user-a", "note.txt", "text/plain", []byte("campusos"))
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := service.UserFilePath(ctx, manifest.Name, "user-a", ""+stringID(file.ID))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/plugins/" + manifest.Name + "/"; !containsPath(path, want) {
		t.Fatalf("file path %q does not contain %q", path, want)
	}
	if err := service.DeleteUserFile(ctx, manifest.Name, "user-a", stringID(file.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestMarketServiceRejectsManuallyVerifiedRelease(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	if _, err := service.SaveRelease(context.Background(), PluginRelease{
		PluginName: manifest.Name, Version: "1.0.0", Checksum: "sha256:test", SignatureState: "verified",
	}, "admin"); !errors.Is(err, ErrMarketInvalidInput) {
		t.Fatalf("manual verified release = %v, want invalid input", err)
	}
	if _, err := service.SaveRelease(context.Background(), PluginRelease{
		PluginName: manifest.Name, Version: "1.0.0", Checksum: "sha256:test", SignatureState: "unsigned",
	}, "admin"); err != nil {
		t.Fatalf("unsigned release = %v", err)
	}
	if _, err := service.SaveRelease(context.Background(), PluginRelease{
		PluginName: "unknown-v2", Version: "1.0.0", Checksum: "sha256:test", SignatureState: "unsigned",
	}, "admin"); !errors.Is(err, ErrMarketNotFound) {
		t.Fatalf("unknown release plugin = %v, want not found", err)
	}
	if _, err := service.SaveRelease(context.Background(), PluginRelease{
		PluginName: manifest.Name, Version: "1.0.0", Checksum: "sha256:test", SignatureState: "unsigned", Channel: "arbitrary",
	}, "admin"); !errors.Is(err, ErrMarketInvalidInput) {
		t.Fatalf("invalid release channel = %v, want invalid input", err)
	}
	if _, err := service.SaveRelease(context.Background(), PluginRelease{
		PluginName: manifest.Name, Version: "1.0.0", Checksum: "sha256:test", SignatureState: "unsigned", RolloutState: "complete",
	}, "admin"); !errors.Is(err, ErrMarketInvalidInput) {
		t.Fatalf("invalid rollout state = %v, want invalid input", err)
	}
}

func TestMarketServiceRejectsUndeclaredRecordFields(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{Data: map[string]interface{}{
		"title": "declared",
		"extra": "must not be stored",
	}})
	if !errors.Is(err, ErrMarketInvalidInput) {
		t.Fatalf("undeclared record field = %v, want invalid input", err)
	}
}

func mustV2MarketManifest(t *testing.T) *Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(`
api_version: campusos.plugin/v2
host_api_version: v2
name: notes-v2
version: 1.0.0
runtime: wasm
scope: user
type: external
permissions:
  user:
    - resource: managed_data
      actions: [read, write, delete]
      purpose: "Save notes"
      revocable: true
    - resource: plugin_file
      actions: [read, write, delete]
      purpose: "Attach files"
      revocable: true
    - resource: plugin_search
      actions: [read]
      purpose: "Search notes"
      revocable: true
managed_data:
  collections:
    - name: notes
      owner: user
      fields: [{name: title, type: string, required: true}]
      searchable: [title]
      filterable: [title]
      max_records: 10
files:
  enabled: true
  allowed_mimes: [text/plain]
  allowed_extensions: [.txt]
  max_file_bytes: 1024
  quota_bytes: 4096
`))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestMarketServiceRequiresPublishedCatalogForNewConsent(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewMarketService(NewMemoryMarketStore(), storage, func(name string) (*Manifest, bool) { return manifest, name == manifest.Name })
	if err := service.SyncCatalog(context.Background(), []*Plugin{{Manifest: manifest}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Grant(context.Background(), manifest.Name, "user-a", nil); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("draft catalog grant = %v, want denied", err)
	}
}

func TestMarketServiceRejectsSpoofedFileMIMEAndDetectsOrphan(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UploadUserFile(ctx, manifest.Name, "user-a", "fake.txt", "text/plain", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("spoofed MIME upload = %v, want denied", err)
	}
	file, err := service.UploadUserFile(ctx, manifest.Name, "user-a", "note.txt", "text/plain", []byte("campusos"))
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := service.UserFilePath(ctx, manifest.Name, "user-a", stringID(file.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.UserFilePath(ctx, manifest.Name, "user-a", stringID(file.ID)); !errors.Is(err, ErrMarketNotFound) {
		t.Fatalf("orphan metadata lookup = %v, want not found", err)
	}
}

func TestMarketServiceSearchRequiresSeparateConsent(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", []string{"managed_data:read", "managed_data:write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SearchMyRecords(ctx, manifest.Name, "user-a", "notes", "", 1, 20); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("search without plugin_search consent = %v, want denied", err)
	}
}

func TestMarketServiceRetainsUserControlAfterCatalogIsHidden(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{RecordKey: "retained", Data: map[string]interface{}{"title": "mine"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetCatalogVisibility(ctx, manifest.Name, CatalogHidden, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetUserRecord(ctx, manifest.Name, "user-a", "notes", "retained"); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("hidden plugin runtime read = %v, want denied", err)
	}
	exported, err := service.ExportMyData(ctx, manifest.Name, "user-a")
	if err != nil || len(exported["records"].([]ManagedRecord)) != 1 {
		t.Fatalf("retained export = %#v, %v", exported, err)
	}
}

func TestMarketServiceBlocksRuntimeCallsWhenPluginStops(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{RecordKey: "retained", Data: map[string]interface{}{"title": "mine"}}); err != nil {
		t.Fatal(err)
	}
	service.SetPluginActiveResolver(func(string) bool { return false })
	if _, err := service.GetUserRecord(ctx, manifest.Name, "user-a", "notes", "retained"); !errors.Is(err, ErrMarketDenied) {
		t.Fatalf("stopped plugin runtime read = %v, want denied", err)
	}
	exported, err := service.ExportMyData(ctx, manifest.Name, "user-a")
	if err != nil || len(exported["records"].([]ManagedRecord)) != 1 {
		t.Fatalf("stopped plugin retained export = %#v, %v", exported, err)
	}
}

func TestMarketServiceRecordsVerifiedImportAndAuditsIt(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	release, err := service.RecordImportedRelease(context.Background(), manifest, "sha256:verified", "verified", "admin")
	if err != nil || release.SignatureState != "verified" || release.RolloutState != "imported" {
		t.Fatalf("imported release = %#v, %v", release, err)
	}
	audits, err := service.Audits(context.Background(), manifest.Name, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, audit := range audits {
		found = found || audit.Action == "release.import"
	}
	if !found {
		t.Fatalf("release import audit missing: %#v", audits)
	}
}

func TestMarketServiceAcceptsGovernedRequestForUninstalledPlugin(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	request, err := service.RequestInstall(context.Background(), "calendar-assistant", "user-a", "local package")
	if err != nil || request.Status != RequestPending || request.PluginName != "calendar-assistant" {
		t.Fatalf("uninstalled plugin request = %#v, %v", request, err)
	}
	if _, err := service.RequestInstall(context.Background(), "../unsafe", "user-a", ""); !errors.Is(err, ErrMarketInvalidInput) {
		t.Fatalf("unsafe plugin request = %v, want invalid input", err)
	}
}

func TestMarketServiceEnforcesManagedDataByteQuota(t *testing.T) {
	manifest := mustV2MarketManifest(t)
	manifest.ManagedData.DefaultQuotaByte = 24
	storage, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := newPublishedMarketService(t, manifest, storage)
	ctx := context.Background()
	if _, err := service.Grant(ctx, manifest.Name, "user-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{RecordKey: "one", Data: map[string]interface{}{"title": "12345678"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUserRecord(ctx, manifest.Name, "user-a", "notes", RecordInput{RecordKey: "two", Data: map[string]interface{}{"title": "12345678"}}); !errors.Is(err, ErrMarketQuotaExceeded) {
		t.Fatalf("record quota = %v, want exceeded", err)
	}
}

func newPublishedMarketService(t *testing.T, manifest *Manifest, storage corestorage.Port) *MarketService {
	t.Helper()
	service := NewMarketService(NewMemoryMarketStore(), storage, func(name string) (*Manifest, bool) { return manifest, name == manifest.Name })
	ctx := context.Background()
	if err := service.SyncCatalog(ctx, []*Plugin{{Manifest: manifest}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetCatalogVisibility(ctx, manifest.Name, CatalogPublished, "admin"); err != nil {
		t.Fatal(err)
	}
	return service
}

func stringID(value int64) string { return fmt.Sprintf("%d", value) }
func containsPath(value, expected string) bool {
	return strings.Contains(filepath.ToSlash(value), expected)
}
