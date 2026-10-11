package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrAdminDomainUnavailable = errors.New("admin domain service is unavailable")
	ErrAdminSelfTransition    = errors.New("admin cannot suspend or revoke their own account")
)

// AdminActor is the host-verified admin-domain caller of a lifecycle command.
// The entry must have authenticated the admin session and MFA already.
type AdminActor struct {
	ID               string
	MFAAuthenticated bool
	CredentialID     string
}

// AdminDomainConfig carries the deployment's identity domain separation facts.
// Two domains sharing any value are rejected by the policy evaluation.
type AdminDomainConfig struct {
	User  port.IdentityDomainConfig
	Admin port.IdentityDomainConfig
	// BootstrapEnabled gates the controlled first-security-admin entry.
	BootstrapEnabled bool
}

// DefaultAdminDomainConfig returns the v1.2 default separation; deployments
// override the values via configuration, never by editing call sites.
func DefaultAdminDomainConfig() AdminDomainConfig {
	domain := func(kind string) port.IdentityDomainConfig {
		return port.IdentityDomainConfig{
			Issuer: "campusos-" + kind, Audience: kind + "-api", KeyID: kind + "-key",
			CredentialStore: kind + "-credentials", SessionStore: kind + "-sessions",
			RecoveryStore: kind + "-recovery", RoleStore: kind + "-roles",
		}
	}
	return AdminDomainConfig{User: domain("user"), Admin: domain("admin")}
}

// AdminDomainService runs the admin account lifecycle on the independent
// admin domain stores. Every command evaluates the frozen identity-domain
// policy against freshly loaded facts and commits with its required audit in
// one transaction.
type AdminDomainService struct {
	admins   repository.AdminDomainRepository
	catalog  repository.AuthorizationRepository
	reliable *reliability.Service
	now      func() time.Time
	config   AdminDomainConfig
	policy   port.IdentityDomainPolicy
}

func NewAdminDomainService(admins repository.AdminDomainRepository, catalog repository.AuthorizationRepository, config AdminDomainConfig) *AdminDomainService {
	return &AdminDomainService{
		admins:  admins,
		catalog: catalog,
		now:     time.Now,
		config:  config,
		policy:  policy.IdentityDomainPolicy{},
	}
}

func (s *AdminDomainService) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.admins.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// SetClock replaces the wall clock in tests; production keeps time.Now.
func (s *AdminDomainService) SetClock(now func() time.Time) { s.now = now }

func newAdminID() string { return "adm-" + strconv.FormatInt(idgen.New(), 10) }

// lifecycleInput builds the host-constructed evaluation input for one admin
// lifecycle operation. The request half describes the admin domain's own
// credential binding; only current facts and the operation vary.
func (s *AdminDomainService) lifecycleInput(accountID, activationSource string, op port.IdentityDomainOperation) port.IdentityDomainInput {
	admin := s.config.Admin
	return port.IdentityDomainInput{
		Contract: port.IdentityDomainsContract,
		Domains:  port.IdentityDomainPair{User: s.config.User, Admin: admin},
		Request: port.IdentityDomainRequest{
			Kind: "admin", AccountID: accountID, Issuer: admin.Issuer, Audience: admin.Audience,
			KeyID: admin.KeyID, CredentialStore: admin.CredentialStore, SessionStore: admin.SessionStore,
			ChallengeKind: "access", ChallengeDomain: "admin", TargetAccountID: accountID,
			Status: "active", ActivationSource: activationSource,
		},
		Operation: &op,
	}
}

// evaluateLifecycle loads the current security-admin count and evaluates the
// frozen policy. Callers run it once before and once inside the transaction.
func (s *AdminDomainService) evaluateLifecycle(ctx context.Context, accountID, activationSource string, op port.IdentityDomainOperation) error {
	count, err := s.admins.CountActiveSecurityAdmins(ctx)
	if err != nil {
		return err
	}
	op.ActiveSecurityAdmins = int64(count)
	op.BootstrapEnabled = s.config.BootstrapEnabled
	return s.policy.EvaluateIdentityDomain(s.lifecycleInput(accountID, activationSource, op))
}

func (s *AdminDomainService) executeLifecycle(ctx context.Context, actorKind, actorID, operationCode, targetID string, action func(context.Context) error) error {
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	event, err := reliability.NewEvent("authorization.changed", "admin_account", targetID, map[string]string{
		"command": operationCode, "actor_id": actorID, "resource_type": "admin_account", "resource_id": targetID,
	})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{
		Code: operationCode, ActorID: actorID, ActorType: actorKind,
		ResourceType: "admin_account", ResourceID: targetID,
		OperationCode: operationCode, Event: &event,
	}, action)
}

func (s *AdminDomainService) audit(ctx context.Context, actorKind, actorID, operationCode, targetID, reason, requestID string) error {
	if s.catalog == nil {
		return nil
	}
	return s.catalog.RecordAuthorizationAudit(ctx, repository.AuthorizationAudit{
		ActorKind: actorKind, ActorID: actorID, OperationCode: operationCode,
		ResourceType: "admin_account", ResourceID: targetID, Outcome: "allow", Reason: reason,
		RequestID: requestID,
	})
}

// BootstrapSecurityAdmin creates the very first security administrator through
// the controlled system entry: bootstrap must be enabled and no active
// security administrator may exist. The policy is re-evaluated inside the
// transaction so a concurrent bootstrap fails closed.
func (s *AdminDomainService) BootstrapSecurityAdmin(ctx context.Context, username, email, password, auditReason string) (*repository.AdminDomainAccount, error) {
	if s.admins == nil {
		return nil, ErrAdminDomainUnavailable
	}
	accountID := newAdminID()
	operation := port.IdentityDomainOperation{
		Kind: "bootstrap", ActorKind: "system", ActorMFA: true,
		TargetAdminID: accountID, AuditReason: auditReason,
	}
	if err := s.evaluateLifecycle(ctx, accountID, "bootstrap", operation); err != nil {
		return nil, err
	}
	err := s.executeLifecycle(ctx, "system", "bootstrap", "identity.admin_domain.bootstrap", accountID, func(commandCtx context.Context) error {
		if err := s.evaluateLifecycle(commandCtx, accountID, "bootstrap", operation); err != nil {
			return err
		}
		return s.createAccountRecord(commandCtx, accountID, username, email, password, "security_admin", "system", "bootstrap", "identity.admin_domain.bootstrap", auditReason)
	})
	if err != nil {
		return nil, err
	}
	account, err := s.admins.GetAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// CreateAdmin creates an administrator account under an MFA-authenticated
// admin actor. securityAdmin binds the protected security_admin role.
func (s *AdminDomainService) CreateAdmin(ctx context.Context, actor AdminActor, username, email, password string, securityAdmin bool, auditReason string) (*repository.AdminDomainAccount, error) {
	if s.admins == nil {
		return nil, ErrAdminDomainUnavailable
	}
	accountID := newAdminID()
	operation := port.IdentityDomainOperation{
		Kind: "create", ActorKind: "admin", ActorMFA: actor.MFAAuthenticated,
		TargetAdminID: accountID, AuditReason: auditReason,
	}
	if err := s.evaluateLifecycle(ctx, accountID, "direct", operation); err != nil {
		return nil, err
	}
	err := s.executeLifecycle(ctx, "admin", actor.ID, "identity.admin_domain.create", accountID, func(commandCtx context.Context) error {
		if err := s.evaluateLifecycle(commandCtx, accountID, "direct", operation); err != nil {
			return err
		}
		bindingRole := ""
		if securityAdmin {
			bindingRole = "security_admin"
		}
		return s.createAccountRecord(commandCtx, accountID, username, email, password, bindingRole, "admin", actor.ID, "identity.admin_domain.create", auditReason)
	})
	if err != nil {
		return nil, err
	}
	account, err := s.admins.GetAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *AdminDomainService) createAccountRecord(ctx context.Context, accountID, username, email, password, bindingRole, actorKind, actorID, operationCode, reason string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.admins.CreateAccount(ctx, repository.AdminDomainAccount{
		ID: accountID, Username: username, Email: email, DisplayName: username,
	}); err != nil {
		return err
	}
	if _, _, err := s.admins.SetCredentialCAS(ctx, accountID, hash, 0, s.now()); err != nil {
		return err
	}
	if bindingRole != "" {
		if err := s.admins.BindRole(ctx, repository.AdminDomainRoleBinding{
			ID: "bind-" + accountID, AccountID: accountID, RoleID: bindingRole, GrantedBy: actorID,
		}); err != nil {
			return err
		}
	}
	return s.audit(ctx, actorKind, actorID, operationCode, accountID, reason, operationCode+"-"+accountID)
}

// SuspendAdmin and RevokeAdmin transition one admin account under an MFA
// admin actor with last-security-admin protection. Revocation is terminal.
func (s *AdminDomainService) SuspendAdmin(ctx context.Context, actor AdminActor, targetID, reason string) error {
	return s.transitionAdmin(ctx, actor, targetID, reason, "suspend", "suspended")
}

func (s *AdminDomainService) RevokeAdmin(ctx context.Context, actor AdminActor, targetID, reason string) error {
	return s.transitionAdmin(ctx, actor, targetID, reason, "revoke", "revoked")
}

// RestoreAdmin lifts a suspension; a revoked account is terminal and never
// returns.
func (s *AdminDomainService) RestoreAdmin(ctx context.Context, actor AdminActor, targetID, reason string) error {
	return s.transitionAdmin(ctx, actor, targetID, reason, "restore", "active")
}

func (s *AdminDomainService) transitionAdmin(ctx context.Context, actor AdminActor, targetID, reason, operation, status string) error {
	if s.admins == nil {
		return ErrAdminDomainUnavailable
	}
	if actor.ID == targetID {
		return ErrAdminSelfTransition
	}
	target, err := s.admins.GetAccountByID(ctx, targetID)
	if err != nil {
		return err
	}
	bindings, err := s.admins.ListActiveBindings(ctx, targetID)
	if err != nil {
		return err
	}
	targetSecurity := false
	for _, binding := range bindings {
		if binding.RoleID == "security_admin" {
			targetSecurity = true
		}
	}
	op := port.IdentityDomainOperation{
		Kind: operation, ActorKind: "admin", ActorMFA: actor.MFAAuthenticated,
		TargetAdminID: targetID, TargetIsSecurityAdmin: targetSecurity, AuditReason: reason,
	}
	if err := s.evaluateLifecycle(ctx, targetID, "direct", op); err != nil {
		return err
	}
	return s.executeLifecycle(ctx, "admin", actor.ID, "identity.admin_domain."+operation, targetID, func(commandCtx context.Context) error {
		current, err := s.admins.GetAccountByID(commandCtx, targetID)
		if err != nil {
			return err
		}
		if current.Version != target.Version {
			return repository.ErrAdminDomainConflict
		}
		if err := s.evaluateLifecycle(commandCtx, targetID, "direct", op); err != nil {
			return err
		}
		_, changed, err := s.admins.SetAccountStatusCAS(commandCtx, targetID, current.Version, status, reason, actor.ID, s.now())
		if err != nil {
			return err
		}
		if !changed {
			// A revoked account is terminal; a version race also fails closed.
			return repository.ErrAdminDomainConflict
		}
		return s.audit(commandCtx, "admin", actor.ID, "identity.admin_domain."+operation, targetID, reason, operation+"-"+targetID)
	})
}

// HasCode reports whether an active admin account holds one permission code
// through its active admin-domain role bindings.
func (s *AdminDomainService) HasCode(ctx context.Context, accountID, code string) (bool, error) {
	codes, err := s.admins.ListActivePermissionCodes(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, item := range codes {
		if item == code {
			return true, nil
		}
	}
	return false, nil
}
