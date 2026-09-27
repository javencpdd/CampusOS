package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/google/uuid"
)

type AuthorizationReason string

const (
	ReasonAllow                 AuthorizationReason = "ALLOW"
	ReasonUnknownOperation      AuthorizationReason = "DENY_UNKNOWN_OPERATION"
	ReasonCapabilityNotDeclared AuthorizationReason = "DENY_CAPABILITY_NOT_DECLARED"
	ReasonAdminGrantMissing     AuthorizationReason = "DENY_ADMIN_GRANT_MISSING"
	ReasonUserConsentMissing    AuthorizationReason = "DENY_USER_CONSENT_MISSING"
	ReasonVersionMismatch       AuthorizationReason = "DENY_VERSION_MISMATCH"
	ReasonScopeMismatch         AuthorizationReason = "DENY_SCOPE_MISMATCH"
	ReasonPluginInactive        AuthorizationReason = "DENY_PLUGIN_INACTIVE"
	ReasonDelegationInvalid     AuthorizationReason = "DENY_DELEGATION_INVALID"
	ReasonSystemPolicy          AuthorizationReason = "DENY_SYSTEM_POLICY"
)

var ErrAuthorizationDenied = errors.New("plugin authorization denied")

type CapabilityDeclaration struct {
	// PostgreSQL identifiers are 64-bit Snowflake values. They must cross the
	// JSON/browser boundary as decimal strings: JavaScript Number silently loses
	// precision above Number.MAX_SAFE_INTEGER, which would otherwise turn a
	// valid declaration into a lookup for a different plugin version.
	ID                 int64                  `json:"id,string"`
	PluginVersionID    int64                  `json:"plugin_version_id,string"`
	CapabilityCode     string                 `json:"capability_code"`
	Purpose            string                 `json:"purpose"`
	RiskLevel          string                 `json:"risk_level"`
	Required           bool                   `json:"required"`
	ResourceScope      map[string]interface{} `json:"resource_scope"`
	DataClassification string                 `json:"data_classification"`
	CreatedAt          time.Time              `json:"created_at"`
}

type PluginVersion struct {
	ID                    int64                  `json:"id,string"`
	PluginID              int64                  `json:"plugin_id,string"`
	PluginName            string                 `json:"plugin_name"`
	Version               string                 `json:"version"`
	PackageDigest         string                 `json:"package_digest"`
	SignatureState        string                 `json:"signature_state"`
	Channel               string                 `json:"channel"`
	LifecycleStatus       string                 `json:"lifecycle_status"`
	ManifestAPIVersion    string                 `json:"manifest_api_version"`
	HostAPIVersion        string                 `json:"host_api_version"`
	PermissionFingerprint string                 `json:"permission_fingerprint"`
	Manifest              map[string]interface{} `json:"manifest"`
	CreatedAt             time.Time              `json:"created_at"`
}

type AdminGrant struct {
	ID              int64                  `json:"id,string"`
	PluginVersionID int64                  `json:"plugin_version_id,string"`
	CapabilityCode  string                 `json:"capability_code"`
	Status          string                 `json:"status"`
	GrantedScope    map[string]interface{} `json:"granted_scope"`
	PolicyRevision  int64                  `json:"policy_revision"`
	DecidedBy       *int64                 `json:"decided_by,string,omitempty"`
	Reason          string                 `json:"reason"`
	ExpiresAt       *time.Time             `json:"expires_at,omitempty"`
	SupersededAt    *time.Time             `json:"superseded_at,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type UserConsent struct {
	ID              int64                  `json:"id,string"`
	UserID          int64                  `json:"user_id,string"`
	PluginVersionID int64                  `json:"plugin_version_id,string"`
	CapabilityCode  string                 `json:"capability_code"`
	Status          string                 `json:"status"`
	ConsentScope    map[string]interface{} `json:"consent_scope"`
	PurposeHash     string                 `json:"purpose_hash"`
	PolicyRevision  int64                  `json:"policy_revision"`
	DecidedAt       time.Time              `json:"decided_at"`
	ExpiresAt       *time.Time             `json:"expires_at,omitempty"`
	RevokedAt       *time.Time             `json:"revoked_at,omitempty"`
	SupersededAt    *time.Time             `json:"superseded_at,omitempty"`
	Metadata        map[string]interface{} `json:"metadata"`
}

type Delegation struct {
	ID                  int64                  `json:"id,string"`
	PluginVersionID     int64                  `json:"plugin_version_id,string"`
	SubjectUserID       int64                  `json:"subject_user_id,string"`
	TokenDigest         string                 `json:"-"`
	GrantedCapabilities []string               `json:"granted_capabilities"`
	ResourceScope       map[string]interface{} `json:"resource_scope"`
	Status              string                 `json:"status"`
	NotBefore           time.Time              `json:"not_before"`
	ExpiresAt           time.Time              `json:"expires_at"`
	RevokedAt           *time.Time             `json:"revoked_at,omitempty"`
	CreatedBy           *int64                 `json:"created_by,string,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
}

type AuthorizationDecision struct {
	ID              int64                  `json:"id,string"`
	RequestID       string                 `json:"request_id"`
	PluginVersionID int64                  `json:"plugin_version_id,string"`
	UserID          *int64                 `json:"user_id,string,omitempty"`
	CapabilityCode  string                 `json:"capability_code"`
	OperationCode   string                 `json:"operation_code"`
	ResourceScope   map[string]interface{} `json:"resource_scope"`
	AdminGrantID    *int64                 `json:"admin_grant_id,string,omitempty"`
	UserConsentID   *int64                 `json:"user_consent_id,string,omitempty"`
	DelegationID    *int64                 `json:"delegation_id,string,omitempty"`
	Outcome         string                 `json:"outcome"`
	ReasonCode      AuthorizationReason    `json:"reason_code"`
	PolicyRevision  int64                  `json:"policy_revision"`
	TraceID         string                 `json:"trace_id"`
	Context         map[string]interface{} `json:"context"`
	CreatedAt       time.Time              `json:"created_at"`
}

type AuthorizationInput struct {
	PluginName      string                 `json:"plugin_name"`
	PluginVersion   string                 `json:"plugin_version"`
	CapabilityCode  string                 `json:"capability_code"`
	OperationCode   string                 `json:"operation_code"`
	ActorUserID     string                 `json:"actor_user_id,omitempty"`
	ResourceOwnerID string                 `json:"resource_owner_id,omitempty"`
	ResourceScope   map[string]interface{} `json:"resource_scope,omitempty"`
	DelegationToken string                 `json:"-"`
	Background      bool                   `json:"background"`
	TraceID         string                 `json:"trace_id,omitempty"`
}

type AuthorizationResult struct {
	Allow          bool                `json:"allow"`
	ReasonCode     AuthorizationReason `json:"reason_code"`
	Message        string              `json:"message"`
	PolicyRevision int64               `json:"policy_revision"`
	RequestID      string              `json:"request_id"`
	MatchedScope   string              `json:"matched_scope,omitempty"`
	AuditLevel     string              `json:"audit_level,omitempty"`
}

type AuthorizationOverview struct {
	Version      PluginVersion           `json:"version"`
	Declarations []CapabilityDeclaration `json:"declarations"`
	Catalog      []CapabilityDescriptor  `json:"catalog"`
	AdminGrants  []AdminGrant            `json:"admin_grants"`
	UserConsents []UserConsent           `json:"user_consents,omitempty"`
}

type AuthorizationStore interface {
	SyncVersion(context.Context, int64, *Manifest, string, string, []CapabilityDeclaration, *int64) (PluginVersion, error)
	ActiveVersion(context.Context, string) (PluginVersion, error)
	VersionByID(context.Context, int64) (PluginVersion, error)
	ListDeclarations(context.Context, int64) ([]CapabilityDeclaration, error)
	CurrentAdminGrant(context.Context, int64, string) (AdminGrant, error)
	ListAdminGrants(context.Context, int64) ([]AdminGrant, error)
	SetAdminGrant(context.Context, AdminGrant) (AdminGrant, error)
	CurrentUserConsent(context.Context, int64, int64, string) (UserConsent, error)
	ListUserConsents(context.Context, int64, int64) ([]UserConsent, error)
	SetUserConsent(context.Context, UserConsent) (UserConsent, error)
	CreateDelegation(context.Context, Delegation) (Delegation, error)
	DelegationByDigest(context.Context, string) (Delegation, error)
	RevokeDelegation(context.Context, int64, int64) error
	SaveAuthorizationDecision(context.Context, AuthorizationDecision) error
	ListAuthorizationDecisions(context.Context, int64, int) ([]AuthorizationDecision, error)
}

type AuthorizationActiveResolver func(string) bool

type AuthorizationService struct {
	store  AuthorizationStore
	repo   PluginRepository
	active AuthorizationActiveResolver
	now    func() time.Time
}

func NewAuthorizationService(store AuthorizationStore, repo PluginRepository, active AuthorizationActiveResolver) *AuthorizationService {
	return &AuthorizationService{store: store, repo: repo, active: active, now: time.Now}
}

func (s *AuthorizationService) Available() bool { return s != nil && s.store != nil && s.repo != nil }

func (s *AuthorizationService) ActiveVersion(ctx context.Context, pluginName string) (PluginVersion, error) {
	if !s.Available() {
		return PluginVersion{}, errors.New("plugin authorization service is unavailable")
	}
	return s.store.ActiveVersion(ctx, pluginName)
}

// RequireActiveVersion binds a version ID to the named plugin's current release.
func (s *AuthorizationService) RequireActiveVersion(ctx context.Context, pluginName string, versionID int64) error {
	if pluginName == "" {
		return errors.New("version mismatch")
	}
	_, err := s.requireActiveVersion(ctx, pluginName, versionID)
	return err
}

func (s *AuthorizationService) requireActiveVersion(ctx context.Context, pluginName string, versionID int64) (PluginVersion, error) {
	if !s.Available() || versionID <= 0 {
		return PluginVersion{}, errors.New("version mismatch")
	}
	version, err := s.store.VersionByID(ctx, versionID)
	if err != nil || version.PluginName == "" || version.LifecycleStatus != "active" ||
		(pluginName != "" && version.PluginName != pluginName) {
		return PluginVersion{}, errors.New("version mismatch")
	}
	active, err := s.store.ActiveVersion(ctx, version.PluginName)
	if err != nil || active.ID != version.ID || active.LifecycleStatus != "active" {
		return PluginVersion{}, errors.New("version mismatch")
	}
	return version, nil
}

func (s *AuthorizationService) SyncInstalled(ctx context.Context, installed *Plugin, actorID string) (PluginVersion, error) {
	if !s.Available() || installed == nil || installed.Manifest == nil {
		return PluginVersion{}, errors.New("plugin authorization service is unavailable")
	}
	record, err := s.repo.GetByName(ctx, installed.Manifest.Name)
	if err != nil {
		return PluginVersion{}, err
	}
	digest := strings.ToLower(strings.TrimSpace(installed.Checksum))
	if len(digest) != 64 {
		digest = digestJSON(installed.Manifest)
	}
	declarations, err := declarationsForManifest(installed.Manifest)
	if err != nil {
		return PluginVersion{}, err
	}
	fingerprint := declarationFingerprint(declarations)
	var actor *int64
	if value, parseErr := strconv.ParseInt(actorID, 10, 64); parseErr == nil && value > 0 {
		actor = &value
	}
	return s.store.SyncVersion(ctx, record.ID, installed.Manifest, digest, fingerprint, declarations, actor)
}

func declarationsForManifest(manifest *Manifest) ([]CapabilityDeclaration, error) {
	if manifest == nil {
		return nil, errors.New("manifest is required")
	}
	result := []CapabilityDeclaration{}
	seen := map[string]bool{}
	if manifest.IsV3() {
		for _, request := range manifest.CapabilityDeclarations {
			descriptor, ok := CapabilityByCode(request.Code)
			if !ok {
				return nil, fmt.Errorf("unknown capability %s", request.Code)
			}
			result = append(result, CapabilityDeclaration{CapabilityCode: descriptor.Code, Purpose: strings.TrimSpace(request.Purpose), RiskLevel: descriptor.Risk, Required: request.Required, ResourceScope: map[string]interface{}{"scope": descriptor.Scope, "limits": request.Limits}, DataClassification: descriptor.DataClassification})
			seen[descriptor.Code] = true
		}
	} else {
		for _, permission := range manifest.Permissions.API {
			for _, action := range permission.Actions {
				descriptor, ok := CapabilityForPermission(permission.Resource, action)
				if !ok || seen[descriptor.Code] {
					continue
				}
				result = append(result, CapabilityDeclaration{CapabilityCode: descriptor.Code, Purpose: "兼容 Manifest v1/v2 的 Host API 权限声明", RiskLevel: descriptor.Risk, Required: true, ResourceScope: map[string]interface{}{"scope": descriptor.Scope, "compatibility": true}, DataClassification: descriptor.DataClassification})
				seen[descriptor.Code] = true
			}
		}
		for _, permission := range manifest.Permissions.User {
			for _, action := range permission.Actions {
				code := legacyUserCapabilityCode(permission.Resource, action)
				descriptor, ok := CapabilityByCode(code)
				if !ok || seen[code] {
					continue
				}
				result = append(result, CapabilityDeclaration{CapabilityCode: code, Purpose: permission.Purpose, RiskLevel: descriptor.Risk, Required: false, ResourceScope: map[string]interface{}{"scope": "self", "compatibility": true}, DataClassification: descriptor.DataClassification})
				seen[code] = true
			}
		}
		if len(manifest.Events.Subscribe) > 0 && !seen["event.system.subscribe"] {
			descriptor, _ := CapabilityByCode("event.system.subscribe")
			result = append(result, CapabilityDeclaration{CapabilityCode: descriptor.Code, Purpose: "兼容 Manifest v1/v2 的事件订阅声明", RiskLevel: descriptor.Risk, Required: true, ResourceScope: map[string]interface{}{"scope": descriptor.Scope, "events": manifest.Events.Subscribe, "compatibility": true}, DataClassification: descriptor.DataClassification})
			seen[descriptor.Code] = true
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CapabilityCode < result[j].CapabilityCode })
	return result, nil
}

func legacyUserCapabilityCode(resource, action string) string {
	switch resource {
	case ManagedDataResource:
		return "plugin_record.self." + action
	case PluginFileResource:
		return "plugin_file.self." + action
	case PluginSearchResource:
		return "plugin_search.self." + action
	default:
		return ""
	}
}

func declarationFingerprint(items []CapabilityDeclaration) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("%s\x00%t\x00%s\x00%s", item.CapabilityCode, item.Required, item.Purpose, canonicalJSON(item.ResourceScope)))
	}
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return hex.EncodeToString(sum[:])
}

func PurposeHash(purpose string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(purpose)))
	return hex.EncodeToString(sum[:])
}

func digestJSON(value interface{}) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(value interface{}) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func (s *AuthorizationService) Overview(ctx context.Context, pluginName string, userID string) (AuthorizationOverview, error) {
	version, err := s.store.ActiveVersion(ctx, pluginName)
	if err != nil {
		return AuthorizationOverview{}, err
	}
	declarations, err := s.store.ListDeclarations(ctx, version.ID)
	if err != nil {
		return AuthorizationOverview{}, err
	}
	grants, err := s.store.ListAdminGrants(ctx, version.ID)
	if err != nil {
		return AuthorizationOverview{}, err
	}
	catalog := make([]CapabilityDescriptor, 0, len(declarations))
	for _, declaration := range declarations {
		if descriptor, ok := CapabilityByCode(declaration.CapabilityCode); ok {
			catalog = append(catalog, descriptor)
		}
	}
	result := AuthorizationOverview{Version: version, Declarations: declarations, Catalog: catalog, AdminGrants: grants}
	if parsed, parseErr := strconv.ParseInt(userID, 10, 64); parseErr == nil && parsed > 0 {
		result.UserConsents, err = s.store.ListUserConsents(ctx, parsed, version.ID)
	}
	return result, err
}

func (s *AuthorizationService) SetAdminGrant(ctx context.Context, versionID int64, capabilityCode, status, reason string, scope map[string]interface{}, actorID string, expiresAt *time.Time) (AdminGrant, error) {
	if status != "granted" && status != "denied" && status != "revoked" {
		return AdminGrant{}, errors.New("admin grant status must be granted, denied or revoked")
	}
	if strings.TrimSpace(reason) == "" {
		return AdminGrant{}, errors.New("admin grant reason is required")
	}
	descriptor, ok := CapabilityByCode(capabilityCode)
	if !ok {
		return AdminGrant{}, fmt.Errorf("unknown capability %s", capabilityCode)
	}
	declarations, err := s.store.ListDeclarations(ctx, versionID)
	if err != nil {
		return AdminGrant{}, err
	}
	declared := false
	for _, item := range declarations {
		if item.CapabilityCode == capabilityCode {
			declared = true
			break
		}
	}
	if !declared {
		return AdminGrant{}, fmt.Errorf("capability %s is not declared by this version", capabilityCode)
	}
	if requested, exists := scope["scope"]; exists && fmt.Sprint(requested) != descriptor.Scope {
		return AdminGrant{}, fmt.Errorf("capability %s scope cannot exceed %s", capabilityCode, descriptor.Scope)
	}
	var actor *int64
	if parsed, err := strconv.ParseInt(actorID, 10, 64); err == nil && parsed > 0 {
		actor = &parsed
	}
	if _, err := s.requireActiveVersion(ctx, "", versionID); err != nil {
		return AdminGrant{}, err
	}
	return s.store.SetAdminGrant(ctx, AdminGrant{ID: idgen.New(), PluginVersionID: versionID, CapabilityCode: capabilityCode, Status: status, GrantedScope: nonNilMap(scope), PolicyRevision: time.Now().UTC().UnixNano(), DecidedBy: actor, Reason: strings.TrimSpace(reason), ExpiresAt: expiresAt, CreatedAt: s.now(), UpdatedAt: s.now()})
}

func (s *AuthorizationService) SetUserConsent(ctx context.Context, userID string, versionID int64, capabilityCode, status string, scope map[string]interface{}) (UserConsent, error) {
	parsed, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || parsed <= 0 {
		return UserConsent{}, errors.New("valid authenticated user is required")
	}
	if status != "granted" && status != "denied" && status != "revoked" {
		return UserConsent{}, errors.New("user consent status must be granted, denied or revoked")
	}
	declarations, err := s.store.ListDeclarations(ctx, versionID)
	if err != nil {
		return UserConsent{}, err
	}
	var declaration *CapabilityDeclaration
	for index := range declarations {
		if declarations[index].CapabilityCode == capabilityCode {
			declaration = &declarations[index]
			break
		}
	}
	if declaration == nil {
		return UserConsent{}, fmt.Errorf("capability %s is not declared by this version", capabilityCode)
	}
	descriptor, ok := CapabilityByCode(capabilityCode)
	if !ok || !descriptor.ConsentRequired {
		return UserConsent{}, fmt.Errorf("capability %s does not accept user consent", capabilityCode)
	}
	if requested, exists := scope["scope"]; exists && fmt.Sprint(requested) != descriptor.Scope {
		return UserConsent{}, fmt.Errorf("capability %s consent scope cannot exceed %s", capabilityCode, descriptor.Scope)
	}
	now := s.now()
	consent := UserConsent{ID: idgen.New(), UserID: parsed, PluginVersionID: versionID, CapabilityCode: capabilityCode, Status: status, ConsentScope: nonNilMap(scope), PurposeHash: PurposeHash(declaration.Purpose), PolicyRevision: now.UTC().UnixNano(), DecidedAt: now, Metadata: map[string]interface{}{}}
	if status == "revoked" {
		consent.RevokedAt = &now
	}
	if _, err := s.requireActiveVersion(ctx, "", versionID); err != nil {
		return UserConsent{}, err
	}
	return s.store.SetUserConsent(ctx, consent)
}

func (s *AuthorizationService) IssueDelegation(ctx context.Context, pluginName, userID string, versionID int64, capabilities []string, scope map[string]interface{}, ttl time.Duration) (Delegation, string, error) {
	parsed, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || parsed <= 0 {
		return Delegation{}, "", errors.New("valid authenticated user is required")
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		return Delegation{}, "", errors.New("delegation ttl must be greater than zero and at most 24 hours")
	}
	capabilities = uniqueStrings(capabilities)
	if len(capabilities) == 0 {
		return Delegation{}, "", errors.New("delegation requires at least one capability")
	}
	if err := s.RequireActiveVersion(ctx, pluginName, versionID); err != nil {
		return Delegation{}, "", err
	}
	for _, code := range capabilities {
		result := s.Authorize(ctx, AuthorizationInput{PluginName: pluginName, PluginVersion: strconv.FormatInt(versionID, 10), CapabilityCode: code, OperationCode: "delegation.issue", ActorUserID: userID, ResourceOwnerID: userID})
		if !result.Allow {
			return Delegation{}, "", fmt.Errorf("%w: %s", ErrAuthorizationDenied, result.ReasonCode)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Delegation{}, "", err
	}
	token := "cosd_" + hex.EncodeToString(raw)
	if err := s.RequireActiveVersion(ctx, pluginName, versionID); err != nil {
		return Delegation{}, "", err
	}
	now := s.now()
	delegation := Delegation{ID: idgen.New(), PluginVersionID: versionID, SubjectUserID: parsed, TokenDigest: tokenDigest(token), GrantedCapabilities: capabilities, ResourceScope: nonNilMap(scope), Status: "active", NotBefore: now, ExpiresAt: now.Add(ttl), CreatedBy: &parsed, CreatedAt: now}
	created, err := s.store.CreateDelegation(ctx, delegation)
	return created, token, err
}

func (s *AuthorizationService) RevokeDelegation(ctx context.Context, userID string, delegationID int64) error {
	parsed, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || parsed <= 0 {
		return errors.New("valid authenticated user is required")
	}
	return s.store.RevokeDelegation(ctx, delegationID, parsed)
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (s *AuthorizationService) Authorize(ctx context.Context, input AuthorizationInput) AuthorizationResult {
	requestID := uuid.NewString()
	if input.TraceID == "" {
		input.TraceID = requestID
	}
	result := AuthorizationResult{ReasonCode: ReasonSystemPolicy, Message: authorizationMessage(ReasonSystemPolicy), PolicyRevision: 1, RequestID: requestID}
	if !s.Available() {
		return result
	}
	descriptor, known := CapabilityByCode(input.CapabilityCode)
	if !known || strings.TrimSpace(input.OperationCode) == "" {
		result.ReasonCode = ReasonUnknownOperation
		result.Message = authorizationMessage(result.ReasonCode)
		return result
	}
	version, err := s.resolveVersion(ctx, input)
	if err != nil {
		result.ReasonCode = ReasonVersionMismatch
		result.Message = authorizationMessage(result.ReasonCode)
		return result
	}
	decision := AuthorizationDecision{ID: idgen.New(), RequestID: requestID, PluginVersionID: version.ID, CapabilityCode: input.CapabilityCode, OperationCode: input.OperationCode, ResourceScope: nonNilMap(input.ResourceScope), Outcome: "deny", ReasonCode: ReasonSystemPolicy, PolicyRevision: 1, TraceID: input.TraceID, Context: map[string]interface{}{}, CreatedAt: s.now()}
	declarations, err := s.store.ListDeclarations(ctx, version.ID)
	if err != nil {
		return s.finishDecision(ctx, decision, result, ReasonSystemPolicy, descriptor)
	}
	var declaration *CapabilityDeclaration
	for index := range declarations {
		if declarations[index].CapabilityCode == input.CapabilityCode {
			declaration = &declarations[index]
			break
		}
	}
	if declaration == nil {
		return s.finishDecision(ctx, decision, result, ReasonCapabilityNotDeclared, descriptor)
	}
	if s.active != nil && !s.active(version.PluginName) {
		return s.finishDecision(ctx, decision, result, ReasonPluginInactive, descriptor)
	}
	grant, err := s.store.CurrentAdminGrant(ctx, version.ID, input.CapabilityCode)
	if err != nil || grant.Status != "granted" || expired(grant.ExpiresAt, s.now()) {
		return s.finishDecision(ctx, decision, result, ReasonAdminGrantMissing, descriptor)
	}
	decision.AdminGrantID = &grant.ID
	decision.PolicyRevision = grant.PolicyRevision
	if !scopeContains(grant.GrantedScope, input.ResourceScope) {
		return s.finishDecision(ctx, decision, result, ReasonScopeMismatch, descriptor)
	}
	actorID, _ := strconv.ParseInt(input.ActorUserID, 10, 64)
	if input.Background || input.DelegationToken != "" {
		delegation, delegationErr := s.store.DelegationByDigest(ctx, tokenDigest(input.DelegationToken))
		if delegationErr != nil || delegation.PluginVersionID != version.ID || delegation.Status != "active" || s.now().Before(delegation.NotBefore) || !s.now().Before(delegation.ExpiresAt) || !containsAuthorizationString(delegation.GrantedCapabilities, input.CapabilityCode) {
			return s.finishDecision(ctx, decision, result, ReasonDelegationInvalid, descriptor)
		}
		if !scopeContains(delegation.ResourceScope, input.ResourceScope) {
			return s.finishDecision(ctx, decision, result, ReasonDelegationInvalid, descriptor)
		}
		actorID = delegation.SubjectUserID
		input.ActorUserID = strconv.FormatInt(actorID, 10)
		decision.DelegationID = &delegation.ID
	}
	if descriptor.Scope == "self" {
		if actorID <= 0 || (input.ResourceOwnerID != "" && input.ResourceOwnerID != input.ActorUserID) {
			return s.finishDecision(ctx, decision, result, ReasonScopeMismatch, descriptor)
		}
		decision.UserID = &actorID
	}
	if descriptor.ConsentRequired {
		consent, consentErr := s.store.CurrentUserConsent(ctx, actorID, version.ID, input.CapabilityCode)
		if consentErr != nil || consent.Status != "granted" || expired(consent.ExpiresAt, s.now()) || consent.PurposeHash != PurposeHash(declaration.Purpose) {
			return s.finishDecision(ctx, decision, result, ReasonUserConsentMissing, descriptor)
		}
		decision.UserConsentID = &consent.ID
		if !scopeContains(consent.ConsentScope, input.ResourceScope) {
			return s.finishDecision(ctx, decision, result, ReasonScopeMismatch, descriptor)
		}
		if consent.PolicyRevision > decision.PolicyRevision {
			decision.PolicyRevision = consent.PolicyRevision
		}
	}
	// A release can change while grants and consent are being read. Recheck the
	// selected version before returning an allow decision.
	if err := s.RequireActiveVersion(ctx, version.PluginName, version.ID); err != nil {
		return s.finishDecision(ctx, decision, result, ReasonVersionMismatch, descriptor)
	}
	decision.Outcome = "allow"
	return s.finishDecision(ctx, decision, result, ReasonAllow, descriptor)
}

func (s *AuthorizationService) resolveVersion(ctx context.Context, input AuthorizationInput) (PluginVersion, error) {
	if id, err := strconv.ParseInt(input.PluginVersion, 10, 64); err == nil && id > 0 {
		return s.requireActiveVersion(ctx, input.PluginName, id)
	}
	version, err := s.store.ActiveVersion(ctx, input.PluginName)
	if err != nil || version.PluginName != input.PluginName || version.LifecycleStatus != "active" ||
		(input.PluginVersion != "" && version.Version != input.PluginVersion) {
		return PluginVersion{}, errors.New("version mismatch")
	}
	return version, nil
}

func (s *AuthorizationService) finishDecision(ctx context.Context, decision AuthorizationDecision, result AuthorizationResult, reason AuthorizationReason, descriptor CapabilityDescriptor) AuthorizationResult {
	decision.ReasonCode = reason
	if reason == ReasonAllow {
		decision.Outcome = "allow"
	}
	if err := s.store.SaveAuthorizationDecision(ctx, decision); err != nil && reason == ReasonAllow {
		result.Allow = false
		result.ReasonCode = ReasonSystemPolicy
		result.Message = authorizationMessage(ReasonSystemPolicy)
		return result
	}
	result.Allow = reason == ReasonAllow
	result.ReasonCode = reason
	result.Message = authorizationMessage(reason)
	result.PolicyRevision = decision.PolicyRevision
	result.MatchedScope = descriptor.Scope
	result.AuditLevel = descriptor.AuditLevel
	return result
}

func (s *AuthorizationService) Decisions(ctx context.Context, pluginName string, limit int) ([]AuthorizationDecision, error) {
	version, err := s.store.ActiveVersion(ctx, pluginName)
	if err != nil {
		return nil, err
	}
	return s.store.ListAuthorizationDecisions(ctx, version.ID, limit)
}

func authorizationMessage(reason AuthorizationReason) string {
	return map[AuthorizationReason]string{
		ReasonAllow:                 "授权通过",
		ReasonUnknownOperation:      "该操作未纳入插件能力目录，系统已默认拒绝。",
		ReasonCapabilityNotDeclared: "插件当前版本没有声明该能力。",
		ReasonAdminGrantMissing:     "管理员尚未授予该能力，或授权已撤销/过期。",
		ReasonUserConsentMissing:    "你尚未同意该数据用途，或同意已撤销/过期/用途发生变化。",
		ReasonVersionMismatch:       "调用所使用的插件版本与当前授权版本不一致。",
		ReasonScopeMismatch:         "请求的数据范围超出当前用户或系统授权范围。",
		ReasonPluginInactive:        "插件已停用、隔离、停止或运行异常。",
		ReasonDelegationInvalid:     "后台任务委托无效、已过期、已撤销或超出能力范围。",
		ReasonSystemPolicy:          "系统安全策略拒绝了该请求，请联系管理员查看诊断信息。",
	}[reason]
}

func nonNilMap(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
}

func expired(value *time.Time, now time.Time) bool { return value != nil && !now.Before(*value) }
func containsAuthorizationString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func scopeContains(granted, requested map[string]interface{}) bool {
	if len(requested) == 0 || len(granted) == 0 {
		return true
	}
	for key, value := range requested {
		if key == "scope" {
			continue
		}
		allowed, exists := granted[key]
		if !exists || canonicalJSON(allowed) != canonicalJSON(value) {
			return false
		}
	}
	return true
}

// MemoryAuthorizationStore keeps the same policy semantics for tests and the
// explicit no-PostgreSQL development profile.
type MemoryAuthorizationStore struct {
	mu           sync.RWMutex
	versions     map[int64]PluginVersion
	active       map[string]int64
	declarations map[int64][]CapabilityDeclaration
	admin        map[string]AdminGrant
	consents     map[string]UserConsent
	delegations  map[string]Delegation
	decisions    []AuthorizationDecision
}

func NewMemoryAuthorizationStore() *MemoryAuthorizationStore {
	return &MemoryAuthorizationStore{versions: map[int64]PluginVersion{}, active: map[string]int64{}, declarations: map[int64][]CapabilityDeclaration{}, admin: map[string]AdminGrant{}, consents: map[string]UserConsent{}, delegations: map[string]Delegation{}}
}

func (m *MemoryAuthorizationStore) SyncVersion(_ context.Context, pluginID int64, manifest *Manifest, digest, fingerprint string, declarations []CapabilityDeclaration, _ *int64) (PluginVersion, error) {
	if manifest == nil {
		return PluginVersion{}, errors.New("manifest is required")
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return PluginVersion{}, fmt.Errorf("marshal plugin manifest: %w", err)
	}
	manifestMap := map[string]interface{}{}
	if err := json.Unmarshal(manifestJSON, &manifestMap); err != nil {
		return PluginVersion{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, version := range m.versions {
		if version.PluginID != pluginID {
			continue
		}
		if version.Version != manifest.Version {
			if version.PackageDigest == digest {
				return PluginVersion{}, fmt.Errorf("plugin package digest is already assigned to version %s", version.Version)
			}
			continue
		}
		if version.PackageDigest != digest || version.PermissionFingerprint != fingerprint ||
			version.ManifestAPIVersion != manifest.APIVersion || version.HostAPIVersion != manifest.HostAPIVersion ||
			!reflect.DeepEqual(version.Manifest, manifestMap) {
			return PluginVersion{}, fmt.Errorf("plugin version %s is immutable: package digest, capability fingerprint or manifest changed", manifest.Version)
		}
		m.retireActiveVersions(pluginID, id)
		version.LifecycleStatus = "active"
		m.versions[id], m.active[manifest.Name] = version, id
		return clonePluginVersion(version), nil
	}
	id := idgen.New()
	version := PluginVersion{ID: id, PluginID: pluginID, PluginName: manifest.Name, Version: manifest.Version, PackageDigest: digest, SignatureState: "unsigned", Channel: "stable", LifecycleStatus: "active", ManifestAPIVersion: manifest.APIVersion, HostAPIVersion: manifest.HostAPIVersion, PermissionFingerprint: fingerprint, Manifest: manifestMap, CreatedAt: time.Now()}
	m.retireActiveVersions(pluginID, 0)
	m.versions[id], m.active[manifest.Name] = version, id
	m.setDeclarations(id, declarations)
	return clonePluginVersion(version), nil
}

func (m *MemoryAuthorizationStore) retireActiveVersions(pluginID, exceptID int64) {
	for id, version := range m.versions {
		if version.PluginID == pluginID && id != exceptID && version.LifecycleStatus == "active" {
			version.LifecycleStatus = "retired"
			m.versions[id] = version
			retiredAt := time.Now()
			for digest, delegation := range m.delegations {
				if delegation.PluginVersionID == id && delegation.Status == "active" {
					delegation.Status = "revoked"
					delegation.RevokedAt = &retiredAt
					m.delegations[digest] = delegation
				}
			}
		}
	}
}

func (m *MemoryAuthorizationStore) setDeclarations(versionID int64, items []CapabilityDeclaration) {
	copyItems := make([]CapabilityDeclaration, len(items))
	for index := range items {
		copyItems[index] = items[index]
		copyItems[index].ResourceScope = cloneAuthorizationMap(items[index].ResourceScope)
		copyItems[index].ID = idgen.New()
		copyItems[index].PluginVersionID = versionID
		copyItems[index].CreatedAt = time.Now()
	}
	m.declarations[versionID] = copyItems
}

func cloneAuthorizationMap(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	encoded, _ := json.Marshal(value)
	var clone map[string]interface{}
	_ = json.Unmarshal(encoded, &clone)
	return clone
}

func clonePluginVersion(value PluginVersion) PluginVersion {
	value.Manifest = cloneAuthorizationMap(value.Manifest)
	return value
}

func (m *MemoryAuthorizationStore) ActiveVersion(_ context.Context, name string) (PluginVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.versions[m.active[name]]
	if !ok {
		return PluginVersion{}, ErrMarketNotFound
	}
	return clonePluginVersion(value), nil
}
func (m *MemoryAuthorizationStore) VersionByID(_ context.Context, id int64) (PluginVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.versions[id]
	if !ok {
		return PluginVersion{}, ErrMarketNotFound
	}
	return clonePluginVersion(value), nil
}
func (m *MemoryAuthorizationStore) ListDeclarations(_ context.Context, id int64) ([]CapabilityDeclaration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := append([]CapabilityDeclaration(nil), m.declarations[id]...)
	for index := range result {
		result[index].ResourceScope = cloneAuthorizationMap(result[index].ResourceScope)
	}
	return result, nil
}
func grantKey(versionID int64, code string) string {
	return strconv.FormatInt(versionID, 10) + ":" + code
}
func consentKey(userID, versionID int64, code string) string {
	return fmt.Sprintf("%d:%d:%s", userID, versionID, code)
}
func (m *MemoryAuthorizationStore) CurrentAdminGrant(_ context.Context, versionID int64, code string) (AdminGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.admin[grantKey(versionID, code)]
	if !ok {
		return AdminGrant{}, ErrMarketNotFound
	}
	return value, nil
}
func (m *MemoryAuthorizationStore) ListAdminGrants(_ context.Context, versionID int64) ([]AdminGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []AdminGrant{}
	for _, v := range m.admin {
		if v.PluginVersionID == versionID {
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CapabilityCode < result[j].CapabilityCode })
	return result, nil
}
func (m *MemoryAuthorizationStore) requireActiveVersionLocked(versionID int64) error {
	version, ok := m.versions[versionID]
	if !ok || version.LifecycleStatus != "active" || m.active[version.PluginName] != versionID {
		return errors.New("version mismatch")
	}
	return nil
}

func (m *MemoryAuthorizationStore) SetAdminGrant(_ context.Context, value AdminGrant) (AdminGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireActiveVersionLocked(value.PluginVersionID); err != nil {
		return AdminGrant{}, err
	}
	if previous, ok := m.admin[grantKey(value.PluginVersionID, value.CapabilityCode)]; ok {
		value.PolicyRevision = previous.PolicyRevision + 1
	}
	m.admin[grantKey(value.PluginVersionID, value.CapabilityCode)] = value
	return value, nil
}
func (m *MemoryAuthorizationStore) CurrentUserConsent(_ context.Context, userID, versionID int64, code string) (UserConsent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.consents[consentKey(userID, versionID, code)]
	if !ok {
		return UserConsent{}, ErrMarketNotFound
	}
	return value, nil
}
func (m *MemoryAuthorizationStore) ListUserConsents(_ context.Context, userID, versionID int64) ([]UserConsent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []UserConsent{}
	for _, v := range m.consents {
		if v.UserID == userID && v.PluginVersionID == versionID {
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CapabilityCode < result[j].CapabilityCode })
	return result, nil
}
func (m *MemoryAuthorizationStore) SetUserConsent(_ context.Context, value UserConsent) (UserConsent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireActiveVersionLocked(value.PluginVersionID); err != nil {
		return UserConsent{}, err
	}
	key := consentKey(value.UserID, value.PluginVersionID, value.CapabilityCode)
	if previous, ok := m.consents[key]; ok {
		value.PolicyRevision = previous.PolicyRevision + 1
	}
	m.consents[key] = value
	return value, nil
}
func (m *MemoryAuthorizationStore) CreateDelegation(_ context.Context, value Delegation) (Delegation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireActiveVersionLocked(value.PluginVersionID); err != nil {
		return Delegation{}, err
	}
	m.delegations[value.TokenDigest] = value
	return value, nil
}
func (m *MemoryAuthorizationStore) DelegationByDigest(_ context.Context, digest string) (Delegation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.delegations[digest]
	if !ok {
		return Delegation{}, ErrMarketNotFound
	}
	return value, nil
}
func (m *MemoryAuthorizationStore) RevokeDelegation(_ context.Context, id, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range m.delegations {
		if value.ID == id && value.SubjectUserID == userID {
			now := time.Now()
			value.Status = "revoked"
			value.RevokedAt = &now
			m.delegations[key] = value
			return nil
		}
	}
	return ErrMarketNotFound
}
func (m *MemoryAuthorizationStore) SaveAuthorizationDecision(_ context.Context, value AuthorizationDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions = append(m.decisions, value)
	return nil
}
func (m *MemoryAuthorizationStore) ListAuthorizationDecisions(_ context.Context, versionID int64, limit int) ([]AuthorizationDecision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	result := []AuthorizationDecision{}
	for i := len(m.decisions) - 1; i >= 0 && len(result) < limit; i-- {
		if m.decisions[i].PluginVersionID == versionID {
			result = append(result, m.decisions[i])
		}
	}
	return result, nil
}
