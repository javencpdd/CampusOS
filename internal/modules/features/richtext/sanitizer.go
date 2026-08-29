package richtext

import (
	"fmt"
	"strings"

	coreeditor "github.com/campusos/CampusOS/internal/modules/core/contenteditor"
)

const (
	MaxHTMLBytes = coreeditor.MaxHTMLBytes
	MaxNodes     = coreeditor.MaxNodes
	MaxDepth     = coreeditor.MaxDepth
)

type SanitizeResult = coreeditor.SanitizeResult

func Sanitize(input string) (SanitizeResult, error) {
	result, err := coreeditor.SanitizeHTML(input)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("%w: %v", ErrInvalidArticle, err)
	}
	return result, nil
}

func RenderArticleHTML(sanitized string) string {
	return coreeditor.RenderHTML(strings.TrimSpace(sanitized))
}
