package port

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	policyJSONLimit    = 32 * 1024
	policyJSONMaxDepth = 64
)

var errStrictJSON = errors.New("invalid policy JSON object")

// decodeStrictObject checks one bounded object before any contract semantics.
// Duplicate keys (including escaped equivalents) are rejected at every depth
// before contract precedence is applied. Semantic field validation stays with
// the exact-schema parsers; nesting is bounded independently of the byte limit.
func decodeStrictObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > policyJSONLimit || !utf8.Valid(raw) {
		return nil, errStrictJSON
	}
	syntax := json.NewDecoder(bytes.NewReader(raw))
	syntax.UseNumber()
	if err := strictJSONValue(syntax, 0); err != nil {
		return nil, errStrictJSON
	}
	if _, err := syntax.Token(); err != io.EOF {
		return nil, errStrictJSON
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errStrictJSON
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errStrictJSON
		}
		if _, exists := fields[key]; exists {
			return nil, errStrictJSON
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, errStrictJSON
		}
		fields[key] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return nil, errStrictJSON
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, errStrictJSON
	}
	return fields, nil
}

// strictObject rejects unknown, missing, null, duplicate and trailing fields.
// It deliberately returns only a generic error, never caller-supplied data.
func strictObject(raw []byte, required, optional []string) (map[string]json.RawMessage, error) {
	fields, err := decodeStrictObject(raw)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
		if _, exists := fields[key]; !exists {
			return nil, errStrictJSON
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errStrictJSON
		}
	}
	return fields, nil
}

// strictJSONValue detects ambiguous objects even inside otherwise unknown
// fields, while accepting the full JSON numeric grammar without float coercion.
func strictJSONValue(dec *json.Decoder, depth int) error {
	if depth > policyJSONMaxDepth {
		return errStrictJSON
	}
	token, err := dec.Token()
	if err != nil {
		return errStrictJSON
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			token, err := dec.Token()
			key, ok := token.(string)
			if err != nil || !ok || seen[key] {
				return errStrictJSON
			}
			seen[key] = true
			if err := strictJSONValue(dec, depth+1); err != nil {
				return errStrictJSON
			}
		}
		if token, err := dec.Token(); err != nil || token != json.Delim('}') {
			return errStrictJSON
		}
	case '[':
		for dec.More() {
			if err := strictJSONValue(dec, depth+1); err != nil {
				return errStrictJSON
			}
		}
		if token, err := dec.Token(); err != nil || token != json.Delim(']') {
			return errStrictJSON
		}
	default:
		return errStrictJSON
	}
	return nil
}
