package personaldocuments

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type attachmentSourceObjects struct {
	corestorage.ObjectPort
	opens    int
	putError error
}

func (o *attachmentSourceObjects) Open(ctx context.Context, owner, id string) (corestorage.ObjectReader, error) {
	o.opens++
	return o.ObjectPort.Open(ctx, owner, id)
}
func (o *attachmentSourceObjects) Put(ctx context.Context, owner string, r corestorage.PutRequest) (corestorage.Object, error) {
	if o.putError != nil {
		return corestorage.Object{}, o.putError
	}
	return o.ObjectPort.Put(ctx, owner, r)
}
func (o *attachmentSourceObjects) LockOwnedObjectForCommand(ctx context.Context, owner, id string) (corestorage.Object, error) {
	return o.ObjectPort.(corestorage.OwnedObjectCommandLocker).LockOwnedObjectForCommand(ctx, owner, id)
}
func TestAttachmentCopySourceCurrentPolicyBeforeIOAndDetachedProof(t *testing.T) {
	for _, mode := range []string{"current", "other", "admin", "missing", "disabled", "trashed", "format", "provider_pending"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := newReliableTestService(t)
			proof := documentProof("1001")
			d, err := s.UploadForPrincipal(ctx, proof, "private.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
			if err != nil {
				t.Fatal(err)
			}
			objects := &attachmentSourceObjects{ObjectPort: s.objects}
			s.objects = objects
			switch mode {
			case "other":
				proof = documentProof("1002")
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := documentProof("1001")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, err
				}
			case "missing":
				proof = nil
			case "disabled":
				s.SetEnabledChecker(func() bool { return false })
			case "trashed":
				d, err = s.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
			case "format":
				d, err = s.CreateTextForPrincipal(ctx, proof, CreateRequest{Name: "private.txt", Format: FormatText, Content: "private"})
				if err != nil {
					t.Fatal(err)
				}
			case "provider_pending":
				objects.putError = errors.Join(corestorage.ErrObjectQuota, corestorage.ErrObjectCompensationPending)
				d, err = s.UploadForPrincipal(ctx, proof, "pending.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
				if !errors.Is(err, ErrDocumentCompensationPending) || d.ID != "" {
					t.Fatal("provider pending was mislabeled quota success", d, err)
				}
				return
			}
			source, err := s.ReadOnly().OpenOwnAttachmentSource(ctx, proof, d.ID)
			if mode != "current" {
				if err == nil || source.Object.Reader != nil || source.CheckCurrent != nil || objects.opens != 0 {
					t.Fatal("failed source policy performed byte I/O", mode, err, objects.opens)
				}
				return
			}
			if err != nil || source.CheckCurrent == nil || source.Object.Reader == nil || source.Document.Version != d.Version || objects.opens != 1 {
				t.Fatal(source.Document, err, objects.opens)
			}
			defer source.Object.Reader.Close()
			source.Document.Name = "forged"
			source.Document.CurrentVersion.SourceObjectID = "forged"
			source.Document.CurrentVersion.SHA256 = "forged"
			source.Object.Object.SHA256 = "forged"
			if err := source.CheckCurrent(ctx, false); err != nil {
				t.Fatal("caller mutation aliased expected proof", err)
			}
			if err := source.CheckCurrent(ctx, true); err == nil {
				t.Fatal("source locks accepted nontransaction context")
			}
			if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error { return source.CheckCurrent(tx, true) }); err != nil {
				t.Fatal("actual memory command proof failed", err)
			}
		})
	}
}

func TestAttachmentCopySourceRejectsCurrentVersionCredentialAndObjectChanges(t *testing.T) {
	for _, mode := range []string{"status_aba", "credential", "disabled", "object_deleted"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := newReliableTestService(t)
			p, _ := documentProof("1001")(ctx)
			resolve := func(context.Context) (identityport.PrincipalContext, error) { return p, nil }
			d, err := s.UploadForPrincipal(ctx, resolve, "private.pdf", FormatPDF, "application/pdf", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
			if err != nil {
				t.Fatal(err)
			}
			source, err := s.ReadOnly().OpenOwnAttachmentSource(ctx, resolve, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Object.Reader.Close()
			switch mode {
			case "status_aba":
				d, err = s.SetStatusForPrincipal(ctx, resolve, d.ID, d.Version, StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
				d, err = s.SetStatusForPrincipal(ctx, resolve, d.ID, d.Version, StatusActive)
				if err != nil {
					t.Fatal(err)
				}
			case "credential":
				p.CredentialID = "new-current-session"
			case "disabled":
				s.SetEnabledChecker(func() bool { return false })
			case "object_deleted":
				o := source.Object.Object
				if err := s.objects.Delete(ctx, "1001", o.ID, o.Version); err != nil {
					t.Fatal(err)
				}
			}
			if err := source.CheckCurrent(ctx, false); err == nil {
				t.Fatal("changed source proof reused", mode)
			}
			if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error { return source.CheckCurrent(tx, true) }); err == nil {
				t.Fatal("changed source execution proof reused", mode)
			}
		})
	}
}
