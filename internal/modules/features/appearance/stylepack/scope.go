package stylepack

import (
	"fmt"
	"strings"
)

const (
	TargetPersonalSpace = "personal-space"
	TargetHomepage      = "homepage"
	TargetWeb           = "web"
)

var targetRootSelectors = map[string]string{
	TargetPersonalSpace: ".public-space[data-campusos-space]",
	TargetHomepage:      ".home[data-campusos-home]",
	TargetWeb:           ".app-container[data-campusos-web]",
}

func ScopeRootSelector(target string) string {
	return targetRootSelectors[strings.TrimSpace(target)]
}

func ValidateCSSScope(target, input string) ValidationResult {
	var result ValidationResult
	prefix := ScopeRootSelector(target)
	if prefix == "" {
		result.addError("style pack target does not have a CSS scope")
		return result.finish()
	}
	validateCSSBlock(stripCSSComments(input), prefix, &result)
	return result.finish()
}

func validateCSSBlock(css, prefix string, result *ValidationResult) {
	for cursor := 0; cursor < len(css); {
		cursor = skipCSSSpace(css, cursor)
		if cursor >= len(css) {
			return
		}

		headerEnd, delimiter := findCSSHeaderEnd(css, cursor)
		if headerEnd < 0 {
			result.addError("css contains an incomplete rule")
			return
		}
		header := strings.TrimSpace(css[cursor:headerEnd])
		if delimiter == ';' {
			if strings.HasPrefix(header, "@") {
				result.addError("css top-level statements are not supported: " + header)
			}
			cursor = headerEnd + 1
			continue
		}

		blockEnd := findMatchingCSSBrace(css, headerEnd)
		if blockEnd < 0 {
			result.addError("css braces are not balanced")
			return
		}
		body := css[headerEnd+1 : blockEnd]
		lowerHeader := strings.ToLower(header)
		switch {
		case strings.HasPrefix(lowerHeader, "@media "),
			strings.HasPrefix(lowerHeader, "@supports "),
			strings.HasPrefix(lowerHeader, "@container "),
			strings.HasPrefix(lowerHeader, "@layer "):
			validateCSSBlock(body, prefix, result)
		case strings.HasPrefix(lowerHeader, "@keyframes "),
			strings.HasPrefix(lowerHeader, "@-webkit-keyframes "):
			// Keyframe step selectors cannot address document elements.
		case strings.HasPrefix(lowerHeader, "@"):
			result.addError("css at-rule is not supported: " + header)
		default:
			validateScopedSelectors(header, prefix, result)
		}
		cursor = blockEnd + 1
	}
}

func validateScopedSelectors(header, prefix string, result *ValidationResult) {
	for _, selector := range splitCSSSelectors(header) {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			result.addError("css contains an empty selector")
			continue
		}
		if !strings.HasPrefix(selector, prefix) {
			result.addError(fmt.Sprintf("css selector %q must start with %q", selector, prefix))
		}
	}
}

func splitCSSSelectors(value string) []string {
	var selectors []string
	start := 0
	parenDepth := 0
	bracketDepth := 0
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if parenDepth == 0 && bracketDepth == 0 {
				selectors = append(selectors, value[start:i])
				start = i + 1
			}
		}
	}
	return append(selectors, value[start:])
}

func findCSSHeaderEnd(css string, start int) (int, byte) {
	parenDepth := 0
	bracketDepth := 0
	quote := byte(0)
	for i := start; i < len(css); i++ {
		ch := css[i]
		if quote != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{', ';':
			if parenDepth == 0 && bracketDepth == 0 {
				return i, ch
			}
		}
	}
	return -1, 0
}

func findMatchingCSSBrace(css string, open int) int {
	depth := 0
	quote := byte(0)
	for i := open; i < len(css); i++ {
		ch := css[i]
		if quote != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func skipCSSSpace(css string, cursor int) int {
	for cursor < len(css) {
		switch css[cursor] {
		case ' ', '\n', '\r', '\t':
			cursor++
		default:
			return cursor
		}
	}
	return cursor
}

func stripCSSComments(css string) string {
	for {
		start := strings.Index(css, "/*")
		if start < 0 {
			return css
		}
		end := strings.Index(css[start+2:], "*/")
		if end < 0 {
			return css[:start]
		}
		end += start + 2
		css = css[:start] + css[end+2:]
	}
}
