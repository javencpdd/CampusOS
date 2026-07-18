package richtext

import (
	"fmt"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/contentbody"
)

const (
	MaxHTMLBytes = contentbody.MaxHTMLBytes
	MaxNodes     = contentbody.MaxNodes
	MaxDepth     = contentbody.MaxDepth
)

type SanitizeResult = contentbody.SanitizeResult

func Sanitize(input string) (SanitizeResult, error) {
	result, err := contentbody.Sanitize(input)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("%w: %v", ErrInvalidArticle, err)
	}
	return result, nil
}

func RenderArticleHTML(sanitized string) string {
	return contentbody.RenderHTML(strings.TrimSpace(sanitized))
}
