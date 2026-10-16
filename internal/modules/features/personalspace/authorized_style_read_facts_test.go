package space

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

var styleReadOperations = []string{"validate", "preview", "export", "html_validate", "html_example", "zip_validate", "pack_example", "pack_zip"}

func runStyleReadOperation(t *testing.T, ctx context.Context, s *Service, op string, proof identityport.SessionPrincipalResolver) (json.RawMessage, error) {
	t.Helper()
	var result any
	var err error
	switch op {
	case "validate":
		result, err = s.ValidateOwnSpaceStylePackage(ctx, styleReadPackage(), proof)
	case "preview":
		result, err = s.PreviewOwnSpaceStylePackage(ctx, styleReadPackage(), proof)
	case "export":
		result, err = s.ExportOwnSpaceStylePackage(ctx, StyleExportRequest{}, proof)
	case "html_validate":
		result, err = s.ValidateOwnSpaceCustomHTML(ctx, "<section>Current</section>", proof)
	case "html_example":
		result, err = s.OwnSpaceCustomHTMLExample(ctx, proof)
	case "zip_validate":
		data := zipStylePack(t, styleVariantFiles())
		result, err = s.ValidateOwnSpaceStylePackZip(ctx, bytes.NewReader(data), int64(len(data)), proof)
	case "pack_example":
		result, err = s.OwnSpaceStylePackExample(ctx, proof)
	case "pack_zip":
		result, err = s.OwnSpaceStylePackExampleZip(ctx, proof)
	default:
		t.Fatal("unknown actual style read", op)
	}
	if err != nil {
		if v := reflect.ValueOf(result); v.IsValid() && !v.IsNil() {
			t.Fatal("failed read retained data", op, result, err)
		}
		return nil, err
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return raw, nil
}

func TestSpaceStyleReadAllPathsCurrentDomainFactsProofAndNoWrites(t *testing.T) {
	for _, op := range styleReadOperations {
		for _, mode := range []string{"current", "disabled", "admin", "missing", "credential", "email", "space_aba", "feature", "cancel"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s, repo, events := spaceOwnerFixture(t, true, "")
				if mode == "disabled" {
					at := time.Now()
					if err := repo.SetDisabled(ctx, "1001", &at, "", "held"); err != nil {
						t.Fatal(err)
					}
				}
				before, beforeEvents := repo.Snapshot(), events.Snapshot()
				calls, ran := 0, false
				proof := identityport.SessionPrincipalResolver(func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					calls++
					if mode == "admin" {
						p.Actor.Kind, p.Audience = "admin", "admin"
					}
					if calls == 2 {
						switch mode {
						case "credential":
							ran = true
							p.CredentialID = "changed"
						case "email":
							ran = true
							s.users.(*fakeUserLookup).byID["1001"].Email = "changed@example.test"
						case "space_aba":
							ran = true
							space, err := repo.GetByUserID(ctx, "1001")
							if err != nil {
								t.Fatal(err)
							}
							old := space.Bio
							space.Bio = "ABA"
							if err = repo.Upsert(ctx, space); err != nil {
								t.Fatal(err)
							}
							space.Bio = old
							if err = repo.Upsert(ctx, space); err != nil {
								t.Fatal(err)
							}
							before = repo.Snapshot()
						case "feature":
							ran = true
							s.SetPluginEnabledChecker(func() bool { return false })
						case "cancel":
							ran = true
							cancel()
						}
					}
					return p, e
				})
				if mode == "missing" {
					proof = nil
				}
				result, err := runStyleReadOperation(t, ctx, s, op, proof)
				allowed := mode == "current" || mode == "disabled"
				if (err == nil) != allowed || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("actual read decision/side effect", op, mode, string(result), err)
				}
				if mode != "current" && mode != "disabled" && mode != "admin" && mode != "missing" && !ran {
					t.Fatal("actual mutation did not run", op, mode)
				}
			})
		}
	}
}
