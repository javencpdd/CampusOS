package service

import (
	"strings"
	"unicode/utf8"
)

const (
	maxThreadTags = 20
	maxTagLength  = 32
)

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		value := strings.TrimSpace(tag)
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > maxTagLength {
			value = string([]rune(value)[:maxTagLength])
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= maxThreadTags {
			break
		}
	}
	return normalized
}

func mergeTags(tagGroups ...[]string) []string {
	total := 0
	for _, tags := range tagGroups {
		total += len(tags)
	}
	merged := make([]string, 0, total)
	for _, tags := range tagGroups {
		merged = append(merged, tags...)
	}
	return normalizeTags(merged)
}
