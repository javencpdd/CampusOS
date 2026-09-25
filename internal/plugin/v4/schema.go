package v4

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ValidateConfigSchema fixes the supported configuration-schema subset before
// package installation exists. It deliberately rejects executable validators,
// remote references and unknown keywords. A later builder may resolve a local
// reference only after adding an explicit, cycle-safe resolver.
func ValidateConfigSchema(raw []byte) error {
	if len(raw) == 0 || int64(len(raw)) > maxConfigBytes {
		return fmt.Errorf("configuration schema must be between 1 and %d bytes", maxConfigBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("parse configuration schema: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("configuration schema must contain one JSON value")
	}
	object, ok := value.(map[string]any)
	if !ok || object["type"] != "object" {
		return errors.New("configuration schema root must be an object schema")
	}
	return validateSchemaObject(object, "$", true)
}

func validateSchemaObject(schema map[string]any, location string, root bool) error {
	allowed := map[string]bool{
		"$schema": true, "$id": true, "type": true, "title": true, "description": true,
		"default": true, "properties": true, "required": true, "additionalProperties": true,
		"enum": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true,
		"pattern": true, "format": true, "items": true, "minItems": true, "maxItems": true,
	}
	for key, value := range schema {
		if !allowed[key] {
			return fmt.Errorf("configuration schema %s uses unsupported keyword %q", location, key)
		}
		if key == "$schema" || key == "$id" {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("configuration schema %s %s must be a string", location, key)
			}
			// $schema declares a JSON-Schema vocabulary only. It is never fetched
			// by CampusOS and the recognized URI is not a package dependency.
			if key == "$schema" && !knownSchemaVocabulary(text) {
				return fmt.Errorf("configuration schema %s uses an unsupported schema vocabulary", location)
			}
			if key == "$id" && strings.Contains(text, "://") {
				return fmt.Errorf("configuration schema %s $id must not reference a remote URL", location)
			}
		}
	}
	if properties, exists := schema["properties"]; exists {
		children, ok := properties.(map[string]any)
		if !ok {
			return fmt.Errorf("configuration schema %s properties must be an object", location)
		}
		for name, rawChild := range children {
			child, ok := rawChild.(map[string]any)
			if !ok {
				return fmt.Errorf("configuration schema %s.properties.%s must be an object", location, name)
			}
			if err := validateSchemaObject(child, location+".properties."+name, false); err != nil {
				return err
			}
		}
	}
	if items, exists := schema["items"]; exists {
		itemSchema, ok := items.(map[string]any)
		if !ok {
			return fmt.Errorf("configuration schema %s items must be an object", location)
		}
		if err := validateSchemaObject(itemSchema, location+".items", false); err != nil {
			return err
		}
	}
	if root && schema["additionalProperties"] == true {
		return errors.New("configuration schema root must not allow undeclared properties")
	}
	return nil
}

func knownSchemaVocabulary(value string) bool {
	return value == "https://json-schema.org/draft/2020-12/schema" ||
		value == "https://json-schema.org/draft/2019-09/schema" ||
		value == "http://json-schema.org/draft-07/schema#"
}
