package personaldocuments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// DocumentPrincipalResolver is supplied by trusted ingress, never by a wire DTO.
type DocumentPrincipalResolver = identityport.SessionPrincipalResolver

func nilDocumentAdapter(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func documentPrincipal(ctx context.Context, resolve DocumentPrincipalResolver) (identityport.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	if resolve == nil {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	p, err := resolve(ctx)
	if err != nil {
		return identityport.PrincipalContext{}, err
	}
	if p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	return p, nil
}

func documentRevision(d DocumentDetail) (string, error) {
	if d.ID == "" || d.OwnerID == "" || d.Version < 1 || d.CurrentVersionID == "" || d.CurrentVersion == nil ||
		d.CurrentVersion.ID != d.CurrentVersionID || d.CurrentVersion.DocumentID != d.ID ||
		d.CurrentVersion.VersionNumber < 1 || d.CurrentVersion.SourceObjectID == "" ||
		d.CurrentVersion.SHA256 == "" || d.CurrentVersion.SizeBytes < 0 || !supported(d.Format) ||
		d.CurrentVersion.Format != d.Format || (d.Status != StatusActive && d.Status != StatusTrashed) ||
		(d.Status == StatusActive && d.DeletedAt != nil) || (d.Status == StatusTrashed && d.DeletedAt == nil) {
		return "", identityport.ErrPolicyDecisionMismatch
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func documentReadRequest(d DocumentDetail, p identityport.PrincipalContext) (identityport.ResourcePolicyRequest, error) {
	version, err := documentRevision(d)
	if err != nil {
		return identityport.ResourcePolicyRequest{}, err
	}
	status := "active"
	if d.Status == StatusTrashed {
		status = "deleted"
	}
	r := identityport.ResourcePolicyRequest{
		Contract: identityport.ResourcePolicyContract, RequestID: fmt.Sprint(idgen.New()),
		Principal: p, Action: "personal.document.read", PolicyVersion: "personal.document.read/v1",
		Facts:  identityport.ResourcePolicyFacts{Kind: "document", ID: d.ID, OwnerID: d.OwnerID, Status: status, Version: version},
		Grants: []identityport.ResourcePolicyGrant{}, EvaluatedAtMS: time.Now().UnixMilli(),
	}
	return r, r.Validate()
}

// Metadata for owned tombstones is a separate lifecycle projection. It cannot
// authorize original bytes or reveal their storage/version references.
func documentMetadata(d DocumentDetail, p identityport.PrincipalContext) (DocumentDetail, error) {
	if _, err := documentRevision(d); err != nil {
		return DocumentDetail{}, err
	}
	if d.OwnerID != p.Actor.ID {
		return DocumentDetail{}, identityport.ErrPolicyDecisionMismatch
	}
	if d.Status == StatusActive {
		r, err := documentReadRequest(d, p)
		if err != nil {
			return DocumentDetail{}, err
		}
		decision, err := (identitypolicy.ResourcePolicy{}).DecideResource(r)
		if err != nil || decision.Effect != "allow" {
			return DocumentDetail{}, errors.Join(ErrNotFound, err)
		}
		if err := identitypolicy.CheckResourceDecision(r, decision, r); err != nil {
			return DocumentDetail{}, err
		}
	} else {
		d.CurrentVersion = nil
		d.CurrentVersionID = ""
	}
	return d, nil
}

func (s *Service) checkDocumentRead(ctx context.Context, resolve DocumentPrincipalResolver, initial DocumentDetail, p identityport.PrincipalContext, request identityport.ResourcePolicyRequest, decision identityport.ResourcePolicyDecision) error {
	if err := s.enabledError(); err != nil {
		return err
	}
	currentP, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, currentP) {
		return identityport.ErrPolicyFactsChanged
	}
	current, err := s.repository.Get(ctx, p.Actor.ID, initial.ID)
	if err != nil {
		return s.translateError(err)
	}
	currentRequest, err := documentReadRequest(current, currentP)
	if err != nil {
		return err
	}
	currentRequest.RequestID = request.RequestID
	if err := identitypolicy.CheckResourceDecision(request, decision, currentRequest); err != nil {
		return err
	}
	finalP, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, finalP) {
		return identityport.ErrPolicyFactsChanged
	}
	return ctx.Err()
}

func (s *Service) GetForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id string) (DocumentDetail, error) {
	p, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return DocumentDetail{}, err
	}
	d, err := s.Get(ctx, p.Actor.ID, id)
	if err != nil {
		return DocumentDetail{}, err
	}
	out, err := documentMetadata(d, p)
	if err != nil {
		return DocumentDetail{}, err
	}
	current, err := s.repository.Get(ctx, p.Actor.ID, id)
	if err != nil {
		return DocumentDetail{}, s.translateError(err)
	}
	if !reflect.DeepEqual(d, current) {
		return DocumentDetail{}, identityport.ErrPolicyFactsChanged
	}
	finalP, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return DocumentDetail{}, err
	}
	if !reflect.DeepEqual(p, finalP) {
		return DocumentDetail{}, identityport.ErrPolicyFactsChanged
	}
	if err := s.enabledError(); err != nil {
		return DocumentDetail{}, err
	}
	return out, ctx.Err()
}

func (s *Service) ListForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, status string) ([]DocumentDetail, error) {
	p, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	rows, err := s.List(ctx, p.Actor.ID, status)
	if err != nil {
		return nil, err
	}
	out := make([]DocumentDetail, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, d := range rows {
		if seen[d.ID] || (status != "" && d.Status != status) {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		seen[d.ID] = true
		projected, err := documentMetadata(d, p)
		if err != nil {
			return nil, err
		}
		out = append(out, projected)
	}
	current, err := s.repository.List(ctx, p.Actor.ID, ListFilter{Status: status})
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(rows, current) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	finalP, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(p, finalP) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if err := s.enabledError(); err != nil {
		return nil, err
	}
	return out, ctx.Err()
}

// authorizeDocumentRead binds all projections to both the document metadata
// and the immutable selected version. No stale body is returned on a conflict.
func (s *Service) authorizeDocumentRead(ctx context.Context, resolve DocumentPrincipalResolver, id, versionID string, read func(string, DocumentDetail, DocumentVersion) error) error {
	p, err := documentPrincipal(ctx, resolve)
	if err != nil {
		return err
	}
	d, err := s.Get(ctx, p.Actor.ID, id)
	if err != nil {
		return err
	}
	r, err := documentReadRequest(d, p)
	if err != nil {
		return err
	}
	decision, err := (identitypolicy.ResourcePolicy{}).DecideResource(r)
	if err != nil {
		return err
	}
	if decision.Effect != "allow" {
		return s.translateError(ErrNotFound)
	}
	selected := *d.CurrentVersion
	if versionID != "" {
		selected, err = s.repository.Version(ctx, p.Actor.ID, id, versionID)
		if err != nil {
			return s.translateError(err)
		}
		if selected.DocumentID != id || selected.ID != versionID || selected.SourceObjectID == "" || selected.SHA256 == "" || selected.VersionNumber < 1 || selected.SizeBytes < 0 || selected.Format != d.Format {
			return identityport.ErrPolicyDecisionMismatch
		}
	}
	object, err := s.objects.Stat(ctx, p.Actor.ID, selected.SourceObjectID)
	if err != nil {
		return s.objectError(err, limitFor(d.Format), p.Actor.ID, selected.SizeBytes)
	}
	if err := documentObjectMatches(object, p.Actor.ID, selected); err != nil {
		return err
	}
	if err := read(p.Actor.ID, d, selected); err != nil {
		return err
	}
	currentVersion, err := s.repository.Version(ctx, p.Actor.ID, id, selected.ID)
	if err != nil {
		return s.translateError(err)
	}
	if !reflect.DeepEqual(selected, currentVersion) {
		return identityport.ErrPolicyFactsChanged
	}
	currentObject, err := s.objects.Stat(ctx, p.Actor.ID, selected.SourceObjectID)
	if err != nil {
		return s.objectError(err, limitFor(d.Format), p.Actor.ID, selected.SizeBytes)
	}
	if !reflect.DeepEqual(object, currentObject) {
		return identityport.ErrPolicyFactsChanged
	}
	return s.checkDocumentRead(ctx, resolve, d, p, r, decision)
}

func documentObjectMatches(o corestorage.Object, owner string, v DocumentVersion) error {
	if o.ID != v.SourceObjectID || o.OwnerID != owner || o.Namespace != "personal-documents" ||
		o.Purpose != "source."+v.Format || o.Status != corestorage.ObjectStatusReady || o.DeletedAt != nil ||
		o.Version < 1 || o.SHA256 != v.SHA256 || o.SizeBytes != v.SizeBytes {
		return identityport.ErrPolicyDecisionMismatch
	}
	return nil
}

func (s *Service) OpenVersionForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id, versionID string) (DocumentDetail, corestorage.ObjectReader, error) {
	var d DocumentDetail
	var object corestorage.ObjectReader
	err := s.authorizeDocumentRead(ctx, resolve, id, versionID, func(owner string, expected DocumentDetail, selected DocumentVersion) (err error) {
		d, object, err = s.OpenVersion(ctx, owner, id, versionID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(d, expected) || object.Reader == nil {
			return identityport.ErrPolicyFactsChanged
		}
		return documentObjectMatches(object.Object, owner, selected)
	})
	if err != nil {
		if object.Reader != nil {
			_ = object.Reader.Close()
		}
		return DocumentDetail{}, corestorage.ObjectReader{}, err
	}
	return d, object, nil
}

func (s *Service) TextContentForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id, versionID string) (DocumentDetail, string, error) {
	var d DocumentDetail
	var content string
	err := s.authorizeDocumentRead(ctx, resolve, id, versionID, func(owner string, expected DocumentDetail, selected DocumentVersion) (err error) {
		d, content, err = s.TextContentVersion(ctx, owner, id, versionID)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(content))
		if !reflect.DeepEqual(d, expected) || int64(len([]byte(content))) != selected.SizeBytes || hex.EncodeToString(digest[:]) != selected.SHA256 {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
	if err != nil {
		return DocumentDetail{}, "", err
	}
	return d, content, nil
}

func (s *Service) PreviewForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id, versionID string) (PreviewStatus, error) {
	var preview PreviewStatus
	err := s.authorizeDocumentRead(ctx, resolve, id, versionID, func(owner string, expected DocumentDetail, selected DocumentVersion) (err error) {
		preview = PreviewStatus{Document: expected, Status: "converter_unavailable", DownloadAvailable: true, Message: "当前部署未启用满足隔离要求的文档转换服务；原文件已安全保存，可下载后查看。"}
		if !editable(expected.Format) {
			return nil
		}
		d, content, err := s.TextContentVersion(ctx, owner, id, versionID)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(content))
		if !reflect.DeepEqual(d, expected) || int64(len([]byte(content))) != selected.SizeBytes || hex.EncodeToString(digest[:]) != selected.SHA256 {
			return identityport.ErrPolicyFactsChanged
		}
		rendered, err := coreeditor.RenderDocument(expected.Format, content)
		if err != nil {
			return err
		}
		preview.Status, preview.Message, preview.RenderedHTML, preview.Warnings = "native", "此格式可在“我的文档”中安全编辑和查看。", rendered.HTML, rendered.Warnings
		return nil
	})
	if err != nil {
		return PreviewStatus{}, err
	}
	return preview, nil
}

func (s *Service) VersionsForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id string) ([]DocumentVersion, error) {
	var versions []DocumentVersion
	err := s.authorizeDocumentRead(ctx, resolve, id, "", func(owner string, expected DocumentDetail, selected DocumentVersion) (err error) {
		versions, err = s.Versions(ctx, owner, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func (s *Service) PDFInvocationForPrincipal(ctx context.Context, resolve DocumentPrincipalResolver, id, presentation string) (PDFPreviewInvocation, error) {
	var result PDFPreviewInvocation
	err := s.authorizeDocumentRead(ctx, resolve, id, "", func(owner string, expected DocumentDetail, selected DocumentVersion) (err error) {
		result, err = s.CreatePDFInvocation(identityport.WithSessionPrincipalResolver(ctx, resolve), owner, id, presentation)
		return err
	})
	if err != nil {
		return PDFPreviewInvocation{}, err
	}
	return result, nil
}
