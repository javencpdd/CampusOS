package space

import (
	"context"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestSpaceStyleVariantsHTMLCurrentAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "audit", "outbox", "unsafe", "admin", "credential"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, true, mode)
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			html := `<section><h1>Owned HTML</h1></section>`
			proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
			if mode == "unsafe" {
				html = `<script>window.evil()</script>`
			}
			if mode == "admin" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					p.Actor.Kind, p.Audience = "admin", "admin"
					return p, e
				}
			}
			if mode == "credential" {
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					calls++
					if calls > 1 {
						p.CredentialID = "changed"
					}
					return p, e
				}
			}
			ack, err := s.ApplyOwnSpaceCustomHTML(ctx, html, proof)
			if mode != "current" {
				if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
					t.Fatal("HTML authority/required rollback failed", ack, err)
				}
				return
			}
			if err != nil || ack == nil {
				t.Fatal(ack, err)
			}
			stored, err := repo.GetByUserID(ctx, "1001")
			if err != nil || stored.StyleManifest == nil || !stored.StyleManifest.CustomHTMLEnabled || stored.StyleManifest.CustomHTML != html {
				t.Fatal(stored, err)
			}
		})
	}
}
