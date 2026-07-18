package contentbody

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeSafeHTMLSanitizesActiveContent(t *testing.T) {
	result, err := Normalize(`<h2>台灯</h2><img src="/api/v1/content/assets/images/u/a.png" onerror="bad()"><script>bad()</script>`, FormatSafeHTML)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatSafeHTML || !strings.Contains(result.Content, "<h2>台灯</h2>") {
		t.Fatalf("unexpected normalized content: %#v", result)
	}
	if !strings.Contains(result.Content, `src="/api/v1/content/assets/images/u/a.png"`) {
		t.Fatalf("safe content image was removed: %s", result.Content)
	}
	if strings.Contains(result.Content, "script") || strings.Contains(result.Content, "onerror") {
		t.Fatalf("unsafe content survived: %s", result.Content)
	}
}

func TestNormalizeSafeHTMLRejectsExecutableImageSources(t *testing.T) {
	result, err := Normalize(`<p>图片说明</p><img src="data:text/html,<script>bad()</script>">`, FormatSafeHTML)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "src=") || strings.Contains(result.Content, "script") {
		t.Fatalf("executable image source survived: %s", result.Content)
	}
}

func TestNormalizePreservesLegacyPlainTextFormat(t *testing.T) {
	result, err := Normalize("  ordinary text  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatPlainText || result.Content != "ordinary text" {
		t.Fatalf("unexpected plain normalization: %#v", result)
	}
}

func TestNormalizeRejectsUnsupportedOrEmptyContent(t *testing.T) {
	if _, err := Normalize("body", "richtext_article"); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected unsupported format, got %v", err)
	}
	if _, err := Normalize("<script>bad()</script>", FormatSafeHTML); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("expected empty sanitized body, got %v", err)
	}
}
