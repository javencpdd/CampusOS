package space

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func writeStyleVariantSource(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, "space-style-packs", "variant-space")
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestSpaceStyleVariantsSourceCurrentStrictDeliveryAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "audit", "outbox", "admin", "wrong_target", "legacy_readonly", "invalid_name"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			files := styleVariantFiles()
			if mode == "wrong_target" {
				files["style.yaml"] = strings.ReplaceAll(files["style.yaml"], "target: personal-space", "target: homepage-customizer")
			}
			if mode == "legacy_readonly" {
				files["style.yaml"] = strings.ReplaceAll(files["style.yaml"], "delivery_contract: campusos.appearance-delivery/v1\n", "")
			}
			writeStyleVariantSource(t, root, files)
			s, repo, events := spaceOwnerFixture(t, true, mode)
			proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
			name := "variant-space"
			if mode == "invalid_name" {
				name = "../secret"
			}
			if mode == "admin" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					p.Actor.Kind, p.Audience = "admin", "admin"
					return p, e
				}
			}
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ack, err := s.ApplyOwnSpaceSourceStylePack(ctx, name, proof)
			if mode != "current" {
				if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) || strings.Contains(err.Error(), root) {
					t.Fatal("source authority/required rollback failed", ack, err)
				}
				return
			}
			if err != nil || ack == nil {
				t.Fatal(ack, err)
			}
			stored, err := repo.GetByUserID(ctx, "1001")
			if err != nil || stored.StyleManifest == nil || stored.StyleName != "variant-space" {
				t.Fatal(stored, err)
			}
		})
	}
}

func TestSpaceStyleVariantsSourceWholeContentRootAndCurrentProofChangesClose(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"root", "body", "preview", "credential", "after_write"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			dir := writeStyleVariantSource(t, root, styleVariantFiles())
			s, repo, events := spaceOwnerFixture(t, true, "")
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ran := false
			mutate := func() {
				ran = true
				switch mode {
				case "root":
					t.Setenv("RESOURCE_DIR", t.TempDir())
				case "body":
					if err := os.WriteFile(filepath.Join(dir, "templates/page.html"), []byte(`<section>Changed</section>`), 0644); err != nil {
						t.Fatal(err)
					}
				case "preview":
					if err := os.WriteFile(filepath.Join(dir, "preview-mobile.png"), []byte("changed preview"), 0644); err != nil {
						t.Fatal(err)
					}
				case "after_write":
					if err := os.WriteFile(filepath.Join(dir, "templates/page.html"), []byte(`<section>After metadata</section>`), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "after_write" {
				s.repo = &spaceOwnerRepoHook{MemoryRepository: repo, after: func(context.Context, *Space) { mutate() }}
			}
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				calls++
				if calls == 2 && mode != "after_write" {
					mutate()
				}
				if ran && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, e
			}
			ack, err := s.ApplyOwnSpaceSourceStylePack(ctx, "variant-space", proof)
			if !ran || err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("actual source/config/proof change accepted", mode, ack, err, ran)
			}
		})
	}
}
