// Package contenteditor owns the pure, reusable content validation and
// rendering contract for CampusOS built-in modules. It has no Community,
// document repository, filesystem, or plugin dependency.
package contenteditor

import (
	"bytes"
	"encoding/json"
	"errors"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// FormatPlainText is kept as the historical Community wire value.
	FormatPlainText = "markdown"
	FormatSafeHTML  = "safe_html"
	FormatText      = "text"
	FormatMarkdown  = "markdown"
	FormatCampusDoc = "campusdoc"
	MaxHTMLBytes    = 100000
	MaxNodes        = 1000
	MaxDepth        = 48
)

var (
	ErrEmptyBody         = errors.New("content body is required")
	ErrUnsupportedFormat = errors.New("unsupported content format")
	ErrInvalidCampusDoc  = errors.New("invalid CampusDoc v1")
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
// The plain_text alias is accepted for clients but persists as markdown so
// existing Community readers remain byte-for-byte compatible.
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
		result, err := SanitizeHTML(input)
		if err != nil {
			return Normalized{}, err
		}
		return Normalized{Content: result.HTML, Text: result.Text, Format: FormatSafeHTML, Warnings: result.Warnings}, nil
	default:
		return Normalized{}, ErrUnsupportedFormat
	}
}

// RenderDocument is the content-only adapter used by Personal Documents.
// It never accepts HTML as Markdown and CampusDoc image blocks only preserve
// an Object ID; they cannot smuggle arbitrary image URLs into a preview.
func RenderDocument(format, input string) (SanitizeResult, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatText:
		if len(input) > MaxHTMLBytes {
			return SanitizeResult{}, errors.New("text document exceeds the size limit")
		}
		return SanitizeResult{HTML: plainTextDocumentHTML(input), Text: input}, nil
	case FormatMarkdown:
		return RenderMarkdown(input)
	case FormatCampusDoc:
		return RenderCampusDoc(input)
	default:
		return SanitizeResult{}, ErrUnsupportedFormat
	}
}

func plainTextDocumentHTML(input string) string {
	if input == "" {
		return "<p></p>"
	}
	paragraphs := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n\n")
	var out strings.Builder
	for _, paragraph := range paragraphs {
		out.WriteString("<p>")
		out.WriteString(strings.ReplaceAll(stdhtml.EscapeString(paragraph), "\n", "<br>"))
		out.WriteString("</p>")
	}
	return out.String()
}

func ValidateDocument(format, input string) error {
	_, err := RenderDocument(format, input)
	return err
}

// Sanitize is retained as a short name for feature adapters.
func Sanitize(input string) (SanitizeResult, error) { return SanitizeHTML(input) }

func SanitizeHTML(input string) (SanitizeResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SanitizeResult{}, ErrEmptyBody
	}
	if len(trimmed) > MaxHTMLBytes {
		return SanitizeResult{}, errors.New("content html exceeds the size limit")
	}
	nodes, err := xhtml.ParseFragment(strings.NewReader(trimmed), &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Div, Data: "div"})
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

// RenderMarkdown is deliberately conservative. Raw HTML is escaped first;
// only the small, documented block syntax below can become HTML.
func RenderMarkdown(input string) (SanitizeResult, error) {
	if len(input) > MaxHTMLBytes {
		return SanitizeResult{}, errors.New("markdown document exceeds the size limit")
	}
	if strings.TrimSpace(input) == "" {
		return SanitizeResult{}, nil
	}
	var out strings.Builder
	var paragraph []string
	listKind := ""
	inCode := false
	var code []string
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		out.WriteString("<p>")
		out.WriteString(strings.Join(paragraph, "<br>"))
		out.WriteString("</p>")
		paragraph = nil
	}
	closeList := func() {
		if listKind != "" {
			out.WriteString("</")
			out.WriteString(listKind)
			out.WriteString(">")
			listKind = ""
		}
	}
	flushCode := func() {
		if !inCode {
			return
		}
		out.WriteString("<pre><code>")
		out.WriteString(stdhtml.EscapeString(strings.Join(code, "\n")))
		out.WriteString("</code></pre>")
		code = nil
		inCode = false
	}
	for _, raw := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			flushParagraph()
			closeList()
			if inCode {
				flushCode()
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if trimmed == "" {
			flushParagraph()
			closeList()
			continue
		}
		if level, body, ok := markdownHeading(trimmed); ok {
			flushParagraph()
			closeList()
			out.WriteString("<h")
			out.WriteString(strconv.Itoa(level))
			out.WriteString(">")
			out.WriteString(stdhtml.EscapeString(body))
			out.WriteString("</h")
			out.WriteString(strconv.Itoa(level))
			out.WriteString(">")
			continue
		}
		if body, ok := markdownListItem(trimmed, "- "); ok {
			flushParagraph()
			if listKind != "ul" {
				closeList()
				listKind = "ul"
				out.WriteString("<ul>")
			}
			out.WriteString("<li>")
			out.WriteString(stdhtml.EscapeString(body))
			out.WriteString("</li>")
			continue
		}
		if body, ok := markdownNumberedItem(trimmed); ok {
			flushParagraph()
			if listKind != "ol" {
				closeList()
				listKind = "ol"
				out.WriteString("<ol>")
			}
			out.WriteString("<li>")
			out.WriteString(stdhtml.EscapeString(body))
			out.WriteString("</li>")
			continue
		}
		flushParagraph()
		closeList()
		if body, ok := strings.CutPrefix(trimmed, "> "); ok {
			out.WriteString("<blockquote><p>")
			out.WriteString(stdhtml.EscapeString(body))
			out.WriteString("</p></blockquote>")
			continue
		}
		paragraph = append(paragraph, stdhtml.EscapeString(line))
	}
	if inCode {
		flushCode()
	}
	flushParagraph()
	closeList()
	return SanitizeHTML(out.String())
}

func markdownHeading(value string) (int, string, bool) {
	level := 0
	for level < len(value) && level < 6 && value[level] == '#' {
		level++
	}
	if level == 0 || len(value) <= level || value[level] != ' ' {
		return 0, "", false
	}
	body := strings.TrimSpace(value[level+1:])
	return level, body, body != ""
}

func markdownListItem(value, prefix string) (string, bool) {
	body, ok := strings.CutPrefix(value, prefix)
	body = strings.TrimSpace(body)
	return body, ok && body != ""
}

func markdownNumberedItem(value string) (string, bool) {
	index := strings.Index(value, ". ")
	if index < 1 {
		return "", false
	}
	if _, err := strconv.Atoi(value[:index]); err != nil {
		return "", false
	}
	body := strings.TrimSpace(value[index+2:])
	return body, body != ""
}

// RenderCampusDoc validates the v1 block envelope. A non-empty legacy JSON
// object remains readable as escaped JSON so historic v0.14 drafts are not
// silently rejected; newly shaped documents must use version=1 and blocks.
func RenderCampusDoc(input string) (SanitizeResult, error) {
	if len(input) > MaxHTMLBytes {
		return SanitizeResult{}, errors.New("CampusDoc exceeds the size limit")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil || len(raw) == 0 {
		return SanitizeResult{}, ErrInvalidCampusDoc
	}
	versionRaw, hasVersion := raw["version"]
	blocksRaw, hasBlocks := raw["blocks"]
	if !hasVersion && !hasBlocks {
		pretty, err := json.Marshal(raw)
		if err != nil {
			return SanitizeResult{}, ErrInvalidCampusDoc
		}
		return SanitizeHTML("<pre><code>" + stdhtml.EscapeString(string(pretty)) + "</code></pre>")
	}
	if !hasVersion || !hasBlocks {
		return SanitizeResult{}, ErrInvalidCampusDoc
	}
	var version int
	if err := json.Unmarshal(versionRaw, &version); err != nil || version != 1 {
		return SanitizeResult{}, ErrInvalidCampusDoc
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(blocksRaw, &blocks); err != nil || len(blocks) == 0 || len(blocks) > 200 {
		return SanitizeResult{}, ErrInvalidCampusDoc
	}
	var out strings.Builder
	for _, block := range blocks {
		if err := renderCampusDocBlock(&out, block); err != nil {
			return SanitizeResult{}, err
		}
	}
	return SanitizeHTML(out.String())
}

func renderCampusDocBlock(out *strings.Builder, block map[string]json.RawMessage) error {
	var kind string
	if err := json.Unmarshal(block["type"], &kind); err != nil {
		return ErrInvalidCampusDoc
	}
	text := campusDocString(block, "text")
	switch kind {
	case "paragraph":
		if strings.TrimSpace(text) == "" {
			return ErrInvalidCampusDoc
		}
		out.WriteString("<p>" + stdhtml.EscapeString(text) + "</p>")
	case "heading":
		level := 2
		_ = json.Unmarshal(block["level"], &level)
		if level < 1 || level > 6 || strings.TrimSpace(text) == "" {
			return ErrInvalidCampusDoc
		}
		out.WriteString("<h" + strconv.Itoa(level) + ">" + stdhtml.EscapeString(text) + "</h" + strconv.Itoa(level) + ">")
	case "blockquote":
		if strings.TrimSpace(text) == "" {
			return ErrInvalidCampusDoc
		}
		out.WriteString("<blockquote><p>" + stdhtml.EscapeString(text) + "</p></blockquote>")
	case "code":
		if text == "" {
			return ErrInvalidCampusDoc
		}
		out.WriteString("<pre><code>" + stdhtml.EscapeString(text) + "</code></pre>")
	case "divider":
		out.WriteString("<hr>")
	case "bulleted_list", "ordered_list":
		var items []string
		if err := json.Unmarshal(block["items"], &items); err != nil || len(items) == 0 || len(items) > 100 {
			return ErrInvalidCampusDoc
		}
		tag := "ul"
		if kind == "ordered_list" {
			tag = "ol"
		}
		out.WriteString("<" + tag + ">")
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				return ErrInvalidCampusDoc
			}
			out.WriteString("<li>" + stdhtml.EscapeString(item) + "</li>")
		}
		out.WriteString("</" + tag + ">")
	case "table":
		var rows [][]string
		if err := json.Unmarshal(block["rows"], &rows); err != nil || len(rows) == 0 || len(rows) > 50 {
			return ErrInvalidCampusDoc
		}
		out.WriteString("<table><tbody>")
		for _, row := range rows {
			if len(row) == 0 || len(row) > 20 {
				return ErrInvalidCampusDoc
			}
			out.WriteString("<tr>")
			for _, cell := range row {
				out.WriteString("<td>" + stdhtml.EscapeString(cell) + "</td>")
			}
			out.WriteString("</tr>")
		}
		out.WriteString("</tbody></table>")
	case "image":
		objectID := campusDocString(block, "object_id")
		if !numericID(objectID) {
			return ErrInvalidCampusDoc
		}
		alt := campusDocString(block, "alt")
		out.WriteString("<figure data-object-id=\"" + objectID + "\"><figcaption>" + stdhtml.EscapeString(alt) + "</figcaption></figure>")
	default:
		return ErrInvalidCampusDoc
	}
	return nil
}

func campusDocString(block map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(block[key], &value)
	return value
}

func numericID(value string) bool {
	if len(value) == 0 || len(value) > 30 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
		out.WriteString("</" + tag + ">")
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		c.render(out, text, child, depth+1)
	}
	out.WriteString("</" + tag + ">")
	if tag == "p" || tag == "div" || tag == "section" || tag == "article" || strings.HasPrefix(tag, "h") || tag == "li" {
		text.WriteByte(' ')
	}
}

func allowedTag(tag string) bool {
	switch tag {
	case "article", "section", "div", "p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "strong", "em", "b", "i", "u", "s", "span", "small", "ul", "ol", "li", "blockquote", "pre", "code", "a", "img", "figure", "figcaption", "table", "thead", "tbody", "tr", "td", "th":
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
		key, value := strings.ToLower(strings.TrimSpace(attr.Key)), strings.TrimSpace(attr.Val)
		if attr.Namespace != "" || key == "" || strings.HasPrefix(key, "on") || key == "srcdoc" || len(value) > 2000 || containsDangerousValue(value) {
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
			if key == "alt" || key == "width" || key == "height" || (key == "loading" && (value == "lazy" || value == "eager")) {
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
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https":
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
