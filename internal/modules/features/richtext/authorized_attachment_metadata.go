package richtext

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type AttachmentMutationAck struct {
	ThreadID     string `json:"thread_id"`
	AttachmentID string `json:"attachment_id,omitempty"`
}
type attachmentMetadataFacts struct {
	Core     *domain.Thread
	Article  *Article
	Bindings []AssetBindingAuthorityFact
	Selected json.RawMessage
	Object   *corestorage.Object
}
type attachmentMetadataPart struct {
	s                            *Service
	operation, assetID, id, name string
	order                        []string
	initial, result              *attachmentMetadataFacts
	ack                          AttachmentMutationAck
}

func (p *attachmentMetadataPart) ThreadType() domain.ThreadType { return domain.ThreadTypeArticle }
func canonicalAttachmentCore(t *domain.Thread) *domain.Thread {
	if t == nil {
		return nil
	}
	x := *t
	x.Tags = append([]string(nil), t.Tags...)
	x.ViewCount, x.ReplyCount, x.LikeCount = 0, 0, 0
	x.CreatedAt, x.UpdatedAt = t.CreatedAt.UTC(), t.CreatedAt.UTC()
	if t.DeletedAt != nil {
		at := t.DeletedAt.UTC()
		x.DeletedAt = &at
	}
	if t.ModerationAt != nil {
		at := t.ModerationAt.UTC()
		x.ModerationAt = &at
	}
	return &x
}
func (p *attachmentMetadataPart) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	if err := p.s.ensureEnabled(); err != nil {
		return nil, err
	}
	get := p.s.store.GetArticleByThreadID
	if lock {
		reader, ok := p.s.store.(ArticleAuthorityStore)
		if !ok || nilReadAdapter(reader) {
			return nil, communityport.ErrAuthorCommandUnavailable
		}
		get = reader.LockArticleAuthority
	}
	a, err := get(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.FactsVersion < 1 || a.CreatedBy != t.AuthorID || a.ThreadID != t.ID || a.Title != t.Title {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	rows, err := p.s.store.LoadArticleBindingAuthorityFacts(ctx, a.ID, lock)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxArticleAttachmentCount {
		return nil, ErrAttachmentLimit
	}
	f := &attachmentMetadataFacts{Core: canonicalAttachmentCore(t), Article: canonicalAuthorArticle(a), Bindings: rows}
	seen := map[string]bool{}
	for _, row := range rows {
		b, err := attachmentFromAuthorityFact(row)
		if err != nil {
			return nil, err
		}
		if b.ArticleContentID != a.ID || b.Asset.OwnerID != t.AuthorID || seen[b.ID] {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		seen[b.ID] = true
	}
	if p.operation == "bind" {
		f.Selected, err = p.s.store.LoadAssetAuthorityFacts(ctx, p.assetID, lock)
		if err != nil {
			return nil, err
		}
		asset, err := assetFromAuthorityFact(f.Selected)
		if err != nil {
			return nil, err
		}
		if asset.ID != p.assetID || asset.OwnerID != t.AuthorID || asset.Kind != AssetKindAttachment || asset.Status != AssetStatusActive {
			return nil, ErrPermissionDenied
		}
		var object corestorage.Object
		if lock {
			l, ok := p.s.objects.(corestorage.OwnedObjectCommandLocker)
			if !ok || nilReadAdapter(l) {
				return nil, ErrAssetUnavailable
			}
			object, err = l.LockOwnedObjectForCommand(ctx, asset.OwnerID, asset.StorageObjectID)
		} else {
			object, err = p.s.assetReadObject(ctx, asset)
		}
		if err != nil {
			return nil, err
		}
		if object.ID != asset.StorageObjectID || object.OwnerID != asset.OwnerID || object.Status != corestorage.ObjectStatusReady || object.Version < 1 || object.SizeBytes != asset.SizeBytes || object.MimeType != asset.MimeType || object.DeletedAt != nil {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		object = canonicalAttachmentObject(object)
		f.Object = &object
	}
	if lock && p.initial == nil {
		p.initial = f
	}
	return f, nil
}
func (p *attachmentMetadataPart) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
	if err := p.s.ensureEnabled(); err != nil {
		return err
	}
	if p.initial == nil {
		return identityport.ErrPolicyDecisionMismatch
	}
	switch p.operation {
	case "bind":
		a, err := p.s.AttachExisting(ctx, t.AuthorID, t.ID, p.assetID, p.name)
		if err != nil {
			return err
		}
		p.id = a.ID
	case "remove":
		if err := p.s.RemoveAttachment(ctx, t.AuthorID, t.ID, p.id); err != nil {
			return err
		}
	case "rename":
		if err := p.s.RenameAttachment(ctx, t.AuthorID, t.ID, p.id, p.name); err != nil {
			return err
		}
	case "reorder":
		if err := p.s.ReorderAttachments(ctx, t.AuthorID, t.ID, p.order); err != nil {
			return err
		}
	default:
		return ErrAssetInvalid
	}
	f, err := p.LoadAuthorFacts(ctx, t, true)
	if err != nil {
		return err
	}
	p.result = f.(*attachmentMetadataFacts)
	if err := p.checkMutation(); err != nil {
		return err
	}
	p.ack = AttachmentMutationAck{ThreadID: t.ID, AttachmentID: p.id}
	return nil
}
func metadataBindingRows(facts []AssetBindingAuthorityFact) (map[string]*ArticleAttachment, map[string]AssetBindingAuthorityFact, error) {
	items, rows := map[string]*ArticleAttachment{}, map[string]AssetBindingAuthorityFact{}
	for _, f := range facts {
		a, err := attachmentFromAuthorityFact(f)
		if err != nil {
			return nil, nil, err
		}
		if items[a.ID] != nil {
			return nil, nil, identityport.ErrPolicyFactsChanged
		}
		items[a.ID], rows[a.ID] = a, f
	}
	return items, rows, nil
}
func rawMetadataNonce(raw json.RawMessage) (int64, error) {
	fields, err := factRowFields(raw, nil)
	if err != nil {
		return 0, err
	}
	var n int64
	if json.Unmarshal(fields["facts_version"], &n) != nil || n < 1 {
		return 0, ErrAssetBindingFactsUnavailable
	}
	return n, nil
}
func (p *attachmentMetadataPart) checkMutation() error {
	if !reflect.DeepEqual(p.initial.Core, p.result.Core) || !reflect.DeepEqual(p.initial.Article, p.result.Article) || !reflect.DeepEqual(p.initial.Selected, p.result.Selected) || !reflect.DeepEqual(p.initial.Object, p.result.Object) {
		return identityport.ErrPolicyFactsChanged
	}
	before, br, err := metadataBindingRows(p.initial.Bindings)
	if err != nil {
		return err
	}
	after, ar, err := metadataBindingRows(p.result.Bindings)
	if err != nil {
		return err
	}
	count := len(before)
	if p.operation == "bind" {
		count++
	}
	if p.operation == "remove" {
		count--
		if before[p.id] == nil || after[p.id] != nil {
			return identityport.ErrPolicyFactsChanged
		}
	}
	if len(after) != count {
		return identityport.ErrPolicyFactsChanged
	}
	orders := map[string]int{}
	for i, id := range p.order {
		if orders[id] != 0 || before[id] == nil {
			return ErrAttachmentNotFound
		}
		orders[id] = i + 1
	}
	if p.operation == "reorder" && len(orders) != len(before) {
		return ErrAttachmentNotFound
	}
	for id, old := range before {
		if p.operation == "remove" && id == p.id {
			continue
		}
		current := after[id]
		if current == nil || !reflect.DeepEqual(br[id].Asset, ar[id].Asset) {
			return identityport.ErrPolicyFactsChanged
		}
		expected := cloneAttachment(old)
		changed := false
		if p.operation == "rename" && id == p.id {
			expected.DisplayName = strings.TrimSpace(p.name)
			changed = expected.DisplayName != old.DisplayName
		}
		if p.operation == "reorder" {
			expected.DisplayOrder = orders[id] - 1
			changed = expected.DisplayOrder != old.DisplayOrder
		}
		expected.UpdatedAt = current.UpdatedAt
		if !reflect.DeepEqual(expected, current) {
			return identityport.ErrPolicyFactsChanged
		}
		n, err := rawMetadataNonce(br[id].Binding)
		if err != nil {
			return err
		}
		next, err := rawMetadataNonce(ar[id].Binding)
		if err != nil {
			return err
		}
		if changed && next <= n || !changed && p.operation != "reorder" && next != n {
			return identityport.ErrPolicyFactsChanged
		}
	}
	if p.operation == "rename" && before[p.id] == nil {
		return ErrAttachmentNotFound
	}
	if p.operation == "bind" {
		a := after[p.id]
		asset, err := assetFromAuthorityFact(p.initial.Selected)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(p.name)
		if name == "" {
			name = asset.OriginalName
		}
		max := -1
		for _, item := range before {
			if item.DisplayOrder > max {
				max = item.DisplayOrder
			}
		}
		if a == nil || before[p.id] != nil || a.ArticleContentID != p.initial.Article.ID || a.AssetID != asset.ID || !reflect.DeepEqual(a.Asset, *asset) || a.DisplayName != name || a.DisplayOrder != max+1 {
			return identityport.ErrPolicyFactsChanged
		}
	}
	return nil
}
func (p *attachmentMetadataPart) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	f, err := p.LoadAuthorFacts(ctx, t, true)
	if err != nil {
		return err
	}
	if p.result == nil || !reflect.DeepEqual(f, p.result) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (p *attachmentMetadataPart) AuthorEventPayload() any { return p.ack }
func (s *Service) executeAttachmentMetadata(ctx context.Context, thread, operation, id, name string, ids []string, resolve identityport.SessionPrincipalResolver) (*AttachmentMutationAck, error) {
	c, ok := s.community.(communityport.AuthenticatedThreadAuthorCommands)
	if !ok || nilReadAdapter(c) || s.reliable == nil || nilReadAdapter(s.store) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if operation == "rename" && (strings.TrimSpace(name) == "" || len([]rune(strings.TrimSpace(name))) > 255 || strings.ContainsAny(name, "\r\n")) {
		return nil, ErrAttachmentContent
	}
	if operation == "reorder" && (len(ids) == 0 || len(ids) > MaxArticleAttachmentCount) {
		return nil, ErrAttachmentLimit
	}
	p := &attachmentMetadataPart{s: s, operation: operation, id: id, name: name, order: append([]string(nil), ids...)}
	if operation == "bind" {
		p.assetID, p.id = id, ""
	}
	if err := c.ExecuteThreadAuthorCommand(ctx, thread, "edit", "feature.richtext.attachment."+operation, "richtext.attachment.updated", resolve, p); err != nil {
		return nil, err
	}
	return &p.ack, nil
}
func (s *Service) AttachExistingForPrincipal(ctx context.Context, thread, asset, name string, p identityport.SessionPrincipalResolver) (*AttachmentMutationAck, error) {
	return s.executeAttachmentMetadata(ctx, thread, "bind", asset, name, nil, p)
}
func (s *Service) RemoveAttachmentForPrincipal(ctx context.Context, thread, id string, p identityport.SessionPrincipalResolver) error {
	_, err := s.executeAttachmentMetadata(ctx, thread, "remove", id, "", nil, p)
	return err
}
func (s *Service) RenameAttachmentForPrincipal(ctx context.Context, thread, id, name string, p identityport.SessionPrincipalResolver) (*AttachmentMutationAck, error) {
	return s.executeAttachmentMetadata(ctx, thread, "rename", id, name, nil, p)
}
func (s *Service) ReorderAttachmentsForPrincipal(ctx context.Context, thread string, ids []string, p identityport.SessionPrincipalResolver) (*AttachmentMutationAck, error) {
	return s.executeAttachmentMetadata(ctx, thread, "reorder", "", "", ids, p)
}
