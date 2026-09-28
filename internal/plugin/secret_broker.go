package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

var (
	ErrSecretUseDenied = errors.New("plugin secret use denied")
	ErrSecretUseFailed = errors.New("plugin secret use failed")
)

// Trusted host dispatch still has bounded input before any Body copy or
// budget reservation. The Egress transport may impose stricter limits.
const (
	secretUseMaxBodyBytes = 1 << 20
	secretUseMaxURLBytes  = 8 << 10
)

var (
	secretUseNamePattern    = regexp.MustCompile("^[a-z][a-z0-9_.-]{0,63}$")
	secretUseRefPattern     = regexp.MustCompile("^secret-ref:[a-z0-9-]{2,128}$")
	secretUseProfilePattern = regexp.MustCompile("^[A-Za-z][A-Za-z0-9_.-]{0,127}$")
	secretUsePurposePattern = regexp.MustCompile("^[A-Za-z][A-Za-z0-9_.-]{0,63}$")
)

// SecretRefBindingReader loads the current configuration from trusted host
// storage. A ref present in configuration is not itself an authorization.
type SecretRefBindingReader interface {
	IsBoundPluginV5SecretRef(context.Context, int64, *int64, string, string) (bool, error)
}

// SecretResourceDecisionProvider reloads the currently effective host
// resource decision for each use. It must derive facts from trusted host
// configuration and current grants, never from a plugin-provided decision.
type SecretResourceDecisionProvider interface {
	CurrentSecretResources(context.Context, int64, string, string) (security.ResourceDecision, error)
}

// SecretPurposeBudgetProvider gives the host-owned budget for the exact
// version and purpose. Budgets must not be shared across plugin instances.
type SecretPurposeBudgetProvider interface {
	CurrentSecretPurposeBudget(context.Context, int64, string) (*security.PurposeBudget, error)
}

type SecretEgressSender interface {
	Do(context.Context, security.EgressRequest) (security.EgressResponse, error)
}

// SecretUseRequest is accepted only from trusted host dispatch code. The
// plugin-facing Host API has no matching method and never receives plaintext.
type SecretUseRequest struct {
	PluginVersionID int64
	OwnerUserID     *int64
	SecretName      string
	SecretRef       string
	ProfileID       string
	TargetURL       string
	Purpose         string
	Body            []byte
}

// SecretUseResult intentionally does not expose request headers, response
// headers, response body, Secret names, refs, or the credential itself.
type SecretUseResult struct {
	StatusCode int
}

type SecretUseBroker struct {
	mu            sync.Mutex
	inFlight      map[int64]int64
	authorization *AuthorizationService
	refs          SecretRefBindingReader
	secrets       *SecretService
	resources     SecretResourceDecisionProvider
	budgets       SecretPurposeBudgetProvider
	egress        SecretEgressSender
	audit         SecretUseAuditStore
}

func NewSecretUseBroker(auth *AuthorizationService, refs SecretRefBindingReader, secrets *SecretService,
	resources SecretResourceDecisionProvider, budgets SecretPurposeBudgetProvider, egress SecretEgressSender,
	audit SecretUseAuditStore) (*SecretUseBroker, error) {
	if auth == nil || !auth.Available() || refs == nil || secrets == nil || !secrets.Available() ||
		resources == nil || budgets == nil || egress == nil || audit == nil {
		return nil, ErrSecretUseDenied
	}
	return &SecretUseBroker{
		authorization: auth, refs: refs, secrets: secrets,
		resources: resources, budgets: budgets, egress: egress, audit: audit, inFlight: make(map[int64]int64),
	}, nil
}

func validSecretUseRequest(request SecretUseRequest) bool {
	if request.PluginVersionID <= 0 || !secretUseNamePattern.MatchString(request.SecretName) ||
		!secretUseRefPattern.MatchString(request.SecretRef) ||
		!secretUseProfilePattern.MatchString(request.ProfileID) ||
		!secretUsePurposePattern.MatchString(request.Purpose) ||
		request.TargetURL == "" || len(request.TargetURL) > secretUseMaxURLBytes ||
		len(request.Body) > secretUseMaxBodyBytes || request.OwnerUserID != nil && *request.OwnerUserID <= 0 {
		return false
	}
	parsed, err := url.Parse(request.TargetURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.Fragment == "" && !strings.ContainsAny(request.TargetURL, "\r\n\x00")
}

func secretUseBinding(request SecretUseRequest) map[string]interface{} {
	return map[string]interface{}{
		"secret_name": request.SecretName, "secret_ref": request.SecretRef, "profile_id": request.ProfileID,
		"target_url": request.TargetURL, "purpose": request.Purpose,
	}
}

func secretUseTargetOrigin(target string) (string, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", ErrSecretUseDenied
	}
	return "https://" + parsed.Host, nil
}

// Use performs one host-mediated authenticated POST to the exact approved URL.
// The configuration ref, immutable active version, declaration, current
// Grant/Consent binding, resource target and purpose budget are checked for
// every call. A network error charges the reserved unit because dispatch may
// have happened. Only the response status is returned to the caller.
func (b *SecretUseBroker) Use(ctx context.Context, request SecretUseRequest) (result SecretUseResult, err error) {
	if b == nil || ctx == nil || !validSecretUseRequest(request) {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	version, err := b.authorization.store.VersionByID(ctx, request.PluginVersionID)
	if err != nil || version.ID != request.PluginVersionID || version.PluginID <= 0 ||
		b.authorization.RequireActiveVersion(ctx, version.PluginName, version.ID) != nil {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	bound, err := b.refs.IsBoundPluginV5SecretRef(ctx, version.ID, request.OwnerUserID, request.SecretName, request.SecretRef)
	if err != nil || !bound {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	origin, err := secretUseTargetOrigin(request.TargetURL)
	if err != nil {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	capabilityCode, scope := "secret.system.read", "system"
	actorUserID := ""
	if request.OwnerUserID != nil {
		capabilityCode, scope = "secret.self.read", "self"
		actorUserID = strconv.FormatInt(*request.OwnerUserID, 10)
	}
	decision, err := b.resources.CurrentSecretResources(ctx, version.ID, capabilityCode, request.Purpose)
	if err != nil || !decision.AllowsNetworkTarget(origin) || decision.Effective.TimeoutMS <= 0 || decision.Effective.MaxConcurrency <= 0 {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	authorized := b.authorization.Authorize(ctx, AuthorizationInput{
		PluginName: version.PluginName, PluginVersion: strconv.FormatInt(version.ID, 10),
		CapabilityCode: capabilityCode, OperationCode: "broker.secret.use",
		ActorUserID: actorUserID, ResourceOwnerID: actorUserID,
		ResourceScope: map[string]interface{}{"scope": scope, "secret_binding": secretUseBinding(request)},
	})
	if !authorized.Allow {
		return SecretUseResult{}, ErrSecretUseDenied
	}
	operation, details, auditErr := b.startUseAudit(ctx, request)
	if auditErr != nil {
		return SecretUseResult{}, ErrSecretUseFailed
	}
	// Persist completion even if the request context expires. Failed final
	// persistence denies a success response, but cannot retract an already sent
	// request. The initial running row remains for operator reconciliation;
	// retrying automatically could repeat a charged external side effect.
	defer func() {
		if finalErr := b.finishUseAudit(operation, details); finalErr != nil {
			result, err = SecretUseResult{}, ErrSecretUseFailed
		}
	}()
	details.Outcome, details.ChargeKnown = "denied", true
	details.ChargedUnits = secretUseUnits(0)
	if !b.admit(version.ID, decision.Effective.MaxConcurrency) {
		details.ErrorCode = "concurrency_exceeded"
		return SecretUseResult{}, ErrSecretUseDenied
	}
	defer b.release(version.ID)
	budget, err := b.budgets.CurrentSecretPurposeBudget(ctx, version.ID, request.Purpose)
	if err != nil || budget == nil || budget.Purpose() != request.Purpose {
		details.ErrorCode = "budget_unavailable"
		return SecretUseResult{}, ErrSecretUseDenied
	}
	lease, err := budget.Reserve(ctx, 1, time.Duration(decision.Effective.TimeoutMS)*time.Millisecond)
	if err != nil {
		details.ErrorCode = "budget_reserve_failed"
		return SecretUseResult{}, ErrSecretUseDenied
	}
	details.ReservedUnits = 1
	credential, err := b.secrets.Resolve(lease.Context(), version.PluginID, request.OwnerUserID, request.SecretName)
	if err != nil {
		details.ErrorCode = "secret_resolve_failed"
		if cancelErr := lease.Cancel(); cancelErr != nil {
			details.Outcome, details.ChargeKnown, details.ChargedUnits = "unknown", false, nil
			details.ErrorCode = "budget_cancel_failed"
		}
		return SecretUseResult{}, ErrSecretUseDenied
	}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("Authorization", "Bearer "+credential)
	// The plaintext exists only inside this trusted host path; neither this
	// request nor its response headers/body are returned to the caller.
	credential = ""
	response, sendErr := b.egress.Do(lease.Context(), security.EgressRequest{
		Method: http.MethodPost, URL: request.TargetURL, Headers: headers,
		Body: append([]byte(nil), request.Body...), DenyRedirects: true,
	})
	headers.Del("Authorization")
	if sendErr != nil {
		details.Outcome, details.ChargeKnown, details.ChargedUnits = "unknown", false, nil
		details.ErrorCode = "egress_failed"
		if chargeErr := lease.CommitUnknown(); chargeErr == nil {
			details.ChargedUnits = secretUseUnits(details.ReservedUnits)
		} else {
			details.ErrorCode = "budget_unknown_charge_failed"
		}
		return SecretUseResult{}, ErrSecretUseFailed
	}
	details.StatusCode = response.StatusCode
	if err := lease.Settle(1); err != nil {
		details.Outcome, details.ChargeKnown, details.ChargedUnits = "unknown", false, nil
		details.ErrorCode = "budget_settle_failed"
		return SecretUseResult{}, ErrSecretUseFailed
	}
	details.Outcome, details.ChargeKnown, details.ChargedUnits = "success", true, secretUseUnits(1)
	return SecretUseResult{StatusCode: response.StatusCode}, nil
}

// One host Broker instance owns the version-wide concurrency counter across
// all purposes. Distributed runtime budgets belong to the runtime adapters.
func (b *SecretUseBroker) admit(versionID, maximum int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if maximum <= 0 || b.inFlight[versionID] >= maximum {
		return false
	}
	b.inFlight[versionID]++
	return true
}
func (b *SecretUseBroker) release(versionID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight[versionID]--
	if b.inFlight[versionID] == 0 {
		delete(b.inFlight, versionID)
	}
}
