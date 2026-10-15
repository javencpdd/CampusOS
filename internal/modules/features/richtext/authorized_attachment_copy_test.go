package richtext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	personaldocuments "github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func attachmentCopyDOCX(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	z := zip.NewWriter(&body)
	for name, content := range map[string]string{
		"[Content_Types].xml": "<Types><Override PartName=\"/word/document.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml\" /></Types>",
		"word/document.xml":   "<document><body><p>private</p></body></document>",
	} {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

type attachmentCopyReader struct {
	documents    *personaldocuments.Service
	opens        int
	afterOpen    func()
	missingCheck bool
	truncated    bool
	afterCheck   func(context.Context, bool)
}

func (r *attachmentCopyReader) OpenOwnDocumentForAttachment(ctx context.Context, owner, id string) (PersonalDocumentAttachment, error) {
	r.opens++
	resolve := identityport.SessionPrincipalResolverFromContext(ctx)
	if resolve == nil {
		return PersonalDocumentAttachment{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil || p.Actor.ID != owner {
		return PersonalDocumentAttachment{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	source, err := r.documents.ReadOnly().OpenOwnAttachmentSource(ctx, resolve, id)
	if err != nil {
		return PersonalDocumentAttachment{}, err
	}
	if r.afterOpen != nil {
		r.afterOpen()
	}
	check := source.CheckCurrent
	if r.afterCheck != nil {
		check = func(ctx context.Context, lock bool) error {
			if err := source.CheckCurrent(ctx, lock); err != nil {
				return err
			}
			r.afterCheck(ctx, lock)
			return nil
		}
	}
	if r.missingCheck {
		check = nil
	}
	if r.truncated {
		original := source.Object.Reader
		source.Object.Reader = &attachmentCopyLimitedReader{Reader: io.LimitReader(original, 8), close: original}
	}
	return PersonalDocumentAttachment{ID: source.Document.ID, Version: source.Document.Version, Name: source.Document.Name, Format: source.Document.Format, Object: source.Object, CheckCurrent: check}, nil
}

type attachmentCopyLimitedReader struct {
	io.Reader
	close io.Closer
}

func (r *attachmentCopyLimitedReader) Close() error { return r.close.Close() }

func attachmentCopyDocuments(t *testing.T, objects corestorage.ObjectPort, resolve identityport.SessionPrincipalResolver) (*personaldocuments.Service, personaldocuments.DocumentDetail, []byte) {
	t.Helper()
	repo := personaldocuments.NewMemoryRepository()
	s, err := personaldocuments.NewService(repo, objects)
	if err != nil {
		t.Fatal(err)
	}
	s.SetReliability(reliability.NewService(transaction.NewMemory(repo), reliability.NewMemoryStore()))
	body := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")
	d, err := s.UploadForPrincipal(context.Background(), resolve, "source.pdf", personaldocuments.FormatPDF, "application/pdf", int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return s, d, body
}

func TestAttachmentCopyRealSourceCurrentProofAndIndependentSnapshot(t *testing.T) {
	for _, mode := range []string{"current", "current_docx", "other_destination", "admin", "missing", "source_trashed", "source_format", "missing_check", "truncated", "destination_changed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, threads, _, _, _, _ := articleCreateFixture(t)
			objects, adapter, _ := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			documents, d, body := attachmentCopyDocuments(t, objects, articleCreateProof)
			reader := &attachmentCopyReader{documents: documents}
			s.SetPersonalDocumentAttachmentReader(reader)
			objects.puts = 0
			proof := identityport.SessionPrincipalResolver(articleCreateProof)
			switch mode {
			case "current_docx":
				body = attachmentCopyDOCX(t)
				d, err = documents.UploadForPrincipal(ctx, articleCreateProof, "source.docx", personaldocuments.FormatDOCX, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", int64(len(body)), bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				objects.puts = 0
			case "other_destination":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := articleCreateProof(ctx)
					p.Actor.ID = "1002"
					return p, err
				}
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, err := articleCreateProof(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, err
				}
			case "missing":
				proof = nil
			case "source_trashed":
				d, err = documents.SetStatusForPrincipal(ctx, articleCreateProof, d.ID, d.Version, personaldocuments.StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
			case "source_format":
				d, err = documents.CreateTextForPrincipal(ctx, articleCreateProof, personaldocuments.CreateRequest{Name: "source.txt", Format: personaldocuments.FormatText, Content: "private"})
				if err != nil {
					t.Fatal(err)
				}
				objects.puts = 0
			case "missing_check":
				reader.missingCheck = true
			case "truncated":
				reader.truncated = true
			case "destination_changed":
				reader.afterOpen = func() {
					row, _ := threads.GetByID(ctx, draft.ThreadID)
					row.IsLocked = true
					if err := threads.Update(ctx, row); err != nil {
						t.Fatal(err)
					}
				}
			}
			beforeUsage, err := adapter.Usage("1001")
			if err != nil {
				t.Fatal(err)
			}
			ack, err := s.AttachPersonalDocumentForPrincipal(ctx, draft.ThreadID, d.ID, "published."+d.Format, proof)
			rows, readErr := s.store.LoadArticleBindingAuthorityFacts(ctx, draft.Article.ID, false)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "current" || mode == "current_docx" {
				if err != nil || ack == nil || len(rows) != 1 || objects.puts != 1 {
					t.Fatal("real source copy failed", ack, err, len(rows), objects.puts)
				}
				a, err := attachmentFromAuthorityFact(rows[0])
				if err != nil {
					t.Fatal(err)
				}
				if a.Asset.StorageObjectID == d.CurrentVersion.SourceObjectID {
					t.Fatal("copy retained private source reference")
				}
				d, err = documents.SetStatusForPrincipal(ctx, articleCreateProof, d.ID, d.Version, personaldocuments.StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
				opened, err := s.OpenAttachmentForPrincipal(ctx, draft.ThreadID, ack.AttachmentID, articleCreateProof)
				if err != nil {
					t.Fatal(err)
				}
				defer opened.Reader.Close()
				got, err := io.ReadAll(opened.Reader)
				if err != nil || !bytes.Equal(got, body) {
					t.Fatal("later private source lifecycle changed published snapshot", err)
				}
				return
			}
			if err == nil || ack != nil || len(rows) != 0 {
				t.Fatal("failed source copy published", mode, ack, err, len(rows))
			}
			if mode == "other_destination" || mode == "admin" || mode == "missing" {
				if reader.opens != 0 || objects.puts != 0 {
					t.Fatal("failed destination opened private source", mode, reader.opens, objects.puts)
				}
			}
			if mode == "truncated" && (objects.puts != 1 || objects.deletes != 1) {
				t.Fatal("partial source was not rejected and compensated", objects.puts, objects.deletes)
			}
			if usage, err := adapter.Usage("1001"); err != nil || usage != beforeUsage {
				t.Fatal("failed source copy left bytes", mode, usage, beforeUsage, err)
			}
		})
	}
}

func TestAttachmentCopyActualSourceABAAndAuditFailureCompensate(t *testing.T) {
	for _, mode := range []string{"source_status_aba", "source_deleted_object", "source_disabled", "credential", "cancel", "audit_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, _, cats, _, events, core := articleCreateFixture(t)
			objects, adapter, _ := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := articleCreateProof(ctx)
			proof := func(context.Context) (identityport.PrincipalContext, error) { return p, nil }
			documents, d, body := attachmentCopyDocuments(t, objects, proof)
			s.SetPersonalDocumentAttachmentReader(&attachmentCopyReader{documents: documents})
			objects.puts = 0
			ran := false
			if mode == "audit_failure" {
				durable := reliability.NewService(transaction.NewMemory(cats, s.store.(transaction.Snapshotter), events), articleCreateFailAudit{events})
				core.SetReliability(durable)
				s.SetReliability(durable)
			}
			objects.afterPut = func(corestorage.Object) {
				ran = true
				switch mode {
				case "source_status_aba":
					d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusTrashed)
					if err != nil {
						t.Fatal(err)
					}
					d, err = documents.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, personaldocuments.StatusActive)
					if err != nil {
						t.Fatal(err)
					}
				case "source_deleted_object":
					// The held stream remains open, but its current receipt is gone.
					o, err := objects.ObjectPort.Stat(ctx, "1001", d.CurrentVersion.SourceObjectID)
					if err != nil {
						t.Fatal(err)
					}
					if err := objects.ObjectPort.Delete(ctx, "1001", o.ID, o.Version); err != nil {
						t.Fatal(err)
					}
				case "source_disabled":
					documents.SetEnabledChecker(func() bool { return false })
				case "credential":
					p.CredentialID = "changed-current-session"
				case "cancel":
					cancel()
				}
			}
			ack, err := s.AttachPersonalDocumentForPrincipal(ctx, draft.ThreadID, d.ID, "published.pdf", proof)
			if err == nil || ack != nil || !ran || objects.puts != 1 || objects.deletes != 1 || errors.Is(err, ErrAttachmentCompensationPending) {
				t.Fatal("actual source failure was not closed and compensated", mode, ack, err, ran, objects.puts, objects.deletes)
			}
			rows, err := s.store.LoadArticleBindingAuthorityFacts(context.Background(), draft.Article.ID, false)
			if err != nil || len(rows) != 0 {
				t.Fatal("source failure published metadata", len(rows), err)
			}
			want := int64(len(body))
			if mode == "source_deleted_object" {
				want = 0
			}
			if usage, err := adapter.Usage("1001"); err != nil || usage != want {
				t.Fatal("wrong source/staged object cleaned", mode, usage, want, err)
			}
		})
	}
}
