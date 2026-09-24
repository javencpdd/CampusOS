// Package pluginui owns short-lived, authenticated host resource contexts.
// Resource resolvers and policy checks are supplied by host composition, never
// by a plugin. Persistence uses the existing platform invocation table.
package pluginui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"
)

var (
	ErrNotFound     = errors.New("plugin ui invocation was not found")
	ErrExpired      = errors.New("plugin ui invocation has expired")
	ErrPresentation = errors.New("plugin ui invocation presentation is invalid")
)

type Invocation struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	PluginKey          string     `json:"plugin_key"`
	SurfaceID          string     `json:"surface_id"`
	ContextKind        string     `json:"context_kind"`
	ArticleContentID   string     `json:"article_content_id"`
	AssetID            string     `json:"asset_id,omitempty"`
	AttachmentID       string     `json:"attachment_id,omitempty"`
	PersonalDocumentID string     `json:"personal_document_id,omitempty"`
	Presentation       string     `json:"presentation"`
	Purpose            string     `json:"purpose"`
	ContextDigest      string     `json:"-"`
	ExpiresAt          time.Time  `json:"expires_at"`
	OpenedAt           *time.Time `json:"opened_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type Store interface {
	CreateInvocation(context.Context, *Invocation) error
	GetInvocation(context.Context, string) (*Invocation, error)
	MarkInvocationOpened(context.Context, string, string) error
}

type Service struct {
	Store Store
	// Resolve checks the resource's current owner, binding and business state.
	Resolve func(context.Context, string, *Invocation) (string, error)
	// Surface returns an immutable active plugin-version fingerprint.
	Surface   func(context.Context, *Invocation) (string, error)
	Authorize func(context.Context, *Invocation, string) error
}

func NewID() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(9223372036854775807))
	if err != nil {
		return "", err
	}
	return value.Add(value, big.NewInt(1)).String(), nil
}

func validBinding(v *Invocation) bool {
	switch v.ContextKind {
	case "article_attachment":
		return v.ArticleContentID != "" && v.AssetID != "" && v.AttachmentID != "" && v.PersonalDocumentID == ""
	case "personal_asset":
		return v.ArticleContentID == "" && v.AssetID != "" && v.AttachmentID == "" && v.PersonalDocumentID == ""
	case "personal_document":
		return v.ArticleContentID == "" && v.AssetID == "" && v.AttachmentID == "" && v.PersonalDocumentID != ""
	}
	return false
}

func (s Service) check(ctx context.Context, v *Invocation, operation string) (string, error) {
	if s.Store == nil || s.Resolve == nil || s.Surface == nil || s.Authorize == nil || v.UserID == "" || v.PluginKey == "" || v.SurfaceID == "" || !validBinding(v) {
		return "", ErrNotFound
	}
	switch v.Presentation {
	case "modal", "drawer", "fullscreen", "new-tab":
	default:
		return "", ErrPresentation
	}
	digest, err := s.Surface(ctx, v)
	if err != nil {
		return "", err
	}
	resourceDigest, err := s.Resolve(ctx, v.UserID, v)
	if err != nil {
		return "", err
	}
	combined := sha256.Sum256([]byte(digest + ":" + resourceDigest))
	digest = fmt.Sprintf("%x", combined[:])
	if operation != "invocation.create" && digest != v.ContextDigest {
		return "", ErrExpired
	}
	if err := s.Authorize(ctx, v, operation); err != nil {
		return "", err
	}
	return digest, nil
}

func (s Service) Issue(ctx context.Context, binding Invocation) (*Invocation, error) {
	// Purpose is host-defined, not arbitrary plugin input.
	binding.Purpose = binding.ContextKind + "_pdf_preview"
	digest, err := s.check(ctx, &binding, "invocation.create")
	if err != nil {
		return nil, err
	}
	id, err := NewID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	binding.ID, binding.ContextDigest, binding.CreatedAt = id, digest, now
	binding.ExpiresAt = now.Add(10 * time.Minute)
	binding.OpenedAt, binding.RevokedAt = nil, nil
	if err := s.Store.CreateInvocation(ctx, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s Service) Open(ctx context.Context, user, id string) (*Invocation, error) {
	if user == "" || s.Store == nil {
		return nil, ErrNotFound
	}
	v, err := s.Store.GetInvocation(ctx, id)
	if err != nil {
		return nil, err
	}
	if v == nil || v.UserID != user {
		return nil, ErrNotFound
	}
	if v.RevokedAt != nil || !v.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrExpired
	}
	if _, err := s.check(ctx, v, "content.read"); err != nil {
		return nil, err
	}
	if err := s.Store.MarkInvocationOpened(ctx, id, user); err != nil {
		return nil, err
	}
	return v, nil
}
