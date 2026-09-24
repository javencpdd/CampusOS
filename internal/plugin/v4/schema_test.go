package v4

import "testing"

func TestValidateConfigSchema(t *testing.T) {
	valid := []byte(`{"type":"object","properties":{"zoom":{"type":"number","minimum":0.5,"maximum":3}},"additionalProperties":false}`)
	if err := ValidateConfigSchema(valid); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
	for _, input := range [][]byte{
		[]byte(`{"type":"object","$ref":"https://example.invalid/schema"}`),
		[]byte(`{"type":"object","additionalProperties":true}`),
		[]byte(`{"type":"array"}`),
	} {
		if err := ValidateConfigSchema(input); err == nil {
			t.Fatalf("unsafe schema accepted: %s", input)
		}
	}
}
