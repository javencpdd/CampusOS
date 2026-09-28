package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/security"
)

type testSecretRefBinding struct{ allowed bool }

func (r *testSecretRefBinding) IsBoundPluginV5SecretRef(context.Context, int64, *int64, string, string) (bool, error) {
	return r.allowed, nil
}

type testSecretResourceDecision struct {
	allowed bool
}

func (r *testSecretResourceDecision) CurrentSecretResources(context.Context, int64, string, string) (security.ResourceDecision, error) {
	targets := []string{}
	if r.allowed {
		targets = []string{"https://mailer.example.edu"}
	}
	return security.ResourceDecision{Effective: security.ResourceSet{NetworkTargets: targets, TimeoutMS: 2000, MaxConcurrency: 1}}, nil
}

type testSecretPurposeBudget struct{ budget *security.PurposeBudget }

func (p *testSecretPurposeBudget) CurrentSecretPurposeBudget(context.Context, int64, string) (*security.PurposeBudget, error) {
	return p.budget, nil
}

type testSecretEgress struct {
	calls int
	last  security.EgressRequest
	err   error
}

func (e *testSecretEgress) Do(_ context.Context, request security.EgressRequest) (security.EgressResponse, error) {
	e.calls++
	request.Headers = request.Headers.Clone()
	e.last = request
	if e.err != nil {
		return security.EgressResponse{}, e.err
	}
	return security.EgressResponse{StatusCode: 204, Body: []byte("never-return-body")}, nil
}

type secretBrokerFixture struct {
	broker    *SecretUseBroker
	auth      *AuthorizationService
	version   PluginVersion
	refs      *testSecretRefBinding
	resources *testSecretResourceDecision
	budget    *security.PurposeBudget
	egress    *testSecretEgress
	audit     *reliability.MemoryStore
	request   SecretUseRequest
}

func newSecretBrokerFixture(t *testing.T) secretBrokerFixture {
	t.Helper()
	ctx := context.Background()
	repo := NewMemoryPluginRepository()
	const pluginID int64 = 99126101
	if err := repo.Save(ctx, &PluginRecord{ID: pluginID, Name: "secret-broker-fixture", Status: string(StatusEnabled)}); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{
		Name: "secret-broker-fixture", DisplayName: "Secret Broker Fixture", Version: "1.0.0",
		APIVersion: ManifestAPIVersionV3, HostAPIVersion: HostAPIVersionV3,
		Runtime: "process", Scope: ScopeSystem,
		CapabilityDeclarations: []CapabilityRequest{
			{Code: "secret.system.read", Required: true, Purpose: "发送课程通知", Scope: "system"},
		},
	}
	store := NewMemoryAuthorizationStore()
	auth := NewAuthorizationService(store, repo, func(string) bool { return true })
	version, err := auth.SyncInstalled(ctx, &Plugin{Manifest: manifest, Checksum: strings.Repeat("a", 64)}, "1")
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := NewSecretService(NewMemorySecretStore(), []byte("0123456789abcdef0123456789abcdef"), "key-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Put(ctx, pluginID, nil, "mail.password", "fixture-token-do-not-return", nil); err != nil {
		t.Fatal(err)
	}
	budget, err := security.NewPurposeBudget("notify-course", security.PurposeBudgetLimits{
		MaxUnits: 2, MaxConcurrency: 1, MaxDuration: 2 * time.Second,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := &testSecretRefBinding{allowed: true}
	resources := &testSecretResourceDecision{allowed: true}
	egress := &testSecretEgress{}
	budgets := &testSecretPurposeBudget{budget: budget}
	audit := reliability.NewMemoryStore()
	broker, err := NewSecretUseBroker(auth, refs, secrets, resources, budgets, egress, audit)
	if err != nil {
		t.Fatal(err)
	}
	request := SecretUseRequest{
		PluginVersionID: version.ID, SecretName: "mail.password",
		SecretRef: "secret-ref:mail-old", ProfileID: "mail-primary",
		TargetURL: "https://mailer.example.edu/v1/send", Purpose: "notify-course",
		Body: []byte("{}"),
	}
	grant := map[string]interface{}{
		"scope": "system", "secret_bindings": []interface{}{secretUseBinding(request)},
	}
	if _, err := auth.SetAdminGrant(ctx, version.ID, "secret.system.read", "granted", "approve exact mail binding", grant, "1", nil); err != nil {
		t.Fatal(err)
	}
	return secretBrokerFixture{
		broker: broker, auth: auth, version: version, refs: refs,
		resources: resources, budget: budget, egress: egress, audit: audit, request: request,
	}
}

func TestSecretUseBrokerBoundAuthenticatedSendAndBudget(t *testing.T) {
	fixture := newSecretBrokerFixture(t)
	result, err := fixture.broker.Use(context.Background(), fixture.request)
	if err != nil || result.StatusCode != 204 {
		t.Fatalf("approved Secret use: %+v %v", result, err)
	}
	if fixture.egress.calls != 1 || fixture.egress.last.URL != fixture.request.TargetURL ||
		fixture.egress.last.Headers.Get("Authorization") != "Bearer fixture-token-do-not-return" {
		t.Fatalf("host did not send credential to exact target: %+v", fixture.egress)
	}
	if strings.Contains(strings.TrimSpace(string(fixture.egress.last.Body)), "fixture-token") {
		t.Fatal("request body included credential")
	}
	if snapshot := fixture.budget.Snapshot(); snapshot.ConsumedUnits != 1 || snapshot.InFlight != 0 {
		t.Fatalf("budget was not settled: %+v", snapshot)
	}
	if result.StatusCode == 0 {
		t.Fatal("status was not returned")
	}
	operation, details := assertSecretUseAudit(t, fixture.audit)
	if operation.Status != reliability.OperationSucceeded || details.Outcome != "success" ||
		details.ReservedUnits != 1 || details.ChargedUnits == nil || *details.ChargedUnits != 1 ||
		!details.ChargeKnown || details.StatusCode != 204 || details.ErrorCode != "" {
		t.Fatalf("success use audit lost accounting: operation=%+v details=%+v", operation, details)
	}
}

func TestSecretUseBrokerRejectsChangedBindingAndRevocation(t *testing.T) {
	fixture := newSecretBrokerFixture(t)
	for _, change := range []func(*SecretUseRequest){
		func(r *SecretUseRequest) { r.SecretRef = "secret-ref:other" },
		func(r *SecretUseRequest) { r.ProfileID = "other-profile" },
		func(r *SecretUseRequest) { r.TargetURL = "https://mailer.example.edu/v1/echo" },
		func(r *SecretUseRequest) { r.Purpose = "export-private-data" },
	} {
		request := fixture.request
		change(&request)
		if _, err := fixture.broker.Use(context.Background(), request); !errors.Is(err, ErrSecretUseDenied) {
			t.Fatalf("changed binding not denied: %v", err)
		}
	}
	fixture.refs.allowed = false
	if _, err := fixture.broker.Use(context.Background(), fixture.request); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("unbound config ref not denied: %v", err)
	}
	fixture.refs.allowed = true
	fixture.resources.allowed = false
	if _, err := fixture.broker.Use(context.Background(), fixture.request); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("resource target not denied: %v", err)
	}
	fixture.resources.allowed = true
	if _, err := fixture.auth.SetAdminGrant(context.Background(), fixture.version.ID, "secret.system.read", "revoked",
		"revoke mail use", map[string]interface{}{"scope": "system"}, "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.broker.Use(context.Background(), fixture.request); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("revoked grant not denied: %v", err)
	}
	if fixture.egress.calls != 0 {
		t.Fatalf("denied calls reached egress %d times", fixture.egress.calls)
	}
}

func TestSecretUseBrokerChargesUnknownNetworkOutcome(t *testing.T) {
	fixture := newSecretBrokerFixture(t)
	fixture.egress.err = errors.New("network error may include fixture-token-do-not-return")
	if _, err := fixture.broker.Use(context.Background(), fixture.request); !errors.Is(err, ErrSecretUseFailed) ||
		strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("network failure leaked detail: %v", err)
	}
	if snapshot := fixture.budget.Snapshot(); snapshot.ConsumedUnits != 1 || snapshot.InFlight != 0 {
		t.Fatalf("unknown outcome did not charge reservation: %+v", snapshot)
	}
	operation, details := assertSecretUseAudit(t, fixture.audit)
	if operation.Status != reliability.OperationFailed || details.Outcome != "unknown" ||
		details.ReservedUnits != 1 || details.ChargedUnits == nil || *details.ChargedUnits != 1 ||
		details.ChargeKnown || details.ErrorCode != "egress_failed" {
		t.Fatalf("unknown use audit lost conservative charge: operation=%+v details=%+v", operation, details)
	}
}

type blockingSecretSender struct {
	entered chan struct{}
	resume  chan struct{}
}

func (s *blockingSecretSender) Do(ctx context.Context, request security.EgressRequest) (security.EgressResponse, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.resume:
		return security.EgressResponse{StatusCode: 204}, nil
	case <-ctx.Done():
		return security.EgressResponse{}, ctx.Err()
	}
}
func TestSecretUseBrokerResourceConcurrency(t *testing.T) {
	f := newSecretBrokerFixture(t)
	budget, err := security.NewPurposeBudget(f.request.Purpose, security.PurposeBudgetLimits{MaxUnits: 4, MaxConcurrency: 2, MaxDuration: time.Second, ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	f.broker.budgets = &testSecretPurposeBudget{budget: budget}
	sender := &blockingSecretSender{entered: make(chan struct{}, 2), resume: make(chan struct{})}
	f.broker.egress = sender
	done := make(chan error, 1)
	go func() { _, err := f.broker.Use(t.Context(), f.request); done <- err }()
	select {
	case <-sender.entered:
	case <-time.After(time.Second):
		t.Fatal("first call did not dispatch")
	}
	if _, err := f.broker.Use(t.Context(), f.request); !errors.Is(err, ErrSecretUseDenied) {
		t.Fatalf("resource concurrency 1 exceeded: %v", err)
	}
	close(sender.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := f.broker.Use(t.Context(), f.request); err != nil {
		t.Fatal("permit was not released")
	}
}
