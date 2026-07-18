// Package contentbody owns the rendering contract shared by Community content
// features. It sanitizes user-authored HTML before the value reaches a Thread.
package contentbody

import (
	"bytes"
	"errors"
	stdhtml "html"
	"net/url"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	FormatPlainText = "markdown"
	FormatSafeHTML  = "safe_html"
	MaxHTMLBytes    = 100000
	MaxNodes        = 1000
	MaxDepth        = 48
)

var (
	ErrEmptyBody         = errors.New("content body is required")
	ErrUnsupportedFormat = errors.New("unsupported content format")
	whitespacePattern    = regexp.MustCompile(`\s+`)
)

type SanitizeResult struct {
	HTML     string   `json:"html"`
	Text     string   `json:"text"`
	Warnings []string `json:"warnings,omitempty"`
}

type Normalized struct {
	Content  string   `json:"content"`
	Text     string   `json:"text"`
	Format   string   `json:"content_format"`
	Warnings []string `json:"warnings,omitempty"`
}

// Normalize accepts the historical markdown value as the plain-text format.
// The plain_text alias is accepted for clients but is persisted as markdown so
// existing readers continue to behave exactly as before.
func Normalize(input, requestedFormat string) (Normalized, error) {
	format := strings.ToLower(strings.TrimSpace(requestedFormat))
	switch format {
	case "", "plain_text", FormatPlainText:
		content := strings.TrimSpace(input)
		if content == "" {
			return Normalized{}, ErrEmptyBody
		}
		return Normalized{Content: content, Text: content, Format: FormatPlainText}, nil
	case FormatSafeHTML:
		result, err := Sanitize(input)
		if err != nil {
			return Normalized{}, err
		}
		return Normalized{Content: result.HTML, Text: result.Text, Format: FormatSafeHTML, Warnings: result.Warnings}, nil
	default:
		return Normalized{}, ErrUnsupportedFormat
	}
}

func Sanitize(input string) (SanitizeResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SanitizeResult{}, ErrEmptyBody
	}
	if len(trimmed) > MaxHTMLBytes {
		return SanitizeResult{}, errors.New("content html exceeds the size limit")
	}
	nodes, err := xhtml.ParseFragment(strings.NewReader(trimmed), &xhtml.Node{
		Type:     xhtml.ElementNode,
		DataAtom: atom.Div,
		Data:     "div",
	})
	if err != nil {
		return SanitizeResult{}, errors.New("content html cannot be parsed")
	}
	ctx := &sanitizeContext{}
	var out bytes.Buffer
	var text strings.Builder
	for _, node := range nodes {
		ctx.render(&out, &text, node, 1)
	}
	if ctx.nodeLimitHit {
		return SanitizeResult{}, errors.New("content html contains too many nodes")
	}
	if ctx.depthLimitHit {
		return SanitizeResult{}, errors.New("content html nesting is too deep")
	}
	clean := strings.TrimSpace(out.String())
	plain := normalizePlainText(text.String())
	if plain == "" {
		return SanitizeResult{}, ErrEmptyBody
	}
	return SanitizeResult{HTML: clean, Text: plain, Warnings: ctx.warnings}, nil
}

func RenderHTML(sanitized string) string {
	return `<article class="article-content">` + strings.TrimSpace(sanitized) + `</article>`
}

func normalizePlainText(value string) string {
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
}

type sanitizeContext struct {
	nodes         int
	nodeLimitHit  bool
	depthLimitHit bool
	warnings      []string
}

func (c *sanitizeContext) render(out *bytes.Buffer, text *strings.Builder, node *xhtml.Node, depth int) {
	if node == nil || c.nodeLimitHit || c.depthLimitHit {
		return
	}
	if depth > MaxDepth {
		c.depthLimitHit = true
		return
	}
	c.nodes++
	if c.nodes > MaxNodes {
		c.nodeLimitHit = true
		return
	}

	switch node.Type {
	case xhtml.TextNode:
		out.WriteString(stdhtml.EscapeString(node.Data))
		text.WriteString(node.Data)
		text.WriteByte(' ')
	case xhtml.ElementNode:
		c.renderElement(out, text, node, depth)
	case xhtml.DocumentNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			c.render(out, text, child, depth+1)
		}
	case xhtml.CommentNode, xhtml.DoctypeNode:
		c.warnings = append(c.warnings, "removed unsupported html node")
	}
}

func (c *sanitizeContext) renderElement(out *bytes.Buffer, text *strings.Builder, node *xhtml.Node, depth int) {
	tag := strings.ToLower(strings.TrimSpace(node.Data))
	if dropElementWithChildren(tag) {
		c.warnings = append(c.warnings, "removed <"+tag+">")
		return
	}
	if !allowedTag(tag) {
		c.warnings = append(c.warnings, "unwrapped <"+tag+">")
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			c.render(out, text, child, depth+1)
		}
		return
	}

	out.WriteByte('<')
	out.WriteString(tag)
	for _, attr := range sanitizedAttributes(tag, node.Attr) {
		out.WriteByte(' ')
		out.WriteString(attr.Key)
		out.WriteString(`="`)
		out.WriteString(stdhtml.EscapeString(attr.Val))
		out.WriteByte('"')
	}
	out.WriteByte('>')
	if tag == "br" || tag == "hr" {
		text.WriteByte(' ')
		out.WriteString("</")
		out.WriteString(tag)
		out.WriteByte('>')
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		c.render(out, text, child, depth+1)
	}
	out.WriteString("</")
	out.WriteString(tag)
	out.WriteByte('>')
	if tag == "p" || tag == "div" || tag == "section" || tag == "article" || strings.HasPrefix(tag, "h") || tag == "li" {
		text.WriteByte(' ')
	}
}

func allowedTag(tag string) bool {
	switch tag {
	case "article", "section", "div", "p", "br", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"strong", "em", "b", "i", "u", "s", "span", "small",
		"ul", "ol", "li", "blockquote", "pre", "code",
		"a", "img", "figure", "figcaption",
		"table", "thead", "tbody", "tr", "td", "th":
		return true
	default:
		return false
	}
}

func dropElementWithChildren(tag string) bool {
	switch tag {
	case "script", "iframe", "object", "embed", "form", "input", "button", "style", "link", "meta", "svg", "math":
		return true
	default:
		return false
	}
}

func sanitizedAttributes(tag string, attrs []xhtml.Attribute) []xhtml.Attribute {
	clean := make([]xhtml.Attribute, 0, len(attrs))
	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		value := strings.TrimSpace(attr.Val)
		if attr.Namespace != "" || key == "" || strings.HasPrefix(key, "on") || key == "srcdoc" {
			continue
		}
		if len(value) > 2000 || containsDangerousValue(value) {
			continue
		}
		if key == "class" || key == "title" || key == "role" || strings.HasPrefix(key, "aria-") || strings.HasPrefix(key, "data-") {
			clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			continue
		}
		if key == "style" && safeStyle(value) {
			clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			continue
		}
		switch tag {
		case "a":
			if key == "href" && safeURL(value, true) {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			}
			if key == "target" && allowedTarget(value) {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
				if strings.EqualFold(value, "_blank") {
					clean = append(clean, xhtml.Attribute{Key: "rel", Val: "noopener noreferrer"})
				}
			}
		case "img":
			if key == "src" && safeURL(value, false) {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			}
			if key == "alt" || key == "width" || key == "height" {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			}
			if key == "loading" && (value == "lazy" || value == "eager") {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			}
		case "td", "th":
			if key == "colspan" || key == "rowspan" || key == "scope" {
				clean = append(clean, xhtml.Attribute{Key: key, Val: value})
			}
		}
	}
	return clean
}

func containsDangerousValue(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"<script", "</script", "javascript:", "vbscript:", "data:text/html", "\x00"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func safeURL(value string, allowMailto bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "//") {
		return false
	}
	for _, prefix := range []string{"javascript:", "vbscript:", "data:", "file:", "blob:"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" {
		return true
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return true
	case "mailto":
		return allowMailto
	default:
		return false
	}
}

func safeStyle(value string) bool {
	if len(value) > 500 {
		return false
	}
	lower := strings.ToLower(value)
	if strings.ContainsAny(lower, "<>") {
		return false
	}
	for _, marker := range []string{"url(", "expression(", "@import", "javascript:", "vbscript:", "data:", "behavior:", "-moz-binding"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	compact := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(lower)
	return !strings.Contains(compact, "position:fixed") && !strings.Contains(compact, "position:sticky")
}

func allowedTarget(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "_blank", "_self", "_parent", "_top":
		return true
	default:
		return false
	}
}
