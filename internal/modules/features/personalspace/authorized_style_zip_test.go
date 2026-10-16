package space

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func styleVariantFiles() map[string]string {
	return map[string]string{"style.yaml": `schema_version: page-style-pack.v1
target: personal-space
name: variant-space
version: 1.0.0
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
entry: templates/page.html
styles:
  - styles/theme.css
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
`, "templates/page.html": `<section class="cstyle-page"><h2>Current variant</h2></section>`, "styles/theme.css": `.public-space[data-campusos-space] .cstyle-page { padding: 16px; } @media (max-width: 720px) { .public-space[data-campusos-space] .cstyle-page { padding: 12px; } }`, "preview-desktop.png": "desktop preview", "preview-mobile.png": "mobile preview"}
}

type styleVariantReaderAt struct {
	io.ReaderAt
	after func()
	calls int
}

func (r *styleVariantReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	n, e := r.ReaderAt.ReadAt(p, offset)
	if r.after != nil {
		after := r.after
		r.after = nil
		after()
	}
	return n, e
}
func TestSpaceStyleVariantsZIPDetachedCurrentInputAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "audit", "outbox", "admin", "credential", "wrong_target", "short_input", "typed_nil"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, true, mode)
			files := styleVariantFiles()
			if mode == "wrong_target" {
				files["style.yaml"] = strings.ReplaceAll(files["style.yaml"], "target: personal-space", "target: homepage-customizer")
			}
			data := zipStylePack(t, files)
			reader := &styleVariantReaderAt{ReaderAt: bytes.NewReader(data)}
			input := io.ReaderAt(reader)
			proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
			changed := false
			size := int64(len(data))
			if mode == "admin" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					p.Actor.Kind, p.Audience = "admin", "admin"
					return p, e
				}
			}
			if mode == "credential" {
				reader.after = func() { changed = true }
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					if changed {
						p.CredentialID = "changed"
					}
					return p, e
				}
			}
			if mode == "short_input" {
				size++
			}
			if mode == "typed_nil" {
				var nilReader *bytes.Reader
				input = nilReader
			}
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ack, err := s.ApplyOwnSpaceStylePackZip(ctx, input, size, proof)
			if mode != "current" {
				if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("ZIP authority/required rollback failed", ack, err)
				}
				if mode == "admin" && reader.calls != 0 {
					t.Fatal("denied ZIP input read")
				}
				if mode == "credential" && !changed {
					t.Fatal("actual input hook did not execute")
				}
				return
			}
			if err != nil || ack == nil {
				t.Fatal(ack, err)
			}
			stored, err := repo.GetByUserID(ctx, "1001")
			if err != nil || stored.StyleName != "variant-space" || stored.StyleManifest == nil || stored.StyleManifest.SourceStylePack == nil {
				t.Fatal(stored, err)
			}
		})
	}
}
