package personaldocuments

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func documentProof(owner string) DocumentPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract,
			Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user",
			CredentialID: "document-test-session", AuthenticationStrength: "password"}, nil
	}
}

func TestDocumentAuthenticatedReadLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	d, err := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-first"})
	if err != nil {
		t.Fatal(err)
	}
	initial := d
	d, err = s.Save(ctx, "1001", d.ID, SaveRequest{ExpectedVersion: d.Version, Content: "private-second"})
	if err != nil {
		t.Fatal(err)
	}
	proof := documentProof("1001")
	for _, vid := range []string{"", initial.CurrentVersionID} {
		_, content, err := s.TextContentForPrincipal(ctx, proof, d.ID, vid)
		expected := "private-second"
		if vid != "" {
			expected = "private-first"
		}
		if err != nil || content != expected {
			t.Fatalf("content=%q err=%v", content, err)
		}
		_, stream, err := s.OpenVersionForPrincipal(ctx, proof, d.ID, vid)
		if err != nil || stream.Reader == nil {
			t.Fatalf("open: %v", err)
		}
		_ = stream.Reader.Close()
	}
	preview, err := s.PreviewForPrincipal(ctx, proof, d.ID, "")
	if err != nil || !strings.Contains(preview.RenderedHTML, "private-second") {
		t.Fatalf("preview: %#v %v", preview, err)
	}
	versions, err := s.VersionsForPrincipal(ctx, proof, d.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions: %#v %v", versions, err)
	}
	if _, _, err := s.TextContentForPrincipal(ctx, documentProof("1002"), d.ID, ""); err == nil {
		t.Fatal("cross-owner bytes allowed")
	}
	if _, _, err := s.TextContentForPrincipal(ctx, nil, d.ID, ""); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	trashed, err := s.SetStatus(ctx, "1001", d.ID, d.Version, StatusTrashed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TextContentForPrincipal(ctx, proof, d.ID, initial.CurrentVersionID); err == nil {
		t.Fatal("tombstone original allowed")
	}
	if _, err := s.VersionsForPrincipal(ctx, proof, d.ID); err == nil {
		t.Fatal("tombstone versions allowed")
	}
	metadata, err := s.GetForPrincipal(ctx, proof, d.ID)
	if err != nil || metadata.Status != StatusTrashed || metadata.CurrentVersion != nil || metadata.CurrentVersionID != "" {
		t.Fatalf("unsafe tombstone: %#v %v", metadata, err)
	}
	list, err := s.ListForPrincipal(ctx, proof, StatusTrashed)
	if err != nil || len(list) != 1 || list[0].CurrentVersion != nil {
		t.Fatalf("tombstone list: %#v %v", list, err)
	}
	if _, err := s.SetStatus(ctx, "1001", d.ID, trashed.Version, StatusActive); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TextContentForPrincipal(ctx, proof, d.ID, ""); err != nil {
		t.Fatal(err)
	}
}

type documentReadRaceRepository struct {
	Repository
	getCalls  int
	listCalls int
	afterGet  func(int)
	afterList func(int)
}

func (r *documentReadRaceRepository) Get(ctx context.Context, owner, id string) (DocumentDetail, error) {
	d, err := r.Repository.Get(ctx, owner, id)
	r.getCalls++
	if r.afterGet != nil {
		r.afterGet(r.getCalls)
	}
	return d, err
}

func (r *documentReadRaceRepository) List(ctx context.Context, owner string, f ListFilter) ([]DocumentDetail, error) {
	d, err := r.Repository.List(ctx, owner, f)
	r.listCalls++
	if r.afterList != nil {
		r.afterList(r.listCalls)
	}
	return d, err
}

func TestDocumentAuthenticatedReadRejectsRevocationAndDrift(t *testing.T) {
	for _, mode := range []string{"revoked", "strength", "owner", "credential", "domain"} {
		t.Run(mode, func(t *testing.T) {
			s := newTestService(t)
			d, err := s.CreateText(context.Background(), "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-body"})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := documentProof("1001")(ctx)
				calls++
				if calls >= 2 {
					switch mode {
					case "revoked":
						return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
					case "strength":
						p.AuthenticationStrength = "mfa"
					case "owner":
						p.Actor.ID = "1002"
					case "credential":
						p.CredentialID = "replacement"
					case "domain":
						p.Actor.Kind, p.Audience = "admin", "admin"
					}
				}
				return p, nil
			}
			_, body, err := s.TextContentForPrincipal(context.Background(), proof, d.ID, "")
			if err == nil || body != "" {
				t.Fatalf("stale private body=%q err=%v", body, err)
			}
		})
	}
}

func TestDocumentAuthenticatedReadRejectsCommittedFactsChanges(t *testing.T) {
	for _, mode := range []string{"body", "trash", "trash-aba", "owner", "purge", "version-tamper"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s := newTestService(t)
			d, err := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "private-body"})
			if err != nil {
				t.Fatal(err)
			}
			base := s.repository.(*MemoryRepository)
			race := &documentReadRaceRepository{Repository: base}
			race.afterGet = func(n int) {
				if n != 2 {
					return
				}
				switch mode {
				case "body":
					_, err = s.Save(ctx, "1001", d.ID, SaveRequest{ExpectedVersion: d.Version, Content: "new-body"})
				case "trash", "trash-aba":
					var next DocumentDetail
					next, err = base.SetStatus(ctx, "1001", d.ID, d.Version, StatusTrashed)
					if mode == "trash-aba" && err == nil {
						_, err = base.SetStatus(ctx, "1001", d.ID, next.Version, StatusActive)
					}
				case "owner":
					base.mu.Lock()
					value := base.docs[d.ID]
					value.OwnerID = "1002"
					base.docs[d.ID] = value
					base.mu.Unlock()
				case "purge":
					base.mu.Lock()
					delete(base.docs, d.ID)
					base.mu.Unlock()
				case "version-tamper":
					base.mu.Lock()
					versions := base.versions[d.ID]
					versions[0].SHA256 = "tampered"
					base.versions[d.ID] = versions
					base.mu.Unlock()
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			s.repository = race
			_, body, err := s.TextContentForPrincipal(ctx, documentProof("1001"), d.ID, "")
			if err == nil || body != "" {
				t.Fatalf("stale body=%q err=%v", body, err)
			}
		})
	}
}

func TestDocumentAuthenticatedListEmptyAndWholeSnapshot(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	base := s.repository
	race := &documentReadRaceRepository{Repository: base}
	race.afterList = func(n int) {
		if n == 1 {
			_, err := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "new-member"})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	s.repository = race
	if list, err := s.ListForPrincipal(ctx, documentProof("1001"), StatusActive); err == nil || list != nil {
		t.Fatalf("mixed page: %#v %v", list, err)
	}
	if list, err := s.ListForPrincipal(ctx, documentProof("1001"), StatusActive); err != nil || len(list) != 1 {
		t.Fatalf("fresh page: %#v %v", list, err)
	}
	calls := 0
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		calls++
		if calls > 1 {
			return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
		}
		return documentProof("1002")(ctx)
	}
	if list, err := s.ListForPrincipal(ctx, resolve, StatusActive); err == nil || list != nil {
		t.Fatalf("revoked empty page: %#v %v", list, err)
	}
}
