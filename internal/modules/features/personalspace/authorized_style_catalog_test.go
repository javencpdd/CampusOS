package space

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestSpaceStyleCatalogCurrentDefaultExistingEmptyInvalidAndSafeProjection(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "missing_root", "empty", "disabled", "legacy", "wrong_target", "broken_link", "bad_directory", "root_file", "missing_proof", "admin", "suspended", "feature_disabled"} {
			t.Run(map[bool]string{false: "default", true: "existing"}[existing]+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				root := t.TempDir()
				t.Setenv("RESOURCE_DIR", root)
				files := styleVariantFiles()
				if mode == "legacy" {
					files["style.yaml"] = strings.ReplaceAll(files["style.yaml"], "delivery_contract: campusos.appearance-delivery/v1\n", "")
				}
				if mode == "wrong_target" {
					files["style.yaml"] = strings.ReplaceAll(files["style.yaml"], "target: personal-space", "target: homepage")
				}
				if mode != "missing_root" && mode != "root_file" {
					dir := writeStyleVariantSource(t, root, files)
					if mode == "empty" {
						if err := os.RemoveAll(dir); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "bad_directory" {
						if err := os.Rename(dir, filepath.Join(filepath.Dir(dir), "INVALID")); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "broken_link" {
						if err := os.Symlink(filepath.Join(root, "private-missing"), filepath.Join(dir, "broken.png")); err != nil {
							t.Fatal(err)
						}
					}
				}
				if mode == "root_file" {
					if err := os.WriteFile(filepath.Join(root, "space-style-packs"), []byte("not directory"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				s, repo, events := spaceOwnerFixture(t, existing, "")
				proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
				switch mode {
				case "disabled":
					if existing {
						at := time.Now()
						if err := repo.SetDisabled(ctx, "1001", &at, "", "held"); err != nil {
							t.Fatal(err)
						}
					}
				case "missing_proof":
					proof = nil
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						p.Actor.Kind, p.Audience = "admin", "admin"
						return p, e
					}
				case "suspended":
					s.users.(*fakeUserLookup).byID["1001"].Status = "suspended"
				case "feature_disabled":
					s.SetPluginEnabledChecker(func() bool { return false })
				}
				before, beforeEvents := repo.Snapshot(), events.Snapshot()
				result, err := s.ListOwnSpaceSourceStylePacks(ctx, proof)
				allowed := mode != "missing_proof" && mode != "admin" && mode != "suspended" && mode != "feature_disabled" && mode != "root_file"
				if (err == nil) != allowed || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("catalog authorization or read side effect", result, err)
				}
				if !allowed {
					if result != nil || strings.Contains(err.Error(), root) {
						t.Fatal(result, err)
					}
					return
				}
				raw, err := json.Marshal(result)
				var wire struct{ Items []map[string]json.RawMessage }
				if err != nil || json.Unmarshal(raw, &wire) != nil || strings.Contains(string(raw), root) || strings.Contains(string(raw), "raw_files") || strings.Contains(string(raw), "private-missing") {
					t.Fatal("unsafe catalog projection", string(raw), err)
				}
				if result.Items == nil {
					t.Fatal("empty catalog must be []")
				}
				if mode == "missing_root" || mode == "empty" {
					if len(result.Items) != 0 {
						t.Fatal(result)
					}
					return
				}
				if len(result.Items) != 1 || wire.Items[0]["path"] != nil {
					t.Fatal("physical Path returned", string(raw))
				}
				valid := mode == "current" || mode == "disabled"
				if result.Items[0].Validation.Valid != valid {
					t.Fatal(result.Items[0].Validation)
				}
			})
		}
	}
}

func TestSpaceStyleCatalogCurrentSourceInventoryOwnerAndProofChangesClose(t *testing.T) {
	for _, mode := range []string{"body", "preview", "manifest", "raw_asset", "add", "remove", "empty_add", "root", "space_aba", "user_email", "credential", "feature", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := t.TempDir()
			t.Setenv("RESOURCE_DIR", root)
			dir := writeStyleVariantSource(t, root, styleVariantFiles())
			if mode == "empty_add" {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			}
			s, repo, events := spaceOwnerFixture(t, true, "")
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			calls, ran := 0, false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				calls++
				if calls == 2 {
					ran = true
					var err error
					switch mode {
					case "body":
						err = os.WriteFile(filepath.Join(dir, "templates/page.html"), []byte("<section>Changed</section>"), 0644)
					case "preview":
						err = os.WriteFile(filepath.Join(dir, "preview-mobile.png"), []byte("changed"), 0644)
					case "manifest":
						err = os.WriteFile(filepath.Join(dir, "style.yaml"), []byte(strings.ReplaceAll(styleVariantFiles()["style.yaml"], "version: 1.0.0", "version: 1.0.1")), 0644)
					case "raw_asset":
						err = os.WriteFile(filepath.Join(dir, "extra.png"), []byte("changed raw"), 0644)
					case "add", "empty_add":
						writeStyleVariantSource(t, root, styleVariantFiles())
						if mode == "add" {
							err = os.Mkdir(filepath.Join(root, "space-style-packs", "added"), 0755)
						}
					case "remove":
						err = os.RemoveAll(dir)
					case "root":
						t.Setenv("RESOURCE_DIR", t.TempDir())
					case "space_aba":
						space, getErr := repo.GetByUserID(ctx, "1001")
						if getErr != nil {
							t.Fatal(getErr)
						}
						old := space.Bio
						space.Bio = "ABA"
						if err = repo.Upsert(ctx, space); err == nil {
							space.Bio = old
							err = repo.Upsert(ctx, space)
						}
						before = repo.Snapshot()
					case "user_email":
						s.users.(*fakeUserLookup).byID["1001"].Email = "changed@example.test"
					case "feature":
						s.SetPluginEnabledChecker(func() bool { return false })
					case "cancel":
						cancel()
					}
					if err != nil {
						t.Fatal("mutation did not execute", mode, err)
					}
				}
				if ran && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, e
			}
			result, err := s.ListOwnSpaceSourceStylePacks(ctx, proof)
			if !ran || err == nil || result != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("changed catalog accepted or read wrote state", mode, result, err, ran)
			}
		})
	}
}
