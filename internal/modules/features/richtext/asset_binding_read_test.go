package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestAssetBindingReadMemoryCommittedABACloses(t *testing.T) {
	ctx := context.Background()
	s := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, s)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	attachment, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "guide.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(*MemoryStore)
	objects := &richReadObjects{ObjectPort: s.objects}
	objects.afterOpen = func() {
		if err := store.UpdateAttachmentDisplayName(ctx, draft.Article.ID, attachment.ID, "changed.pdf"); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateAttachmentDisplayName(ctx, draft.Article.ID, attachment.ID, attachment.DisplayName); err != nil {
			t.Fatal(err)
		}
		// Reproduce a committed DB update that preserves its old ordinary
		// timestamp. All public DTO fields match; the managed nonce changed.
		store.mu.Lock()
		store.attachments[attachment.ID].UpdatedAt = attachment.UpdatedAt
		store.mu.Unlock()
	}
	s.objects = objects
	opened, err := s.OpenAttachmentForPrincipal(ctx, draft.ThreadID, attachment.ID, richReadProof("1001"))
	if !errors.Is(err, identityport.ErrPolicyFactsChanged) || opened.Reader != nil || objects.closes != 1 {
		t.Fatalf("committed binding ABA returned old byte reader: err=%v reader=%v closes=%d", err, opened.Reader != nil, objects.closes)
	}
}

func TestAssetBindingReadCodecAndUnavailableWholeEmpty(t *testing.T) {
	ctx := context.Background()
	store, _, _ := assetBindingMemoryFixture(t)
	raw, err := store.LoadAssetAuthorityFacts(ctx, "asset.a", false)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	// Actual PG bigint JSON must become decimal strings without float loss.
	fields["id"] = json.RawMessage("9007199254740993")
	fields["owner_user_id"] = json.RawMessage("9007199254740995")
	fields["storage_object_id"] = json.RawMessage("9007199254740997")
	large, _ := json.Marshal(fields)
	asset, err := assetFromAuthorityFact(large)
	if err != nil || asset.ID != "9007199254740993" || asset.OwnerID != "9007199254740995" || asset.StorageObjectID != "9007199254740997" {
		t.Fatal("bigint precision lost", asset, err)
	}
	for _, mode := range []string{"missing_nonce", "zero_nonce", "huge_nonce", "missing_owner", "bad_id", "null_id", "missing_date"} {
		bad := map[string]json.RawMessage{}
		for k, value := range fields {
			bad[k] = append(json.RawMessage(nil), value...)
		}
		switch mode {
		case "missing_nonce":
			delete(bad, "facts_version")
		case "zero_nonce":
			bad["facts_version"] = json.RawMessage("0")
		case "huge_nonce":
			bad["facts_version"] = json.RawMessage("9223372036854775808")
		case "missing_owner":
			delete(bad, "owner_user_id")
		case "bad_id":
			bad["id"] = json.RawMessage("1.1")
		case "null_id":
			bad["id"] = json.RawMessage("null")
		case "missing_date":
			delete(bad, "created_at")
		}
		payload, _ := json.Marshal(bad)
		if value, err := assetFromAuthorityFact(payload); value != nil || !errors.Is(err, ErrAssetBindingFactsUnavailable) {
			t.Fatal("malformed actual snapshot accepted", mode, value, err)
		}
	}
	for _, populated := range []bool{false, true} {
		s := newAttachmentTestService(t)
		draft := createAttachmentDraft(t, s)
		if populated {
			pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			if _, err := s.UploadAttachment(ctx, "1001", draft.ThreadID, "guide.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf)); err != nil {
				t.Fatal(err)
			}
		}
		actual := s.store.(*MemoryStore)
		actual.mu.Lock()
		actual.assetFacts = nil
		actual.mu.Unlock()
		if result, err := s.GetArticleForPrincipal(ctx, draft.ThreadID, richReadProof("1001")); result != nil || !errors.Is(err, ErrAssetBindingFactsUnavailable) {
			t.Fatal("article inventory ignored unavailable source", populated, result, err)
		}
		for _, owner := range []string{"1001", "1002"} {
			if page, err := s.UserAssetPageForPrincipal(ctx, "", "", 10, richReadProof(owner)); len(page.Items) != 0 || !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal("whole/empty owner page ignored unavailable source", populated, owner, page, err)
			}
		}
	}
	// No internal binding or nonce is added to the established DTO.
	wire, _ := json.Marshal(asset)
	var dto map[string]json.RawMessage
	json.Unmarshal(wire, &dto)
	if _, ok := dto["facts_version"]; ok {
		t.Fatal("nonce leaked on wire")
	}
	for _, id := range []string{"0", "-1", "1.1", "9223372036854775808"} {
		bad := map[string]json.RawMessage{"id": json.RawMessage(id)}
		if err := normalizeFactID(bad, "id"); err == nil {
			t.Fatal("invalid PG bigint accepted", strconv.Quote(id))
		}
	}
}
