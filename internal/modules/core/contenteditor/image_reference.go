package contenteditor

import (
	"strings"

	"golang.org/x/net/html"
)

// ReferencesImage matches an actual img source in stored sanitized HTML.
// Text, links, substrings and caller-supplied bodies are not image bindings.
func ReferencesImage(body, source string) bool {
	if source == "" {
		return false
	}
	z := html.NewTokenizer(strings.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if t.Data != "img" {
				continue
			}
			for _, a := range t.Attr {
				if a.Key == "src" && a.Val == source {
					return true
				}
			}
		}
	}
}
