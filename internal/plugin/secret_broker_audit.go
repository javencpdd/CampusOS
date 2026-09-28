package plugin

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
)

const secretUseOperationKind = "plugin.secret.use"

// SecretUseAuditStore persists the host operation before reserving a budget
// or constructing an authenticated request. A missing audit store fails closed.
type SecretUseAuditStore interface {
	StartOperation(context.Context, reliability.Operation) (*reliability.Operation, error)
	UpdateOperation(context.Context, reliability.Operation) error
}

// Details contain only non-secret scope and accounting facts. An unknown
// network outcome charges the entire reservation conservatively, so its
// charged_units may be present while charge_known is false. A nil charged_units
// means settlement was not confirmed; it must never be read as zero. Likewise,
// an initial running row cannot prove whether a request was dispatched later.
type secretUseAuditDetails struct {
	PluginVersionID int64  `json:"plugin_version_id"`
	OwnerUserID     *int64 `json:"owner_user_id"`
	Purpose         string `json:"purpose"`
	Outcome         string `json:"outcome"`
	ReservedUnits   int64  `json:"reserved_units"`
	ChargedUnits    *int64 `json:"charged_units"`
	ChargeKnown     bool   `json:"charge_known"`
	StatusCode      int    `json:"status_code"`
	ErrorCode       string `json:"error_code"`
}

func secretUseUnits(units int64) *int64 { return &units }

func secretUseDetails(details secretUseAuditDetails) json.RawMessage {
	encoded, _ := json.Marshal(details)
	return encoded
}

func (b *SecretUseBroker) startUseAudit(ctx context.Context, request SecretUseRequest) (*reliability.Operation, secretUseAuditDetails, error) {
	details := secretUseAuditDetails{PluginVersionID: request.PluginVersionID,
		OwnerUserID: request.OwnerUserID, Purpose: request.Purpose, Outcome: "running"}
	if b.audit == nil {
		return nil, details, ErrSecretUseDenied
	}
	operation, err := b.audit.StartOperation(ctx, reliability.Operation{
		Kind: secretUseOperationKind, SubjectType: "plugin_version", SubjectID: strconv.FormatInt(request.PluginVersionID, 10),
		Status: reliability.OperationRunning, ActorID: "system:secret-broker", Details: secretUseDetails(details),
	})
	if err != nil || operation == nil || operation.ID == "" {
		return nil, details, ErrSecretUseDenied
	}
	return operation, details, nil
}

func (b *SecretUseBroker) finishUseAudit(operation *reliability.Operation, details secretUseAuditDetails) error {
	operation.Status = reliability.OperationFailed
	if details.Outcome == "success" {
		operation.Status = reliability.OperationSucceeded
	}
	operation.Details = secretUseDetails(details)
	// Preserve stable host error codes, never resolver/remote error strings.
	operation.Error = details.ErrorCode
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.audit.UpdateOperation(ctx, *operation)
}
