package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"io"
	"reflect"
	"strings"
)

type inlineImageAuthorityReader interface {
	LoadInlineImageAuthority(context.Context, string) (json.RawMessage, error)
}

func (s *PgStore) LoadInlineImageAuthority(ctx context.Context, id string) (json.RawMessage, error) {
	var raw []byte
	err := s.db(ctx).QueryRow(ctx, "SELECT to_jsonb(a) FROM richtext_article_assets a WHERE id=$1", id).Scan(&raw)
	return append(json.RawMessage(nil), raw...), err
}
func (s *MemoryStore) LoadInlineImageAuthority(_ context.Context, id string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.assets[id]
	if a == nil {
		return nil, ErrAssetNotFound
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["asset_id"] = nil
	return json.Marshal(fields)
}

type inlineImageFacts struct {
	Core    *domain.Thread
	Article *Article
}
type inlineImagePart struct {
	s         *Service
	articleID string
	file      *corestorage.InlineImageFile
	account   corestorage.InlineImageAccounting
	undo      func() error
	asset     *Asset
	raw       json.RawMessage
	expected  *inlineImageFacts
}

func (p *inlineImagePart) ThreadType() domain.ThreadType { return domain.ThreadTypeArticle }
func (p *inlineImagePart) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	if err := p.s.ensureEnabled(); err != nil {
		return nil, err
	}
	get := p.s.store.GetArticleByThreadID
	if lock {
		store, ok := p.s.store.(ArticleAuthorityStore)
		if !ok || nilReadAdapter(store) {
			return nil, communityport.ErrAuthorCommandUnavailable
		}
		get = store.LockArticleAuthority
	}
	a, err := get(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.FactsVersion < 1 || a.ThreadID != t.ID || a.CreatedBy != t.AuthorID || a.Title != t.Title || (p.articleID != "" && a.ID != p.articleID) {
		return nil, ErrAssetInvalid
	}
	f := &inlineImageFacts{Core: canonicalAttachmentCore(t), Article: canonicalAuthorArticle(a)}
	if p.expected != nil && !reflect.DeepEqual(p.expected, f) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if p.asset != nil {
		if err := p.checkStored(ctx); err != nil {
			return nil, err
		}
	}
	return f, nil
}
func (p *inlineImagePart) checkStored(ctx context.Context) error {
	if err := p.file.Check(ctx); err != nil {
		return err
	}
	reader, ok := p.s.store.(inlineImageAuthorityReader)
	if !ok || nilReadAdapter(reader) {
		return ErrAssetUnavailable
	}
	raw, err := reader.LoadInlineImageAuthority(ctx, p.asset.ID)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return identityport.ErrPolicyFactsChanged
	}
	if string(fields["asset_id"]) != "null" {
		return identityport.ErrPolicyFactsChanged
	}
	store, ok := p.s.store.(inlineImageStore)
	if !ok || nilReadAdapter(store) {
		return ErrAssetUnavailable
	}
	a, err := store.GetInlineImage(ctx, p.asset.UploaderID, p.asset.FileName)
	if a != nil {
		a = cloneAsset(a)
		a.CreatedAt = a.CreatedAt.UTC()
	}
	if err != nil || !reflect.DeepEqual(a, p.asset) {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	if p.raw != nil && !reflect.DeepEqual(p.raw, raw) {
		return identityport.ErrPolicyFactsChanged
	}
	p.raw = raw
	return nil
}
func (p *inlineImagePart) write(ctx context.Context, owner, thread, article string) error {
	var err error
	p.undo, err = p.account.AccountInlineImageForCommand(ctx, owner, p.file.Size())
	if err != nil {
		return err
	}
	if err = p.file.Write(ctx, p.s.assets.storage, owner, "richtext", p.s.assets.cfg.URLPrefix); err != nil {
		return err
	}
	image := p.file.Metadata()
	p.asset = &Asset{ID: fmt.Sprint(idgen.New()), ThreadID: thread, ArticleContentID: article, UploaderID: owner, FileURL: image.FileURL, FileName: image.FileName, FileSize: image.FileSize, MimeType: image.MimeType, Width: image.Width, Height: image.Height, CreatedAt: image.CreatedAt}
	if err = p.s.store.SaveAsset(ctx, p.asset); err != nil {
		return err
	}
	return p.checkStored(ctx)
}
func (p *inlineImagePart) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
	f, err := p.LoadAuthorFacts(ctx, t, true)
	if err != nil {
		return err
	}
	p.expected = f.(*inlineImageFacts)
	return p.write(ctx, t.AuthorID, t.ID, p.expected.Article.ID)
}
func (p *inlineImagePart) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	_, err := p.LoadAuthorFacts(ctx, t, true)
	return err
}
func (p *inlineImagePart) AuthorEventPayload() any {
	return map[string]string{"thread_id": p.asset.ThreadID, "asset_id": p.asset.ID}
}

func (s *Service) UploadAssetForPrincipal(ctx context.Context, reader io.Reader, threadID, articleID string, resolve identityport.SessionPrincipalResolver) (result *Asset, returnErr error) {
	account, ok := s.objects.(corestorage.InlineImageAccounting)
	_, storeOK := s.store.(inlineImageAuthorityReader)
	if !ok || nilReadAdapter(account) || !storeOK || s.reliable == nil || s.assets == nil || transaction.Active(ctx) {
		return nil, ErrAssetUnavailable
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	threadID, articleID = strings.TrimSpace(threadID), strings.TrimSpace(articleID)
	if threadID == "" && articleID != "" {
		return nil, ErrAssetInvalid
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil {
		return nil, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	if p.Actor.Kind != "user" || p.Audience != "user" {
		return nil, ErrPermissionDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if err := ctx.Err(); err != nil {
			return identityport.PrincipalContext{}, err
		}
		if err := s.ensureEnabled(); err != nil {
			return identityport.PrincipalContext{}, err
		}
		current, err := resolve(ctx)
		if err != nil {
			return current, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
		}
		if current.Validate() != nil || !reflect.DeepEqual(p, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	part := &inlineImagePart{s: s, articleID: articleID, account: account}
	var command communityport.AuthenticatedThreadAuthorCommands
	if threadID != "" {
		preflight, ok := s.community.(communityport.AuthenticatedThreadAuthorEditPreflight)
		command, storeOK = s.community.(communityport.AuthenticatedThreadAuthorCommands)
		if !ok || !storeOK || nilReadAdapter(preflight) || nilReadAdapter(command) {
			return nil, communityport.ErrAuthorCommandUnavailable
		}
		t, err := preflight.LoadThreadAuthorEditProof(ctx, threadID, bound)
		if err != nil {
			return nil, err
		}
		if t == nil || t.ThreadType != domain.ThreadTypeArticle || t.AuthorID != p.Actor.ID {
			return nil, ErrPermissionDenied
		}
		facts, err := part.LoadAuthorFacts(ctx, t, false)
		if err != nil {
			return nil, err
		}
		part.expected = facts.(*inlineImageFacts)
	}
	request := identityport.InlineImageUploadRequest{RequestID: fmt.Sprint(idgen.New()), PolicyVersion: identityport.InlineImageUploadPolicyVersion, Namespace: "richtext", OwnerID: p.Actor.ID, Principal: p}
	decision, err := (identitypolicy.InlineImageUploadPolicy{}).DecideInlineImageUpload(request)
	if err != nil || decision.Effect != "allow" {
		return nil, errors.Join(ErrPermissionDenied, err)
	}
	file, err := corestorage.PrepareInlineImage(ctx, reader, s.assets.MaxAssetBytes())
	if err != nil {
		return nil, richInlineImageError(err)
	}
	part.file = file
	published := false
	defer func() {
		if !published {
			var undo error
			if part.undo != nil {
				undo = part.undo()
			}
			returnErr = errors.Join(returnErr, file.Discard(), undo)
		}
	}()
	if command != nil {
		err = command.ExecuteThreadAuthorCommand(ctx, threadID, "edit", "feature.richtext.inline_image.upload", "richtext.inline_image.created", bound, part)
	} else {
		id := request.RequestID
		err = s.reliable.Execute(ctx, reliability.Command{Code: "feature.richtext.inline_image.upload", ActorID: p.Actor.ID, ActorType: "user", ResourceType: "inline_image", ResourceID: id, OperationCode: "feature.richtext.inline_image.upload", EventFactory: func() (reliability.Event, error) {
			return reliability.NewEvent("richtext.inline_image.created", "inline_image", id, map[string]string{"asset_id": part.asset.ID})
		}}, func(tx context.Context) error {
			current, err := bound(tx)
			if err != nil {
				return err
			}
			fresh := request
			fresh.Principal = current
			if err = identitypolicy.CheckInlineImageUploadDecision(request, decision, fresh); err != nil {
				return err
			}
			if err = part.write(tx, p.Actor.ID, "", ""); err != nil {
				return err
			}
			if _, err = bound(tx); err != nil {
				return err
			}
			if err = part.checkStored(tx); err != nil {
				return err
			}
			_, err = bound(tx)
			return err
		})
	}
	if err != nil {
		return nil, richInlineImageError(err)
	}
	published = true
	return cloneAsset(part.asset), nil
}
func richInlineImageError(err error) error {
	if errors.Is(err, corestorage.ErrImageTooLarge) {
		return errors.Join(ErrAssetTooLarge, err)
	}
	if errors.Is(err, corestorage.ErrImageUnsupported) {
		return errors.Join(ErrAssetUnsupported, err)
	}
	if errors.Is(err, corestorage.ErrObjectQuota) {
		return errors.Join(ErrAssetQuotaExceeded, err)
	}
	return err
}
