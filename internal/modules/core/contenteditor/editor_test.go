package contenteditor

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeHTMLContractRemainsControlled(t *testing.T) {
	result, err := SanitizeHTML(`<h2>公告</h2><img src="/api/v1/content/assets/a.png" onerror="bad()"><script>bad()</script>`)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if !strings.Contains(result.HTML, `<h2>公告</h2>`) || !strings.Contains(result.HTML, `src="/api/v1/content/assets/a.png"`) {
		t.Fatalf("expected allowed markup, got %s", result.HTML)
	}
	if strings.Contains(result.HTML, "script") || strings.Contains(result.HTML, "onerror") {
		t.Fatalf("unsafe html survived: %s", result.HTML)
	}
}

func TestRenderMarkdownEscapesRawHTML(t *testing.T) {
	result, err := RenderMarkdown("# 标题\n\n<script>alert(1)</script>\n\n- 第一项\n- 第二项")
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	for _, expected := range []string{"<h1>标题</h1>", "<ul>", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(result.HTML, expected) {
			t.Fatalf("markdown output missing %q: %s", expected, result.HTML)
		}
	}
}

func TestRenderTextEscapesMarkupForServerPreview(t *testing.T) {
	result, err := RenderDocument(FormatText, "<img src=x onerror=bad()>\n第二行")
	if err != nil {
		t.Fatalf("render text: %v", err)
	}
	if strings.Contains(result.HTML, "<img") || !strings.Contains(result.HTML, "&lt;img") || !strings.Contains(result.HTML, "<br>") {
		t.Fatalf("plain text preview must be escaped html: %s", result.HTML)
	}
}

func TestRenderCampusDocV1RejectsExternalImageURL(t *testing.T) {
	valid := `{"version":1,"blocks":[{"type":"paragraph","text":"校园文档"},{"type":"image","object_id":"123456","alt":"私有图片"}]}`
	result, err := RenderCampusDoc(valid)
	if err != nil {
		t.Fatalf("render valid CampusDoc: %v", err)
	}
	if !strings.Contains(result.HTML, `data-object-id="123456"`) || strings.Contains(result.HTML, "src=") {
		t.Fatalf("CampusDoc image must only retain object id: %s", result.HTML)
	}
	invalid := `{"version":1,"blocks":[{"type":"image","object_id":"https://example.invalid/image.png"}]}`
	if _, err := RenderCampusDoc(invalid); !errors.Is(err, ErrInvalidCampusDoc) {
		t.Fatalf("expected invalid CampusDoc, got %v", err)
	}
}

func TestRenderCampusDocKeepsLegacyJSONReadable(t *testing.T) {
	result, err := RenderCampusDoc(`{"legacy":"draft"}`)
	if err != nil {
		t.Fatalf("render legacy document: %v", err)
	}
	if !strings.Contains(result.HTML, "legacy") {
		t.Fatalf("legacy payload was not preserved: %s", result.HTML)
	}
}
