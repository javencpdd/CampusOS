package richtext

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestAttachmentUploadCheckedContentAndCurrentBudgets(t *testing.T) {
	for _, mode := range []string{"signature", "unsupported", "ooxml_invalid", "ooxml_valid", "count", "total", "hint_limit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _, _, _, _, _ := articleCreateFixture(t)
			objects, adapter, _ := attachmentUploadObjectFixture(t, s)
			draft, err := s.CreateDraftForPrincipal(ctx, "Owner", articleCreateRequest(), articleCreateProof)
			if err != nil {
				t.Fatal(err)
			}
			body := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			if mode == "count" {
				for i := 0; i < MaxArticleAttachmentCount; i++ {
					if _, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "old.pdf", "application/pdf", int64(len(body)), bytes.NewReader(body)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "total" {
				if _, err := adapter.SetQuota(ctx, "1001", 100*1024*1024, ""); err != nil {
					t.Fatal(err)
				}
				large := make([]byte, MaxArticleAttachmentBytes)
				copy(large, body)
				for i := 0; i < 2; i++ {
					if _, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "large.pdf", "application/pdf", int64(len(large)), bytes.NewReader(large)); err != nil {
						t.Fatal(err)
					}
				}
			}
			name, mime := "new.pdf", "application/pdf"
			want := ErrAttachmentContent
			switch mode {
			case "signature":
				body = []byte("untrusted bytes")
			case "unsupported":
				name = "program.exe"
				body = []byte("MZ")
				mime = "application/octet-stream"
				want = ErrAttachmentType
			case "ooxml_invalid", "ooxml_valid":
				name, mime = "office.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
				entry := "payload.txt"
				if mode == "ooxml_valid" {
					entry = "word/document.xml"
				}
				body = testOOXMLPayload(t, entry)
			case "count":
				want = ErrAttachmentLimit
			case "total":
				want = ErrAttachmentTotalSize
			case "hint_limit":
				want = ErrAssetTooLarge
			}
			before, err := s.store.LoadArticleBindingAuthorityFacts(ctx, draft.Article.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			oldUsage, err := adapter.Usage("1001")
			if err != nil {
				t.Fatal(err)
			}
			size := int64(len(body))
			if mode == "hint_limit" {
				size = MaxArticleAttachmentBytes + 1
			}
			objects.puts = 0
			reader := &attachmentUploadCountReader{Reader: bytes.NewReader(body)}
			ack, err := s.UploadAttachmentForPrincipal(ctx, draft.ThreadID, name, mime, size, reader, articleCreateProof)
			if mode == "ooxml_valid" {
				if err != nil || ack == nil || objects.puts != 1 {
					t.Fatal("valid actual Office upload failed", ack, err, objects.puts)
				}
				return
			}
			if !errors.Is(err, want) || ack != nil {
				t.Fatal("unsafe content or budget accepted", mode, ack, err, want)
			}
			rows, err := s.store.LoadArticleBindingAuthorityFacts(ctx, draft.Article.ID, false)
			if err != nil || len(rows) != len(before) {
				t.Fatal("failed content published metadata", len(rows), len(before), err)
			}
			if usage, err := adapter.Usage("1001"); err != nil || usage != oldUsage {
				t.Fatal("rejected Office object left bytes", mode, usage, oldUsage, err)
			}
			if (mode == "count" || mode == "total" || mode == "hint_limit") && (reader.calls != 0 || objects.puts != 0) {
				t.Fatal("failed budget performed byte I/O", reader.calls, objects.puts)
			}
			if mode == "ooxml_invalid" && (objects.puts != 1 || objects.deletes != 1) {
				t.Fatal("invalid Office content was not compensated", objects.puts, objects.deletes)
			}
		})
	}
}
