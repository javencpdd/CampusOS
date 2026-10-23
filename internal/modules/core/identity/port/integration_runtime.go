package port

import (
	"context"
	"encoding/json"
	"errors"
)

const IntegrationRuntimeContract = "campusos.integration-access/v2"

var (
	ErrIntegrationDenied            = errors.New("integration.denied")
	ErrIntegrationMFARequired       = errors.New("integration.mfa_required")
	ErrIntegrationUnavailable       = errors.New("integration.unavailable")
	ErrIntegrationChanged           = errors.New("integration.facts_changed")
	ErrIntegrationCredentialInvalid = errors.New("integration.credential_invalid")
)

func IntegrationResourceIDValid(id string) bool { return validOpaqueID(id) }

// Host-only callback: the resource module still authorizes and reads its own
// current public row. Nothing is returned until the machine proof is rechecked
// inside the same transaction. Never accept callbacks or endpoint facts via HTTP.
type IntegrationReadGateway interface {
	ReadForIntegration(context.Context, string, IntegrationAccessTarget, func(context.Context) (any, error)) (any, error)
}

// Host codec retains v1's strict shape and exact integers. It never accepts a
// v1 value as v2 or supplies facts to the public HTTP management handlers.
func ParseIntegrationRuntimeInputJSON(raw []byte) (IntegrationAccessInput, error) {
	bad := func() (IntegrationAccessInput, error) { return IntegrationAccessInput{}, ErrIntegrationInputInvalid }
	if !resourceJSONUnicodeValid(raw) {
		return bad()
	}
	fields, err := strictObject(raw, []string{"contract", "request_id", "principal", "target", "facts", "now_ms"}, nil)
	if err != nil {
		return bad()
	}
	var contract string
	if json.Unmarshal(fields["contract"], &contract) != nil || contract != IntegrationRuntimeContract {
		return bad()
	}
	fields["contract"], _ = json.Marshal(IntegrationAccessContract)
	normalized, err := json.Marshal(fields)
	if err != nil {
		return bad()
	}
	r, err := ParseIntegrationAccessInputJSON(normalized)
	if err != nil {
		return bad()
	}
	r.Contract = IntegrationRuntimeContract
	return r, nil
}
