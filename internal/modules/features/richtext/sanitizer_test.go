package richtext

import (
	"strings"
	"testing"
)

func TestSanitizeRemovesScriptAndEventAttributes(t *testing.T) {
	result, err := Sanitize(`<p>Hello</p><img src="/api/v1/richtext/assets/1/a.png" onerror="alert(1)"><script>alert(1)</script>`)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if strings.Contains(result.HTML, "script") {
		t.Fatalf("script should be removed: %s", result.HTML)
	}
	if strings.Contains(result.HTML, "onerror") {
		t.Fatalf("event handler should be removed: %s", result.HTML)
	}
	if !strings.Contains(result.HTML, `<img src="/api/v1/richtext/assets/1/a.png"`) {
		t.Fatalf("safe image src should remain: %s", result.HTML)
	}
}

func TestSanitizeRejectsEmptyBody(t *testing.T) {
	if _, err := Sanitize(`<script>alert(1)</script>`); err == nil {
		t.Fatalf("expected empty sanitized body to fail")
	}
}
