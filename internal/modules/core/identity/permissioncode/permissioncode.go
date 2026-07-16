// Package permissioncode defines the stable public identifiers used by the
// authorization catalog. Legacy resource:action pairs remain supported during
// the v10 migration, but callers should use Code for new contracts.
package permissioncode

import "strings"

// FromLegacy converts the pre-v10 resource:action representation into a
// stable domain code. Unknown resources intentionally retain their name under
// the platform domain rather than receiving an implicit wildcard.
func FromLegacy(resource, action string) string {
	resource = strings.ToLower(strings.TrimSpace(resource))
	action = strings.ToLower(strings.TrimSpace(action))
	if resource == "" || action == "" {
		return ""
	}
	domain := map[string]string{
		"user":         "identity",
		"role":         "identity",
		"thread":       "community",
		"post":         "community",
		"category":     "community",
		"richtext":     "community",
		"space":        "personal_space",
		"homepage":     "appearance",
		"plugin":       "plugin",
		"feature":      "platform",
		"webhook":      "integration",
		"mcp":          "integration",
		"message":      "integration",
		"integration":  "integration",
		"metrics":      "platform",
		"platform_log": "platform",
		"ai":           "ai",
	}[resource]
	if domain == "" {
		domain = "platform"
	}
	return domain + "." + resource + "." + action
}

// LegacyForCode is the conservative compatibility mapping used while the old
// permissions table remains readable. It never broadens an unknown code.
func LegacyForCode(code string) (resource, action string, ok bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	special := map[string][2]string{
		"community.thread.take_down":       {"thread", "delete"},
		"community.thread.review":          {"thread", "write"},
		"community.thread.direct_restore":  {"thread", "delete"},
		"community.thread.restore":         {"thread", "delete"},
		"community.thread.purge":           {"thread", "delete"},
		"community.thread.trash":           {"thread", "delete"},
		"identity.role.create":             {"role", "manage"},
		"identity.role.update_permissions": {"role", "manage"},
		"identity.role.read_audit":         {"role", "read"},
	}
	if pair, exists := special[code]; exists {
		return pair[0], pair[1], true
	}
	parts := strings.Split(code, ".")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// IsCode validates the intentionally small v10 code grammar. Code is stable
// documentation/API material, so URLs, whitespace and wildcard syntax are
// rejected.
func IsCode(code string) bool {
	parts := strings.Split(strings.TrimSpace(code), ".")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, runeValue := range part {
			if !(runeValue >= 'a' && runeValue <= 'z') && !(runeValue >= '0' && runeValue <= '9') && runeValue != '_' {
				return false
			}
		}
	}
	return true
}
