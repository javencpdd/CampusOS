package v4

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// The bridge is intentionally a small RPC contract. It is a declaration and
// browser-message validation boundary; every allowed request will later be
// authorized again by the host against installation, grant, consent and the
// business resource ACL. Plugins never supply a user ID, a filesystem path,
// an arbitrary URL, SQL or an access token through this contract.
const (
	MaxBridgeMessageBytes = 64 * 1024
	MaxRangeLength        = 1024 * 1024
)

var (
	bridgeRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
	bridgeMethods          = map[string]struct{}{
		"config.read":        {},
		"config.update":      {},
		"resource.describe":  {},
		"resource.readRange": {},
		"backend.invoke":     {},
		"ui.requestSurface":  {},
		"records.read":       {},
		"records.write":      {},
	}
)

// BridgeRequest is sent only over the MessagePort created by a verified
// iframe handshake. Version and request ID make a reloaded/old port
// distinguishable from an active session; neither field is an authorization
// credential.
type BridgeRequest struct {
	Version   string          `json:"version"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
}

type BridgeResponse struct {
	Version   string          `json:"version"`
	RequestID string          `json:"request_id"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *BridgeError    `json:"error,omitempty"`
}

// BridgeError intentionally carries a stable, non-sensitive code. The host
// keeps diagnostics and authorization details in its own audit log.
type BridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateBridgeRequest rejects messages that are not part of the frozen v1
// contract before any handler is selected. It does not grant access.
func ValidateBridgeRequest(raw []byte) (*BridgeRequest, error) {
	if len(raw) == 0 || len(raw) > MaxBridgeMessageBytes {
		return nil, fmt.Errorf("bridge message must be between 1 and %d bytes", MaxBridgeMessageBytes)
	}
	request := &BridgeRequest{}
	if err := json.Unmarshal(raw, request); err != nil {
		return nil, fmt.Errorf("parse bridge request: %w", err)
	}
	if request.Version != BridgeContractVersion {
		return nil, fmt.Errorf("bridge request version must be %q", BridgeContractVersion)
	}
	if !bridgeRequestIDPattern.MatchString(request.RequestID) {
		return nil, errors.New("bridge request_id is invalid")
	}
	if _, ok := bridgeMethods[request.Method]; !ok {
		return nil, errors.New("bridge method is not declared by the v1 contract")
	}
	if len(request.Params) == 0 || !json.Valid(request.Params) {
		return nil, errors.New("bridge params must be valid JSON")
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil {
		return nil, errors.New("bridge params must be a JSON object")
	}
	if request.Method == "resource.readRange" {
		if err := validateRangeParams(params); err != nil {
			return nil, err
		}
	}
	return request, nil
}

func validateRangeParams(params map[string]json.RawMessage) error {
	var (
		handle string
		offset int64
		length int64
	)
	if err := json.Unmarshal(params["handle"], &handle); err != nil || handle == "" {
		return errors.New("bridge resource.readRange requires a resource handle")
	}
	if err := json.Unmarshal(params["offset"], &offset); err != nil || offset < 0 {
		return errors.New("bridge resource.readRange offset must be a non-negative integer")
	}
	if err := json.Unmarshal(params["length"], &length); err != nil || length < 1 || length > MaxRangeLength {
		return fmt.Errorf("bridge resource.readRange length must be between 1 and %d", MaxRangeLength)
	}
	return nil
}
