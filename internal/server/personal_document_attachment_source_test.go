package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	personaldocuments "github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAttachmentCopyTrustedBridgeRequiresCurrentProofAndSourceCheck(t *testing.T) {
	ctx := context.Background()
	adapter, err := corestorage.NewLocalAdapterWithQuota(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	repo := personaldocuments.NewMemoryRepository()
	documents, err := personaldocuments.NewService(repo, objects)
	if err != nil {
		t.Fatal(err)
	}
	documents.SetReliability(reliability.NewService(transaction.NewMemory(repo), reliability.NewMemoryStore()))
	proof := func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", CredentialID: "attachment-bridge-session", AuthenticationStrength: "password"}, nil
	}
	body := "%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF"
	d, err := documents.UploadForPrincipal(ctx, proof, "private.pdf", personaldocuments.FormatPDF, "application/pdf", int64(len(body)), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	bridge := personalDocumentPDFReader{service: func() *personaldocuments.Service { return documents }}
	if source, err := bridge.OpenOwnDocumentForAttachment(ctx, "1001", d.ID); err == nil || source.Object.Reader != nil {
		t.Fatal("bare owner authenticated bridge", source, err)
	}
	bound := identityport.WithSessionPrincipalResolver(ctx, proof)
	if source, err := bridge.OpenOwnDocumentForAttachment(bound, "1002", d.ID); err == nil || source.Object.Reader != nil {
		t.Fatal("cross owner authenticated bridge", source, err)
	}
	source, err := bridge.OpenOwnDocumentForAttachment(bound, "1001", d.ID)
	if err != nil || source.Version != d.Version || source.CheckCurrent == nil || source.Object.Reader == nil {
		t.Fatal("trusted source proof missing", source, err)
	}
	defer source.Object.Reader.Close()
	if err := source.CheckCurrent(bound, false); err != nil {
		t.Fatal(err)
	}
	d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusTrashed)
	if err != nil {
		t.Fatal(err)
	}
	d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.CheckCurrent(bound, false); err == nil {
		t.Fatal("bridge reused source after supported version ABA")
	}
	for _, stopped := range []personalDocumentPDFReader{{}, {service: func() *personaldocuments.Service { return nil }}} {
		if _, err := stopped.OpenOwnDocumentForAttachment(bound, "1001", d.ID); !errors.Is(err, richtext.ErrAssetUnavailable) {
			t.Fatal("stopped bridge was available", err)
		}
	}
}
