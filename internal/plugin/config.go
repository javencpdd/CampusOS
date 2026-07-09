package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/safehtml"
	"github.com/campusos/CampusOS/internal/stylepack"
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
		normalized[field.Key] = coerced
	}
	return normalized, nil
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

func validatePluginSpecificConfig(pluginName string, config map[string]interface{}) error {
	if pluginName != "homepage-customizer" {
		return nil
	}
	if err := validateHomepageCustomHTML(config); err != nil {
		return err
	}
	if err := validateHomepageCustomCSS(config); err != nil {
		return err
	}
	return nil
}

func preservePluginInternalConfig(pluginName string, normalized, incoming, current map[string]interface{}) {
	if pluginName != "homepage-customizer" {
		return
	}
	const snapshotKey = "last_config_snapshot"
	if value, ok := incoming[snapshotKey]; ok {
		normalized[snapshotKey] = value
		return
	}
	if value, ok := current[snapshotKey]; ok {
		normalized[snapshotKey] = value
	}
}

func validateHomepageCustomHTML(config map[string]interface{}) error {
	value, ok := config["custom_html"]
	if !ok || value == nil {
		return nil
	}
	htmlValue := strings.TrimSpace(fmt.Sprint(value))
	if htmlValue == "" {
		return nil
	}
	result := safehtml.Validate(htmlValue)
	if result.Valid {
		return nil
	}
	return fmt.Errorf("config field %q failed safe HTML validation: %s", "custom_html", strings.Join(result.Errors, "; "))
}

func validateHomepageCustomCSS(config map[string]interface{}) error {
	value, ok := config["custom_css"]
	if !ok || value == nil {
		return nil
	}
	cssValue := strings.TrimSpace(fmt.Sprint(value))
	if cssValue == "" {
		return nil
	}
	result := stylepack.ValidateCSS(cssValue)
	if result.Valid {
		return nil
	}
	return fmt.Errorf("config field %q failed safe CSS validation: %s", "custom_css", strings.Join(result.Errors, "; "))
}
