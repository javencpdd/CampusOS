package safehtml

import (
	"strings"
	"testing"
)

func TestValidateAcceptsSafeSnippet(t *testing.T) {
	result := Validate(`<section class="profile" style="padding:16px;background:#fff"><h2>Hello</h2><p>CampusOS</p><a href="/threads">Threads</a><img src="https://example.test/a.png" alt="cover" loading="lazy"></section>`)
	if !result.Valid {
		t.Fatalf("expected valid html, got %#v", result.Errors)
	}
}

func TestValidateRejectsScriptAndEventHandlers(t *testing.T) {
	result := Validate(`<div onclick="alert(1)"><script>alert(1)</script></div>`)
	if result.Valid {
		t.Fatalf("expected invalid html")
	}
	if len(result.Errors) < 2 {
		t.Fatalf("expected script and event handler errors, got %#v", result.Errors)
	}
}

func TestValidateRejectsJavascriptURL(t *testing.T) {
	result := Validate(`<a href="javascript:alert(1)">bad</a>`)
	if result.Valid {
		t.Fatalf("expected invalid html")
	}
}

func TestValidateRejectsUnsafeStyle(t *testing.T) {
	result := Validate(`<div style="background:url(javascript:alert(1));position:fixed">bad</div>`)
	if result.Valid {
		t.Fatalf("expected invalid html")
	}
}

func TestValidateRejectsTooManyNodes(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < MaxNodes+1; i++ {
		builder.WriteString("<span>x</span>")
	}
	result := Validate(builder.String())
	if result.Valid {
		t.Fatalf("expected node limit validation failure")
	}
}
