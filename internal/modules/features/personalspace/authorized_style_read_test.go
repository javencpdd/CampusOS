package space

import (
	"bytes"
	"context"
	"encoding/json"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
	"io"
	"reflect"
	"strings"
	"testing"
)

func styleReadPackage() StylePackage {
	return StylePackage{Manifest: StyleManifest{SchemaVersion: StyleSchemaVersion, Name: "safe-read", Version: "1.0.0", Layout: "blog", Components: []StyleComponent{{Slot: "header", Type: "profile-header"}, {Slot: "main", Type: "content-list"}}, Tokens: map[string]string{"color.primary": "#2563eb"}}}
}

func TestSpaceStyleReadValidateCurrentInvalidAndNoMutation(t *testing.T) {
	for _, valid := range []bool{true, false} {
		s, repo, events := spaceOwnerFixture(t, false, "")
		before, beforeEvents := repo.Snapshot(), events.Snapshot()
		pkg := styleReadPackage()
		if !valid {
			pkg.Manifest.SchemaVersion = "unknown"
		}
		result, err := s.ValidateOwnSpaceStylePackage(context.Background(), pkg, spaceOwnerProof)
		if err != nil || result == nil || result.Valid != valid || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
			t.Fatal(result, err)
		}
	}
}

func TestSpaceStyleReadPreviewDetachedInputAndCurrentOwner(t *testing.T) {
	s, repo, events := spaceOwnerFixture(t, true, "")
	stored, err := repo.GetByUserID(context.Background(), "1001")
	if err != nil {
		t.Fatal(err)
	}
	stored.LastSyncError = "/private/host/path is not preview data"
	if err := repo.Upsert(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	pkg := styleReadPackage()
	calls := 0
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		calls++
		if calls == 2 {
			pkg.Manifest.Tokens["color.primary"] = "#ff0000"
		}
		return spaceOwnerProof(ctx)
	}
	before, beforeEvents := repo.Snapshot(), events.Snapshot()
	result, err := s.PreviewOwnSpaceStylePackage(context.Background(), pkg, proof)
	if err != nil || result == nil || result.Tokens["color.primary"] != "#2563eb" || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
		t.Fatal(result, err)
	}
	raw, _ := json.Marshal(result)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields["space"] != nil || fields["owner"] != nil || strings.Contains(string(raw), "/private/host/path") {
		t.Fatal("raw preview facts exposed", string(raw))
	}
}

func TestSpaceStyleReadExportCurrentDefaultAndExistingWithoutWrites(t *testing.T) {
	for _, existing := range []bool{false, true} {
		s, repo, events := spaceOwnerFixture(t, existing, "")
		before, beforeEvents := repo.Snapshot(), events.Snapshot()
		result, err := s.ExportOwnSpaceStylePackage(context.Background(), StyleExportRequest{}, spaceOwnerProof)
		if err != nil || result == nil || result.Filename == "" || !result.Validation.Valid || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
			t.Fatal(result, err)
		}
	}
}

func TestSpaceStyleReadHTMLValidationCurrentAndUnsafe(t *testing.T) {
	for _, html := range []string{"<section>Current</section>", "<script>alert(1)</script>"} {
		s, repo, events := spaceOwnerFixture(t, false, "")
		before, beforeEvents := repo.Snapshot(), events.Snapshot()
		result, err := s.ValidateOwnSpaceCustomHTML(context.Background(), html, spaceOwnerProof)
		if err != nil || result == nil || result.Valid != (html == "<section>Current</section>") || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
			t.Fatal(result, err)
		}
	}
}

func TestSpaceStyleReadHTMLExampleCurrentDefaultExistingAndNoMutation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		s, repo, events := spaceOwnerFixture(t, existing, "")
		before, beforeEvents := repo.Snapshot(), events.Snapshot()
		result, err := s.OwnSpaceCustomHTMLExample(context.Background(), spaceOwnerProof)
		if err != nil || result == nil || result.HTML == "" || !result.Validation.Valid || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
			t.Fatal(result, err)
		}
	}
}

func TestSpaceStyleReadZIPValidationBoundedDetachedCurrentAndInvalidInput(t *testing.T) {
	for _, mode := range []string{"current", "malformed", "short", "typed_nil", "over_limit", "denied_before_io", "credential_during_io"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, false, "")
			data := zipStylePack(t, styleVariantFiles())
			if mode == "malformed" {
				data = []byte("invalid zip")
			}
			reader := &styleVariantReaderAt{ReaderAt: bytes.NewReader(data)}
			var input io.ReaderAt = reader
			size := int64(len(data))
			if mode == "short" {
				size++
			}
			if mode == "over_limit" {
				size = stylepack.MaxPackageBytes + 1
			}
			if mode == "typed_nil" {
				var empty *bytes.Reader
				input = empty
			}
			changed := false
			if mode == "credential_during_io" {
				reader.after = func() { changed = true }
			}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if changed {
					p.CredentialID = "changed"
				}
				if mode == "denied_before_io" {
					p.Actor.Kind, p.Audience = "admin", "admin"
				}
				return p, e
			}
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			result, err := s.ValidateOwnSpaceStylePackZip(context.Background(), input, size, proof)
			allowed := mode == "current" || mode == "malformed"
			if (err == nil) != allowed || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal(result, err)
			}
			if allowed && (result == nil || result.Validation.Valid != (mode == "current")) {
				t.Fatal(result)
			}
			if !allowed && result != nil {
				t.Fatal("failed read retained result", result)
			}
			if mode == "denied_before_io" && reader.calls != 0 || mode == "credential_during_io" && !changed {
				t.Fatal("actual input boundary not exercised", mode, reader.calls, changed)
			}
		})
	}
}

func TestSpaceStyleReadPackExampleCurrentAndNoMutation(t *testing.T) {
	s, repo, events := spaceOwnerFixture(t, false, "")
	before, beforeEvents := repo.Snapshot(), events.Snapshot()
	result, err := s.OwnSpaceStylePackExample(context.Background(), spaceOwnerProof)
	if err != nil || result == nil || result.Filename == "" || len(result.Files) == 0 || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
		t.Fatal(result, err)
	}
}

func TestSpaceStyleReadZIPExampleFullArchiveBeforeFinalProof(t *testing.T) {
	for _, changed := range []bool{false, true} {
		s, repo, events := spaceOwnerFixture(t, false, "")
		before, beforeEvents := repo.Snapshot(), events.Snapshot()
		calls := 0
		proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
			p, e := spaceOwnerProof(ctx)
			calls++
			if changed && calls > 1 {
				p.CredentialID = "changed"
			}
			return p, e
		}
		result, err := s.OwnSpaceStylePackExampleZip(context.Background(), proof)
		if (err == nil) == changed || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
			t.Fatal(result, err)
		}
		if changed {
			if result != nil {
				t.Fatal("encoded response retained after revocation")
			}
			continue
		}
		pack, validation := stylepack.LoadZipStrict(bytes.NewReader(result.Data), int64(len(result.Data)))
		if !validation.Valid || pack == nil || pack.Manifest.Target != "personal-space" {
			t.Fatal(validation)
		}
	}
}
