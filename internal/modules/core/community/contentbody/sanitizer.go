// Package contentbody is the Community compatibility adapter for the shared
// core.content-editor contract. Community keeps its historical import path and
// wire formats while the sanitizer itself is owned by a dependency-neutral
// Core module.
package contentbody

import coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"

const (
	FormatPlainText = coreeditor.FormatPlainText
	FormatSafeHTML  = coreeditor.FormatSafeHTML
	MaxHTMLBytes    = coreeditor.MaxHTMLBytes
	MaxNodes        = coreeditor.MaxNodes
	MaxDepth        = coreeditor.MaxDepth
)

var (
	ErrEmptyBody         = coreeditor.ErrEmptyBody
	ErrUnsupportedFormat = coreeditor.ErrUnsupportedFormat
)

type SanitizeResult = coreeditor.SanitizeResult
type Normalized = coreeditor.Normalized

func Normalize(input, requestedFormat string) (Normalized, error) {
	return coreeditor.Normalize(input, requestedFormat)
}

func Sanitize(input string) (SanitizeResult, error) { return coreeditor.SanitizeHTML(input) }

func RenderHTML(sanitized string) string { return coreeditor.RenderHTML(sanitized) }
