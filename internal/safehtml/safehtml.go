package safehtml

import (
	"fmt"
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	MaxHTMLLength      = 20000
	MaxNodes           = 200
	MaxDepth           = 24
	MaxAttrValueLength = 2000
	MaxStyleLength     = 500
)

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type validator struct {
	errors       []string
	warnings     []string
	nodes        int
	nodeLimitHit bool
}

func Validate(input string) ValidationResult {
	trimmed := strings.TrimSpace(input)
	v := &validator{}
	if len(trimmed) > MaxHTMLLength {
		v.addError(fmt.Sprintf("html must not exceed %d bytes", MaxHTMLLength))
		return v.result()
	}
	if trimmed == "" {
		return ValidationResult{Valid: true}
	}

	nodes, err := xhtml.ParseFragment(strings.NewReader(trimmed), &xhtml.Node{
		Type:     xhtml.ElementNode,
		DataAtom: atom.Div,
		Data:     "div",
	})
	if err != nil {
		v.addError("html cannot be parsed")
		return v.result()
	}
	for _, node := range nodes {
		v.walk(node, 1)
	}
	return v.result()
}

func (v *validator) result() ValidationResult {
	return ValidationResult{
		Valid:    len(v.errors) == 0,
		Errors:   v.errors,
		Warnings: v.warnings,
	}
}

func (v *validator) walk(node *xhtml.Node, depth int) {
	if node == nil {
		return
	}
	if depth > MaxDepth {
		v.addError(fmt.Sprintf("html nesting depth must not exceed %d", MaxDepth))
		return
	}
	v.nodes++
	if v.nodes > MaxNodes {
		if !v.nodeLimitHit {
			v.nodeLimitHit = true
			v.addError(fmt.Sprintf("html must not contain more than %d nodes", MaxNodes))
		}
		return
	}

	switch node.Type {
	case xhtml.ElementNode:
		v.validateElement(node)
	case xhtml.CommentNode:
		v.addError("html comments are not allowed")
	case xhtml.DoctypeNode:
		v.addError("doctype is not allowed")
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		v.walk(child, depth+1)
	}
}

func (v *validator) validateElement(node *xhtml.Node) {
	tag := strings.ToLower(strings.TrimSpace(node.Data))
	if !allowedTag(tag) {
		v.addError(fmt.Sprintf("tag <%s> is not allowed", tag))
		return
	}
	for _, attr := range node.Attr {
		v.validateAttribute(tag, attr)
	}
}

func (v *validator) validateAttribute(tag string, attr xhtml.Attribute) {
	key := strings.ToLower(strings.TrimSpace(attr.Key))
	value := strings.TrimSpace(attr.Val)
	if attr.Namespace != "" {
		v.addError(fmt.Sprintf("attribute %q uses an unsupported namespace", attr.Key))
		return
	}
	if key == "" {
		v.addError("empty attribute names are not allowed")
		return
	}
	if strings.HasPrefix(key, "on") || key == "srcdoc" {
		v.addError(fmt.Sprintf("attribute %q is not allowed", attr.Key))
		return
	}
	if len(value) > MaxAttrValueLength {
		v.addError(fmt.Sprintf("attribute %q must not exceed %d bytes", attr.Key, MaxAttrValueLength))
		return
	}
	if !allowedAttribute(tag, key) {
		v.addError(fmt.Sprintf("attribute %q is not allowed on <%s>", attr.Key, tag))
		return
	}
	if containsDangerousAttributeValue(value) {
		v.addError(fmt.Sprintf("attribute %q contains unsafe content", attr.Key))
		return
	}

	switch key {
	case "href":
		if !safeURL(value, true) {
			v.addError("href must be http, https, mailto, anchor or relative URL")
		}
	case "src":
		if !safeURL(value, false) {
			v.addError("src must be http, https, anchor or relative URL")
		}
	case "style":
		if !safeStyle(value) {
			v.addError("style contains unsafe CSS")
		}
	case "target":
		if !allowedTarget(value) {
			v.addError("target has an unsupported value")
		}
	case "loading":
		if value != "" && value != "lazy" && value != "eager" {
			v.addError("loading must be lazy or eager")
		}
	}
}

func allowedTag(tag string) bool {
	switch tag {
	case "section", "div", "header", "footer", "main", "article", "aside", "nav",
		"h1", "h2", "h3", "h4", "h5", "h6", "p", "span", "strong", "em", "b", "i", "u", "small",
		"br", "hr", "ul", "ol", "li", "blockquote", "pre", "code", "a", "img", "figure", "figcaption",
		"table", "thead", "tbody", "tr", "th", "td":
		return true
	default:
		return false
	}
}

func allowedAttribute(tag, key string) bool {
	if key == "class" || key == "id" || key == "title" || key == "role" || key == "style" {
		return true
	}
	if strings.HasPrefix(key, "aria-") || strings.HasPrefix(key, "data-") {
		return true
	}
	switch tag {
	case "a":
		return key == "href" || key == "target" || key == "rel"
	case "img":
		return key == "src" || key == "alt" || key == "width" || key == "height" || key == "loading"
	case "th", "td":
		return key == "colspan" || key == "rowspan" || key == "scope"
	default:
		return false
	}
}

func containsDangerousAttributeValue(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{
		"<script",
		"</script",
		"javascript:",
		"vbscript:",
		"data:text/html",
		"\x00",
	}
	for _, marker := range markers {
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
	blockedPrefixes := []string{"javascript:", "vbscript:", "data:", "file:", "blob:"}
	for _, prefix := range blockedPrefixes {
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
	if len(value) > MaxStyleLength {
		return false
	}
	lower := strings.ToLower(value)
	if strings.ContainsAny(lower, "<>") {
		return false
	}
	blocked := []string{
		"url(",
		"expression(",
		"@import",
		"javascript:",
		"vbscript:",
		"data:",
		"behavior:",
		"-moz-binding",
	}
	for _, marker := range blocked {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	compact := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(lower)
	if strings.Contains(compact, "position:fixed") || strings.Contains(compact, "position:sticky") {
		return false
	}
	return true
}

func allowedTarget(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "_blank", "_self", "_parent", "_top":
		return true
	default:
		return false
	}
}

func (v *validator) addError(message string) {
	v.errors = append(v.errors, message)
}
