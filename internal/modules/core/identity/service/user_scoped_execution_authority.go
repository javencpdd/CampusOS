package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type UserScopedExecutionAuthorityService struct {
	roles      repository.UserScopedExecutionFactsReader
	sessions   *SessionService
	admissions repository.AdminAccountRepository
}

func NewUserScopedExecutionAuthorityService(roles repository.UserScopedExecutionFactsReader, sessions *SessionService, admissions repository.AdminAccountRepository) *UserScopedExecutionAuthorityService {
	return &UserScopedExecutionAuthorityService{roles: roles, sessions: sessions, admissions: admissions}
}

type userScopedExecutionBinding struct {
	Query     identityport.UserScopedExecutionQuery
	User      domain.User
	Session   domain.Session
	Roles     []repository.UserScopedExecutionFact
	Admission *repository.AdminAccount
}

func canonicalUserScopeFacts(rows []repository.UserScopedExecutionFact) []repository.UserScopedExecutionFact {
	out := make([]repository.UserScopedExecutionFact, len(rows))
	for i, row := range rows {
		out[i] = repository.UserScopedExecutionFact{Assignment: bytes.Clone(row.Assignment), Role: bytes.Clone(row.Role), Link: bytes.Clone(row.Link), Definition: bytes.Clone(row.Definition)}
	}
	sort.Slice(out, func(i, j int) bool {
		if order := bytes.Compare(out[i].Assignment, out[j].Assignment); order != 0 {
			return order < 0
		}
		return bytes.Compare(out[i].Link, out[j].Link) < 0
	})
	return out
}

func canonicalUserScopeSession(session *domain.Session) domain.Session {
	out := *session
	out.CreatedAt, out.UpdatedAt = out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	out.LastActiveAt, out.ExpiresAt = out.LastActiveAt.UTC(), out.ExpiresAt.UTC()
	for _, field := range []**time.Time{&out.MFAAuthenticatedAt, &out.RevokedAt} {
		if *field != nil {
			at := (*field).UTC()
			*field = &at
		}
	}
	return out
}

type userScopeAssignment struct {
	ID, RoleID, FactsVersion int64
	UserID                   json.RawMessage
	ScopeType                string
	ScopeID                  *int64
	DeletedAt                *time.Time
}

func decodeUserScopeAssignment(raw json.RawMessage) (userScopeAssignment, error) {
	var row struct {
		ID           int64           `json:"id"`
		RoleID       int64           `json:"role_id"`
		UserID       json.RawMessage `json:"user_id"`
		ScopeType    string          `json:"scope_type"`
		ScopeID      *int64          `json:"scope_id"`
		FactsVersion int64           `json:"facts_version"`
		DeletedAt    *time.Time      `json:"deleted_at"`
	}
	err := json.Unmarshal(raw, &row)
	return userScopeAssignment{row.ID, row.RoleID, row.FactsVersion, row.UserID, row.ScopeType, row.ScopeID, row.DeletedAt}, err
}

func scopeUserID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// Validate each complete relationship separately; never combine an unrelated
// assignment, role, link or definition to manufacture execution authority.
func userScopePermission(row repository.UserScopedExecutionFact, user string, board int64, code string) (matches, needsAdmission bool, err error) {
	a, err := decodeUserScopeAssignment(row.Assignment)
	var role struct {
		repository.Role
		FactsVersion int64      `json:"facts_version"`
		DeletedAt    *time.Time `json:"deleted_at"`
	}
	var link struct {
		RoleID       int64      `json:"role_id"`
		PermissionID int64      `json:"permission_id"`
		FactsVersion int64      `json:"facts_version"`
		DeletedAt    *time.Time `json:"deleted_at"`
	}
	var definition struct {
		repository.PermissionDefinition
		FactsVersion int64 `json:"facts_version"`
	}
	if err != nil || json.Unmarshal(row.Role, &role) != nil || json.Unmarshal(row.Link, &link) != nil || json.Unmarshal(row.Definition, &definition) != nil || a.ID < 1 || a.RoleID < 1 || a.FactsVersion < 1 || scopeUserID(a.UserID) != user || role.ID != a.RoleID || link.RoleID != role.ID || definition.ID < 1 || link.PermissionID != definition.ID || role.FactsVersion < 1 || link.FactsVersion < 1 || definition.FactsVersion < 1 || a.DeletedAt != nil || role.DeletedAt != nil || link.DeletedAt != nil || definition.DeprecatedAt != nil || !(a.ScopeType == "global" && a.ScopeID == nil || a.ScopeType == "category" && a.ScopeID != nil && *a.ScopeID == board) {
		return false, false, identityport.ErrGovernanceAuthorityUnavailable
	}
	scopeAllowed := false
	for _, scope := range definition.AllowedScopeTypes {
		if scope == a.ScopeType {
			scopeAllowed = true
		}
	}
	action := "pin"
	if code == "community.thread.lock" {
		action = "lock"
	}
	return definition.Code == code && definition.Domain == "community" && definition.Resource == "thread" && definition.Action == action && scopeAllowed, role.Name == "admin", nil
}

func (s *UserScopedExecutionAuthorityService) load(ctx context.Context, q identityport.UserScopedExecutionQuery, p identityport.PrincipalContext, lock bool) (userScopedExecutionBinding, bool, error) {
	var b userScopedExecutionBinding
	getUser, getSession := s.sessions.users.GetByID, s.sessions.sessions.GetByID
	if lock {
		locker, ok := s.sessions.users.(repository.UserAuthorityLocker)
		if !ok || nilGovernanceAdapter(locker) {
			return b, false, identityport.ErrGovernanceAuthorityUnavailable
		}
		getUser, getSession = locker.GetByIDForUpdate, s.sessions.sessions.GetByIDForUpdate
	}
	u, err := getUser(ctx, p.Actor.ID)
	if err != nil || u == nil || u.ID != p.Actor.ID || u.Status != domain.UserStatusActive || u.AuthVersion < 1 {
		return b, false, identityport.ErrUserSessionPrincipalUnavailable
	}
	session, err := getSession(ctx, p.CredentialID)
	if err != nil || session == nil || session.ID != p.CredentialID || session.UserID != u.ID || session.RevokedAt != nil || !s.sessions.now().Before(session.ExpiresAt) || string(session.AuthenticationStrength) != p.AuthenticationStrength {
		return b, false, identityport.ErrUserSessionPrincipalUnavailable
	}
	board, _ := strconv.ParseInt(q.Scope.ID, 10, 64)
	rows, err := s.roles.LoadUserScopedExecutionFacts(ctx, u.ID, board, lock)
	if err != nil {
		return b, false, errors.Join(identityport.ErrGovernanceAuthorityUnavailable, err)
	}
	b = userScopedExecutionBinding{Query: q, User: *u, Session: canonicalUserScopeSession(session), Roles: canonicalUserScopeFacts(rows)}
	b.User.CreatedAt, b.User.UpdatedAt = b.User.CreatedAt.UTC(), b.User.UpdatedAt.UTC()
	allowed, adminMatch, hasAdminRole := false, false, false
	for _, row := range b.Roles {
		matches, admission, err := userScopePermission(row, u.ID, board, q.PermissionCode)
		if err != nil {
			return b, false, err
		}
		hasAdminRole = hasAdminRole || admission
		if matches && admission {
			adminMatch = true
		} else if matches {
			allowed = true
		}
	}
	if hasAdminRole {
		if nilGovernanceAdapter(s.admissions) {
			return b, false, identityport.ErrGovernanceAuthorityUnavailable
		}
		get := s.admissions.Get
		if lock {
			locker, ok := s.admissions.(repository.UserRoleAdmissionLocker)
			if !ok || nilGovernanceAdapter(locker) {
				return b, false, identityport.ErrGovernanceAuthorityUnavailable
			}
			get = locker.GetForUpdate
		}
		account, err := get(ctx, u.ID)
		if err != nil && !errors.Is(err, repository.ErrAdminAccountNotFound) {
			return b, false, errors.Join(identityport.ErrGovernanceAuthorityUnavailable, err)
		}
		if account != nil {
			if account.UserID != u.ID || account.Version < 1 {
				return b, false, identityport.ErrGovernanceAuthorityUnavailable
			}
			raw, err := json.Marshal(account)
			if err != nil || json.Unmarshal(raw, &b.Admission) != nil {
				return b, false, identityport.ErrGovernanceAuthorityUnavailable
			}
			allowed = allowed || adminMatch && account.Status == repository.AdminAccountStatusActive
		}
	}
	return b, allowed, nil
}

func (s *UserScopedExecutionAuthorityService) LoadUserScopedExecutionAuthority(ctx context.Context, q identityport.UserScopedExecutionQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.UserScopedExecutionAuthority, error) {
	if err := q.Validate(); err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	if s == nil || nilGovernanceAdapter(s.roles) || s.sessions == nil || nilGovernanceAdapter(s.sessions.users) || nilGovernanceAdapter(s.sessions.sessions) || lock && !transaction.Active(ctx) {
		return identityport.UserScopedExecutionAuthority{}, identityport.ErrGovernanceAuthorityUnavailable
	}
	p, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	if p.Actor.Kind != "user" {
		return identityport.UserScopedExecutionAuthority{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	binding, allowed, err := s.load(ctx, q, p, lock)
	if err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	current, err := governancePrincipal(ctx, resolve)
	if err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	if !reflect.DeepEqual(p, current) {
		return identityport.UserScopedExecutionAuthority{}, identityport.ErrPolicyFactsChanged
	}
	final, finalAllowed, err := s.load(ctx, q, p, lock)
	if err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	if !reflect.DeepEqual(binding, final) || allowed != finalAllowed || !s.sessions.now().Before(binding.Session.ExpiresAt) {
		return identityport.UserScopedExecutionAuthority{}, identityport.ErrPolicyFactsChanged
	}
	if err := ctx.Err(); err != nil {
		return identityport.UserScopedExecutionAuthority{}, err
	}
	return identityport.UserScopedExecutionAuthority{Principal: p, Query: q, Allowed: allowed, ExpiresAtMS: binding.Session.ExpiresAt.UnixMilli(), Binding: binding}, nil
}
