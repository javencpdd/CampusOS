package port

import (
	"encoding/json"
	"strconv"
)

// ValidateIntegrationHTTPJSON applies the shared bounded/duplicate/Unicode
// decoder to the public management DTO, never to supplied Principal/facts.
func ValidateIntegrationHTTPJSON(raw []byte, fields []string) error {
	if !resourceJSONUnicodeValid(raw) {
		return ErrIntegrationInputInvalid
	}
	values, err := strictObject(raw, fields, nil)
	if err != nil {
		return ErrIntegrationInputInvalid
	}
	for _, key := range []string{"version", "principal_version"} {
		if value, ok := values[key]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return ErrIntegrationInputInvalid
			}
			version, e := strconv.ParseInt(text, 10, 64)
			if e != nil || version < 1 || strconv.FormatInt(version, 10) != text {
				return ErrIntegrationInputInvalid
			}
		}
	}
	if rulesRaw, ok := values["rules"]; ok {
		var rules []json.RawMessage
		if json.Unmarshal(rulesRaw, &rules) != nil || rules == nil || len(rules) > 64 {
			return ErrIntegrationInputInvalid
		}
		for _, rule := range rules {
			if _, err := strictObject(rule, []string{"id", "channel", "endpoint", "action", "resource_kind", "resource_ids", "status", "not_before_ms", "expires_at_ms"}, nil); err != nil {
				return ErrIntegrationInputInvalid
			}
		}
	}
	return nil
}
