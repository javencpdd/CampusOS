package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
)

func assertSecretUseAudit(t *testing.T, store *reliability.MemoryStore) (reliability.Operation, secretUseAuditDetails) {
	t.Helper()
	operations, count, err := store.ListOperations(context.Background(), secretUseOperationKind, reliability.PageRequest{Page: 1, PageSize: 10})
	if err != nil || count != 1 || len(operations) != 1 {
		t.Fatalf("expected one persistent use operation: count=%d rows=%d err=%v", count, len(operations), err)
	}
	operation := operations[0]
	var details secretUseAuditDetails
	if err := json.Unmarshal(operation.Details, &details); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(operation.Details, &fields); err != nil || len(fields) != 9 {
		t.Fatalf("unexpected use audit fields: fields=%v err=%v", fields, err)
	}
	for _, key := range []string{"plugin_version_id", "owner_user_id", "purpose", "outcome", "reserved_units", "charged_units", "charge_known", "status_code", "error_code"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing audit field %s", key)
		}
	}
	for _, sensitive := range []string{"fixture-token", "mail.password", "secret-ref:", "mail-primary", "https://", "never-return-body"} {
		if strings.Contains(string(operation.Details)+operation.Error, sensitive) {
			t.Fatalf("use audit contains private data %q", sensitive)
		}
	}
	if details.PluginVersionID <= 0 || details.Purpose != "notify-course" {
		t.Fatalf("audit lost non-secret scope: %+v", details)
	}
	return operation, details
}

type failingSecretUseAudit struct {
	store     *reliability.MemoryStore
	startErr  error
	updateErr error
	updated   reliability.Operation
	ctxErr    error
	deadline  time.Time
}

func (s *failingSecretUseAudit) StartOperation(ctx context.Context, operation reliability.Operation) (*reliability.Operation, error) {
	if s.startErr != nil {
		return nil, s.startErr
	}
	return s.store.StartOperation(ctx, operation)
}

func (s *failingSecretUseAudit) UpdateOperation(ctx context.Context, operation reliability.Operation) error {
	s.updated = operation
	s.ctxErr = ctx.Err()
	s.deadline, _ = ctx.Deadline()
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.store.UpdateOperation(ctx, operation)
}

func TestSecretUseBrokerAuditFailuresFailClosed(t *testing.T) {
	t.Run("constructor-requires-audit", func(t *testing.T) {
		f := newSecretBrokerFixture(t)
		if _, err := NewSecretUseBroker(f.auth, f.refs, f.broker.secrets, f.resources, f.broker.budgets, f.egress, nil); !errors.Is(err, ErrSecretUseDenied) {
			t.Fatalf("missing audit accepted: %v", err)
		}
	})
	t.Run("start-failure-before-budget-and-network", func(t *testing.T) {
		f := newSecretBrokerFixture(t)
		f.broker.audit = &failingSecretUseAudit{store: f.audit, startErr: errors.New("fixture-token-start-error")}
		if result, err := f.broker.Use(t.Context(), f.request); !errors.Is(err, ErrSecretUseFailed) || result != (SecretUseResult{}) {
			t.Fatalf("audit start failure did not deny: result=%+v err=%v", result, err)
		}
		if f.egress.calls != 0 || f.budget.Snapshot().ReservedUnits != 0 || f.budget.Snapshot().ConsumedUnits != 0 {
			t.Fatal("failed audit start reserved budget or dispatched")
		}
	})
	t.Run("update-failure-after-send-keeps-running", func(t *testing.T) {
		f := newSecretBrokerFixture(t)
		failed := &failingSecretUseAudit{store: f.audit, updateErr: errors.New("fixture-token-update-error")}
		f.broker.audit = failed
		if result, err := f.broker.Use(t.Context(), f.request); !errors.Is(err, ErrSecretUseFailed) || result != (SecretUseResult{}) {
			t.Fatalf("audit update failure returned success: result=%+v err=%v", result, err)
		}
		if f.egress.calls != 1 || f.budget.Snapshot().ConsumedUnits != 1 || f.budget.Snapshot().InFlight != 0 {
			t.Fatal("sent use lost charge or was blindly retried")
		}
		operation, details := assertSecretUseAudit(t, f.audit)
		if operation.Status != reliability.OperationRunning || details.Outcome != "running" ||
			details.ChargedUnits != nil || details.ChargeKnown {
			t.Fatalf("failed final update fabricated outcome: operation=%+v details=%+v", operation, details)
		}
		if failed.ctxErr != nil || failed.deadline.IsZero() || time.Until(failed.deadline) > 5*time.Second {
			t.Fatal("final audit did not have an independent bounded context")
		}
	})
}

type cancelledSecretUseSender struct{ cancel context.CancelFunc }

func (s *cancelledSecretUseSender) Do(context.Context, security.EgressRequest) (security.EgressResponse, error) {
	s.cancel()
	return security.EgressResponse{}, context.Canceled
}

func TestSecretUseBrokerAuditsUnknownAfterCallerCancellation(t *testing.T) {
	f := newSecretBrokerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	audit := &failingSecretUseAudit{store: f.audit}
	f.broker.audit = audit
	f.broker.egress = &cancelledSecretUseSender{cancel: cancel}
	if _, err := f.broker.Use(ctx, f.request); !errors.Is(err, ErrSecretUseFailed) {
		t.Fatalf("cancelled dispatched use not failed: %v", err)
	}
	_, details := assertSecretUseAudit(t, f.audit)
	if details.Outcome != "unknown" || details.ChargedUnits == nil || *details.ChargedUnits != 1 || audit.ctxErr != nil || audit.deadline.IsZero() {
		t.Fatalf("cancelled request lost charge or audit: %+v ctxerr=%v", details, audit.ctxErr)
	}
}

func TestSecretUseBrokerRejectsOversizedInputBeforeBudget(t *testing.T) {
	for _, name := range []string{"body", "url"} {
		t.Run(name, func(t *testing.T) {
			f := newSecretBrokerFixture(t)
			request := f.request
			if name == "body" {
				request.Body = make([]byte, secretUseMaxBodyBytes+1)
			} else {
				request.TargetURL = "https://mailer.example.edu/" + strings.Repeat("a", secretUseMaxURLBytes)
			}
			if _, err := f.broker.Use(t.Context(), request); !errors.Is(err, ErrSecretUseDenied) {
				t.Fatalf("oversized input not denied: %v", err)
			}
			if f.egress.calls != 0 || f.budget.Snapshot().ReservedUnits != 0 || f.budget.Snapshot().ConsumedUnits != 0 {
				t.Fatal("oversized input reserved budget or dispatched")
			}
			_, count, err := f.audit.ListOperations(t.Context(), secretUseOperationKind, reliability.PageRequest{Page: 1, PageSize: 10})
			if err != nil || count != 0 {
				t.Fatal("oversized input reached execution audit")
			}
		})
	}
}
