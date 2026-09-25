package plugin

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func normalizePluginConfig(manifest *Manifest, input map[string]interface{}) (map[string]interface{}, error) {
	if input == nil {
		input = map[string]interface{}{}
	}
	if manifest == nil || manifest.ConfigSchema == nil {
		return copyConfigMap(input), nil
	}

	normalized := make(map[string]interface{}, len(manifest.ConfigSchema.Fields))
	for _, field := range manifest.ConfigSchema.Fields {
		if field.Secret {
			if _, supplied := input[field.Key]; supplied {
				return nil, fmt.Errorf("config field %q is secret; use the dedicated secret endpoint", field.Key)
			}
			continue
		}
		value, ok := input[field.Key]
		if !ok {
			value, ok = manifest.Config[field.Key]
		}
		if !ok {
			value = field.Default
		}
		if value == nil {
			if field.Required {
				return nil, fmt.Errorf("config field %q is required", field.Key)
			}
			continue
		}
		coerced, err := coerceConfigValue(field, value)
		if err != nil {
			return nil, err
		}
		if err := validateConfigConstraints(field, coerced); err != nil {
			return nil, err
		}
		normalized[field.Key] = coerced
	}
	return normalized, nil
}

func validateConfigConstraints(field ConfigField, value interface{}) error {
	if field.Type == "number" {
		number, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("config field %q must be number", field.Key)
		}
		if field.Min != nil && number < *field.Min {
			return fmt.Errorf("config field %q must be at least %v", field.Key, *field.Min)
		}
		if field.Max != nil && number > *field.Max {
			return fmt.Errorf("config field %q must be at most %v", field.Key, *field.Max)
		}
	}
	if field.Type == "string" || field.Type == "text" || field.Type == "select" {
		text := fmt.Sprint(value)
		length := len([]rune(text))
		if field.MinLength > 0 && length < field.MinLength {
			return fmt.Errorf("config field %q must contain at least %d characters", field.Key, field.MinLength)
		}
		if field.MaxLength > 0 && length > field.MaxLength {
			return fmt.Errorf("config field %q must contain at most %d characters", field.Key, field.MaxLength)
		}
		if field.Pattern != "" {
			pattern, err := regexp.Compile(field.Pattern)
			if err != nil || !pattern.MatchString(text) {
				return fmt.Errorf("config field %q does not match its required pattern", field.Key)
			}
		}
		switch field.Format {
		case "email":
			if _, err := mail.ParseAddress(text); err != nil {
				return fmt.Errorf("config field %q must be a valid email address", field.Key)
			}
		case "url":
			parsed, err := url.ParseRequestURI(text)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return fmt.Errorf("config field %q must be an absolute URL", field.Key)
			}
		case "hostname":
			matched, _ := regexp.MatchString(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`, text)
			if !matched {
				return fmt.Errorf("config field %q must be a valid hostname", field.Key)
			}
		}
	}
	return nil
}

func toFloat64(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func coerceConfigValue(field ConfigField, value interface{}) (interface{}, error) {
	switch field.Type {
	case "boolean":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			trimmed := strings.ToLower(strings.TrimSpace(v))
			switch trimmed {
			case "true", "1", "yes", "on":
				return true, nil
			case "false", "0", "no", "off":
				return false, nil
			default:
				return nil, fmt.Errorf("config field %q must be boolean", field.Key)
			}
		default:
			return nil, fmt.Errorf("config field %q must be boolean", field.Key)
		}
	case "number":
		switch v := value.(type) {
		case int:
			return v, nil
		case int64:
			return v, nil
		case float64:
			return v, nil
		case float32:
			return float64(v), nil
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return nil, fmt.Errorf("config field %q must be number", field.Key)
			}
			parsed, err := strconv.ParseFloat(trimmed, 64)
			if err != nil {
				return nil, fmt.Errorf("config field %q must be number", field.Key)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("config field %q must be number", field.Key)
		}
	case "select":
		for _, option := range field.Options {
			if fmt.Sprint(option.Value) == fmt.Sprint(value) {
				return option.Value, nil
			}
		}
		return nil, fmt.Errorf("config field %q has invalid option %q", field.Key, fmt.Sprint(value))
	case "string", "text":
		return fmt.Sprint(value), nil
	case "json":
		return value, nil
	default:
		return value, nil
	}
}

func copyConfigMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	copied := make(map[string]interface{}, len(input))
	for key, value := range input {
		copied[key] = value
	}
	return copied
}
