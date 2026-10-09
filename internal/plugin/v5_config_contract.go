package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrPluginV5ConfigInvalid           = errors.New("plugin.config_invalid")
	ErrPluginV5ConfigSchemaUnsupported = errors.New("plugin.config_schema_unsupported")
	ErrPluginV5ConfigDefaultsInvalid   = errors.New("plugin.config_defaults_invalid")
	ErrPluginV5ConfigRevisionConflict  = errors.New("plugin.config_revision_conflict")
	ErrPluginV5ConfigSelectorForbidden = errors.New("plugin.config_selector_forbidden")
	ErrPluginV5SecretRefInvalid        = errors.New("plugin.secret_ref_invalid")
	ErrPluginV5ConfigUnavailable       = errors.New("plugin.config_unavailable")
)

var (
	pluginV5ConfigNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	pluginV5ConfigVersionPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
	pluginV5SecretRefPattern     = regexp.MustCompile(`^secret-ref:[a-z0-9-]{2,128}$`)
	pluginV5JSONNumberPattern    = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

type pluginV5ConfigField struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Enum      []json.RawMessage `json:"enum,omitempty"`
	Minimum   *json.Number      `json:"minimum,omitempty"`
	Maximum   *json.Number      `json:"maximum,omitempty"`
	MinLength *int              `json:"minLength,omitempty"`
	MaxLength *int              `json:"maxLength,omitempty"`
}

type pluginV5ConfigSchema struct {
	Type                 string                         `json:"type"`
	AdditionalProperties *bool                          `json:"additionalProperties"`
	Properties           map[string]pluginV5ConfigField `json:"properties"`
	Required             []string                       `json:"required"`
}

type pluginV5ConfigSelector struct {
	Field string `json:"field"`
	Kind  string `json:"kind"`
}

type pluginV5ConfigDefinition struct {
	Contract    string                   `json:"contract"`
	Version     string                   `json:"version"`
	Schema      pluginV5ConfigSchema     `json:"schema"`
	Defaults    json.RawMessage          `json:"defaults"`
	Selectors   []pluginV5ConfigSelector `json:"selectors"`
	SecretNames []string                 `json:"secret_names"`
}

type PluginV5ConfigUpdate struct {
	ExpectedRevision int64             `json:"expected_revision"`
	Values           json.RawMessage   `json:"values"`
	SecretRefs       map[string]string `json:"secret_refs"`
}

type PluginV5Configuration struct {
	PluginVersionID   int64             `json:"plugin_version_id"`
	OwnerUserID       *int64            `json:"owner_user_id,omitempty"`
	DefinitionVersion string            `json:"definition_version"`
	Revision          int64             `json:"revision"`
	Values            json.RawMessage   `json:"values"`
	SecretRefs        map[string]string `json:"secret_refs"`
}

// PluginV5SelectorVerifier must consult host-owned, current visibility facts.
// It must not trust profile or collection lists supplied by a plugin or HTTP
// request. Missing verification fails closed for every selector.
type PluginV5SelectorVerifier func(context.Context, int64, *int64, string, string) (bool, error)

func strictPluginV5JSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrPluginV5ConfigInvalid
	}
	return nil
}

func parsePluginV5ConfigDefinition(manifest []byte) (pluginV5ConfigDefinition, error) {
	var envelope struct {
		APIVersion    string          `json:"api_version"`
		Configuration json.RawMessage `json:"configuration"`
	}
	if err := json.Unmarshal(manifest, &envelope); err != nil || envelope.APIVersion != "campusos.plugin/v5" || len(envelope.Configuration) == 0 {
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
	}
	var definition pluginV5ConfigDefinition
	if err := strictPluginV5JSON(envelope.Configuration, &definition); err != nil {
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
	}
	if definition.Contract != "campusos.plugin-config/v1" || len(definition.Version) > 128 ||
		!pluginV5ConfigVersionPattern.MatchString(definition.Version) ||
		len(definition.Schema.Properties) > 32 || len(definition.Schema.Required) > 32 ||
		len(definition.Selectors) > 32 || len(definition.SecretNames) > 32 ||
		definition.Schema.Type != "object" || definition.Schema.AdditionalProperties == nil || *definition.Schema.AdditionalProperties ||
		definition.Schema.Properties == nil || definition.Schema.Required == nil || definition.Selectors == nil || definition.SecretNames == nil {
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
	}
	schemaJSON, _ := json.Marshal(definition.Schema)
	if len(schemaJSON) > 16*1024 {
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
	}
	for name, field := range definition.Schema.Properties {
		if !pluginV5ConfigNamePattern.MatchString(name) || !validPluginV5ConfigField(field) {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
	}
	required := map[string]bool{}
	for _, name := range definition.Schema.Required {
		if !pluginV5ConfigNamePattern.MatchString(name) || required[name] {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
		if _, found := definition.Schema.Properties[name]; !found {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
		required[name] = true
	}
	selectorFields := map[string]bool{}
	for _, selector := range definition.Selectors {
		field, found := definition.Schema.Properties[selector.Field]
		if !found || field.Type != "string" || selectorFields[selector.Field] ||
			(selector.Kind != "profile" && selector.Kind != "public_collection") {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
		selectorFields[selector.Field] = true
	}
	secretNames := map[string]bool{}
	for _, name := range definition.SecretNames {
		if !pluginV5ConfigNamePattern.MatchString(name) || secretNames[name] {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
		if _, found := definition.Schema.Properties[name]; found {
			return pluginV5ConfigDefinition{}, ErrPluginV5ConfigSchemaUnsupported
		}
		secretNames[name] = true
	}
	if err := validatePluginV5Values(definition, definition.Defaults); err != nil {
		return pluginV5ConfigDefinition{}, ErrPluginV5ConfigDefaultsInvalid
	}
	return definition, nil
}

func validPluginV5ConfigField(field pluginV5ConfigField) bool {
	if !utf8.ValidString(field.Title) || len([]rune(field.Title)) < 1 || len([]rune(field.Title)) > 120 {
		return false
	}
	switch field.Type {
	case "string":
		if field.Minimum != nil || field.Maximum != nil {
			return false
		}
		if field.MinLength != nil && (*field.MinLength < 0 || *field.MinLength > 512) {
			return false
		}
		if field.MaxLength != nil && (*field.MaxLength < 1 || *field.MaxLength > 512) {
			return false
		}
		if field.MinLength != nil && field.MaxLength != nil && *field.MinLength > *field.MaxLength {
			return false
		}
	case "integer", "number":
		if field.MinLength != nil || field.MaxLength != nil {
			return false
		}
		var minimum, maximum *big.Rat
		if field.Minimum != nil {
			var valid bool
			minimum, valid = exactPluginV5Number(*field.Minimum)
			if !valid {
				return false
			}
		}
		if field.Maximum != nil {
			var valid bool
			maximum, valid = exactPluginV5Number(*field.Maximum)
			if !valid {
				return false
			}
		}
		if minimum != nil && maximum != nil && minimum.Cmp(maximum) > 0 {
			return false
		}
	case "boolean":
		if field.MinLength != nil || field.MaxLength != nil || field.Minimum != nil || field.Maximum != nil {
			return false
		}
	default:
		return false
	}
	if len(field.Enum) > 32 || (field.Enum != nil && len(field.Enum) == 0) {
		return false
	}
	seen := make([]any, 0, len(field.Enum))
	for _, raw := range field.Enum {
		value, err := decodePluginV5Scalar(raw)
		if err != nil || !pluginV5ValueMatchesField(field, value, false) {
			return false
		}
		for _, previous := range seen {
			if pluginV5ScalarEqual(previous, value) {
				return false
			}
		}
		seen = append(seen, value)
	}
	return true
}

func decodePluginV5Scalar(raw []byte) (any, error) {
	var value any
	if err := strictPluginV5JSON(raw, &value); err != nil {
		return nil, err
	}
	switch value.(type) {
	case string, json.Number, bool:
		return value, nil
	default:
		return nil, ErrPluginV5ConfigInvalid
	}
}

func pluginV5ValueMatchesField(field pluginV5ConfigField, value any, checkEnum bool) bool {
	switch field.Type {
	case "string":
		valueString, ok := value.(string)
		if !ok || !utf8.ValidString(valueString) || len([]rune(valueString)) > 512 {
			return false
		}
		length := len([]rune(valueString))
		if field.MinLength != nil && length < *field.MinLength || field.MaxLength != nil && length > *field.MaxLength {
			return false
		}
	case "integer", "number":
		numberValue, ok := value.(json.Number)
		if !ok {
			return false
		}
		number, valid := exactPluginV5Number(numberValue)
		if !valid || field.Type == "integer" && !number.IsInt() {
			return false
		}
		if field.Minimum != nil {
			minimum, valid := exactPluginV5Number(*field.Minimum)
			if !valid || number.Cmp(minimum) < 0 {
				return false
			}
		}
		if field.Maximum != nil {
			maximum, valid := exactPluginV5Number(*field.Maximum)
			if !valid || number.Cmp(maximum) > 0 {
				return false
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return false
		}
	default:
		return false
	}
	if checkEnum && field.Enum != nil {
		for _, raw := range field.Enum {
			item, err := decodePluginV5Scalar(raw)
			if err == nil && pluginV5ScalarEqual(item, value) {
				return true
			}
		}
		return false
	}
	return true
}

// exactPluginV5Number normalizes JSON decimal/exponent syntax without
// rounding. Decimal sizes are bounded before constructing powers of ten, so
// an attacker-controlled exponent cannot request an unbounded allocation.
func exactPluginV5Number(number json.Number) (*big.Rat, bool) {
	literal := string(number)
	if len(literal) == 0 || len(literal) > 65536 || !pluginV5JSONNumberPattern.MatchString(literal) {
		return nil, false
	}
	negative := strings.HasPrefix(literal, "-")
	unsigned := strings.TrimPrefix(literal, "-")
	mantissa, exponentText := unsigned, "0"
	if index := strings.IndexAny(unsigned, "eE"); index >= 0 {
		mantissa, exponentText = unsigned[:index], unsigned[index+1:]
	}
	fractionDigits := 0
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		fractionDigits = len(mantissa) - index - 1
		mantissa = mantissa[:index] + mantissa[index+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return new(big.Rat), true
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil || exponent < -131072 || exponent > 131072 {
		return nil, false
	}
	trimmedDigits := strings.TrimRight(digits, "0")
	exponent += int64(len(digits)-len(trimmedDigits)) - int64(fractionDigits)
	digits = trimmedDigits
	// These decimal sizes are within PostgreSQL numeric's persistent range;
	// the raw JSON envelope already bounds mantissa length to 64 KiB.
	if exponent < -16383 || int64(len(digits))+exponent > 131072 {
		return nil, false
	}
	coefficient, valid := new(big.Int).SetString(digits, 10)
	if !valid {
		return nil, false
	}
	if negative {
		coefficient.Neg(coefficient)
	}
	if exponent >= 0 {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(exponent), nil)
		return new(big.Rat).SetInt(coefficient.Mul(coefficient, factor)), true
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(-exponent), nil)
	return new(big.Rat).SetFrac(coefficient, denominator), true
}

func pluginV5ScalarEqual(left, right any) bool {
	switch value := left.(type) {
	case json.Number:
		other, ok := right.(json.Number)
		if !ok {
			return false
		}
		leftNumber, leftValid := exactPluginV5Number(value)
		rightNumber, rightValid := exactPluginV5Number(other)
		return leftValid && rightValid && leftNumber.Cmp(rightNumber) == 0
	case string:
		other, ok := right.(string)
		return ok && value == other
	case bool:
		other, ok := right.(bool)
		return ok && value == other
	default:
		return false
	}
}

func validatePluginV5Values(definition pluginV5ConfigDefinition, raw []byte) error {
	if len(raw) == 0 || len(raw) > 65536 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrPluginV5ConfigInvalid
	}
	var values map[string]json.RawMessage
	if err := strictPluginV5JSON(raw, &values); err != nil || values == nil || len(values) > 32 {
		return ErrPluginV5ConfigInvalid
	}
	for name, rawValue := range values {
		field, found := definition.Schema.Properties[name]
		if !found {
			return ErrPluginV5ConfigInvalid
		}
		value, err := decodePluginV5Scalar(rawValue)
		if err != nil || !pluginV5ValueMatchesField(field, value, true) {
			return ErrPluginV5ConfigInvalid
		}
	}
	for _, name := range definition.Schema.Required {
		if _, found := values[name]; !found {
			return ErrPluginV5ConfigInvalid
		}
	}
	return nil
}

func validatePluginV5SecretRefs(definition pluginV5ConfigDefinition, refs map[string]string) error {
	if refs == nil || len(refs) > 32 {
		return ErrPluginV5SecretRefInvalid
	}
	allowed := map[string]bool{}
	for _, name := range definition.SecretNames {
		allowed[name] = true
	}
	seenRefs := map[string]bool{}
	for name, ref := range refs {
		if !allowed[name] || !pluginV5SecretRefPattern.MatchString(ref) || seenRefs[ref] {
			return ErrPluginV5SecretRefInvalid
		}
		seenRefs[ref] = true
	}
	return nil
}

func validatePluginV5Selectors(ctx context.Context, definition pluginV5ConfigDefinition, values []byte, versionID int64, ownerID *int64, verify PluginV5SelectorVerifier) error {
	if len(definition.Selectors) == 0 {
		return nil
	}
	if verify == nil {
		return ErrPluginV5ConfigSelectorForbidden
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(values, &entries); err != nil {
		return ErrPluginV5ConfigInvalid
	}
	for _, selector := range definition.Selectors {
		var selected string
		if err := json.Unmarshal(entries[selector.Field], &selected); err != nil || selected == "" || strings.TrimSpace(selected) != selected {
			return ErrPluginV5ConfigSelectorForbidden
		}
		allowed, err := verify(ctx, versionID, ownerID, selector.Kind, selected)
		if err != nil || !allowed {
			return ErrPluginV5ConfigSelectorForbidden
		}
	}
	return nil
}
