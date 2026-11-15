package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxRuntimeDocumentBytes = 256 << 10

var runtimeKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type RuntimeSyntaxError struct{ Line int }

func (e *RuntimeSyntaxError) Error() string {
	return fmt.Sprintf("配置文件第 %d 行格式不正确，请使用单行 KEY=value，避免重复项和变量插值", e.Line)
}

// ParseRuntimeText validates the complete fixed deployment document without
// evaluating shell commands, variable substitution, or loading process env.
func ParseRuntimeText(raw []byte) (map[string]string, error) {
	if len(raw) > MaxRuntimeDocumentBytes || !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') {
		return nil, &RuntimeSyntaxError{Line: 1}
	}
	values := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		key = strings.TrimSpace(key)
		if !ok || !runtimeKey.MatchString(key) {
			return nil, &RuntimeSyntaxError{Line: i + 1}
		}
		if _, exists := values[key]; exists {
			return nil, &RuntimeSyntaxError{Line: i + 1}
		}
		parsed, err := runtimeLiteral(strings.TrimSpace(value))
		if err != nil || strings.ContainsAny(parsed, "\r\n\x00") {
			return nil, &RuntimeSyntaxError{Line: i + 1}
		}
		values[key] = parsed
	}
	return values, nil
}
