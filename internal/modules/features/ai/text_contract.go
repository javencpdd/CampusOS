package ai

import (
	"encoding/json"
	"math"
	"unicode/utf8"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const TextContractVersion = "campusos.ai-text/v1"
const MaxChatRequestBytes = 128 * 1024
const MaxChatTextBytes = 64 * 1024
const MaxChatResponseBytes = 1024 * 1024

// Definition describes supported protocol, never a principal or a read grant.
// Only the compiled text contract is implemented in this first 14a slice.
type Definition struct {
	Version          string   `json:"version"`
	ID               string   `json:"id"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tools            bool     `json:"tools"`
	Streaming        bool     `json:"streaming"`
}

func textDefinition() Definition {
	return Definition{Version: TextContractVersion, ID: "builtin.openai-compatible.text", InputModalities: []string{"text"}, OutputModalities: []string{"text"}}
}
func (d Definition) validate() error {
	if d.Version != TextContractVersion || d.ID != "builtin.openai-compatible.text" || len(d.InputModalities) != 1 || d.InputModalities[0] != "text" || len(d.OutputModalities) != 1 || d.OutputModalities[0] != "text" || d.Tools || d.Streaming {
		return newGatewayError(ErrUnsupportedCapability, "unsupported AI text contract", nil)
	}
	return nil
}

// Existing checked JSON rules reject duplicate, null, invalid Unicode, deep
// nesting and unknown fields before decoding. No caller values enter errors.
func textObject(raw []byte, required, optional []string, limit int) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > limit {
		return nil, newGatewayError(ErrInvalidRequest, "invalid AI text object", nil)
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, newGatewayError(ErrInvalidRequest, "invalid AI text object", nil)
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		if !allowed[k] {
			return nil, newGatewayError(ErrInvalidRequest, "unknown AI text field", nil)
		}
		keys = append(keys, k)
	}
	for _, k := range required {
		if _, ok := values[k]; !ok {
			return nil, newGatewayError(ErrInvalidRequest, "missing AI text field", nil)
		}
	}
	if identityport.ValidateClosedHTTPObject(raw, keys, limit) != nil {
		return nil, newGatewayError(ErrInvalidRequest, "invalid AI text object", nil)
	}
	return values, nil
}
func (m *Message) UnmarshalJSON(raw []byte) error {
	if _, err := textObject(raw, []string{"role", "content"}, nil, MaxChatRequestBytes); err != nil {
		return err
	}
	type wire Message
	var v wire
	if json.Unmarshal(raw, &v) != nil {
		return newGatewayError(ErrInvalidRequest, "message must contain role and text strings", nil)
	}
	*m = Message(v)
	return nil
}
func (r *ChatRequest) UnmarshalJSON(raw []byte) error {
	if _, err := textObject(raw, []string{"messages"}, []string{"model", "temperature", "max_tokens", "source", "metadata"}, MaxChatRequestBytes); err != nil {
		return err
	}
	type wire ChatRequest
	var v wire
	if json.Unmarshal(raw, &v) != nil {
		return newGatewayError(ErrInvalidRequest, "invalid AI text request", nil)
	}
	candidate := ChatRequest(v)
	if err := candidate.validate(); err != nil {
		return err
	}
	*r = candidate
	return nil
}
func validText(value string, limit int, required bool) bool {
	return len(value) <= limit && utf8.ValidString(value) && (!required || len(value) > 0)
}
func validateTextRequest(r ChatRequest) error {
	if len(r.Messages) < 1 || len(r.Messages) > 64 || !validText(r.Model, 256, false) || !validText(r.Source, 128, false) || r.MaxTokens < 0 || r.MaxTokens > 65536 || len(r.Metadata) > 16 {
		return newGatewayError(ErrInvalidRequest, "invalid AI text request bounds", nil)
	}
	if r.Temperature != nil && (math.IsNaN(*r.Temperature) || math.IsInf(*r.Temperature, 0) || *r.Temperature < 0 || *r.Temperature > 2) {
		return newGatewayError(ErrInvalidRequest, "invalid AI temperature", nil)
	}
	total := 0
	for _, m := range r.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return newGatewayError(ErrUnsupportedCapability, "unsupported AI message role", nil)
		}
		if !validText(m.Content, MaxChatTextBytes, true) {
			return newGatewayError(ErrInvalidRequest, "invalid AI message text", nil)
		}
		total += len(m.Content)
	}
	if total > MaxChatTextBytes {
		return newGatewayError(ErrInvalidRequest, "AI text request exceeds limit", nil)
	}
	for k, v := range r.Metadata {
		if !validText(k, 64, true) || !validText(v, 256, false) {
			return newGatewayError(ErrInvalidRequest, "invalid AI metadata", nil)
		}
	}
	return nil
}
func validateTextResponse(r *ChatResponse) error {
	if r == nil || !validText(r.Content, MaxChatResponseBytes, true) || !validText(r.Model, 256, false) || !validText(r.Provider, 128, false) || (r.FinishReason != "" && r.FinishReason != "stop" && r.FinishReason != "length") || r.Usage.PromptTokens < 0 || r.Usage.CompletionTokens < 0 || r.Usage.TotalTokens < 0 {
		return newGatewayError(ErrProviderResponse, "invalid AI text response", nil)
	}
	return nil
}
