package space

import (
	"context"
	"errors"
	"fmt"
	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"reflect"
	"strings"
	"time"
)

var ErrSpaceAdminDenied = errors.New("space administration denied")

type SpaceAdminMutationAck struct {
	OwnerID  string `json:"owner_id"`
	SpaceID  string `json:"space_id,omitempty"`
	Disabled bool   `json:"disabled"`
}
type spaceAdminFacts struct {
	Space *spaceOwnerFacts
	User  identityport.User
}

func (s *Service) SetAdminExecutionAuthority(reader identityport.AdminExecutionAuthorityReader) {
	s.adminExecution = reader
}
func spaceAdminUser(user *identitydomain.User) identityport.User {
	return identityport.User{ID: user.ID, Username: user.Username, Nickname: user.Nickname, Email: user.Email, Avatar: user.Avatar, Bio: user.Bio, Status: string(user.Status)}
}
func (s *Service) loadAdminSpaceFacts(ctx context.Context, owner string, lock bool) (*spaceAdminFacts, error) {
	var locked identityport.User
	if lock {
		reader, ok := s.users.(interface {
			GetByIDForUpdate(context.Context, string) (*identitydomain.User, error)
		})
		if !ok || corestorage.NilImageAdapter(reader) {
			return nil, ErrSpaceFactsUnavailable
		}
		user, err := reader.GetByIDForUpdate(ctx, owner)
		if err != nil || user == nil {
			return nil, errors.Join(ErrSpaceFactsUnavailable, err)
		}
		locked = spaceAdminUser(user)
	} else {
		user, err := s.users.GetByID(ctx, owner)
		if err != nil || user == nil {
			return nil, errors.Join(ErrSpaceFactsUnavailable, err)
		}
		locked = spaceAdminUser(user)
	}
	f, err := s.loadSpaceFacts(ctx, owner, lock, false)
	if err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, owner)
	if err != nil || user == nil || user.ID != owner {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	result := &spaceAdminFacts{Space: f, User: spaceAdminUser(user)}
	if !reflect.DeepEqual(locked, result.User) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	return result, nil
}
func adminSpaceRequest(f *spaceAdminFacts, a identityport.AdminExecutionAuthority, op, id string, at int64) identityport.PersonalSpaceAdminRequest {
	return identityport.PersonalSpaceAdminRequest{RequestID: id, PolicyVersion: identityport.PersonalSpaceAdminPolicyVersion, Operation: op, OwnerID: f.User.ID, SpaceID: f.Space.Space.ID, UserStatus: f.User.Status, Principal: a.Principal, Exists: f.Space.Raw != nil, Disabled: f.Space.Space.DisabledAt != nil, FactsVersion: f.Space.Space.FactsVersion, ExecutionQuery: a.Query, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: at}
}
func (s *Service) DisableSpaceForPrincipal(ctx context.Context, owner, reason string, resolve identityport.SessionPrincipalResolver) (*SpaceAdminMutationAck, error) {
	return s.changeAdminSpaceState(ctx, owner, "disable", reason, resolve)
}
func (s *Service) EnableSpaceForPrincipal(ctx context.Context, owner string, resolve identityport.SessionPrincipalResolver) (*SpaceAdminMutationAck, error) {
	return s.changeAdminSpaceState(ctx, owner, "enable", "", resolve)
}
func (s *Service) changeAdminSpaceState(ctx context.Context, owner, op, reason string, resolve identityport.SessionPrincipalResolver) (*SpaceAdminMutationAck, error) {
	if s == nil || s.reliable == nil || corestorage.NilImageAdapter(s.adminExecution) || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	writer, ok := s.repo.(adminSpaceStateWriter)
	if !ok || corestorage.NilImageAdapter(writer) {
		return nil, ErrSpaceFactsUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrAdminSessionPrincipalUnavailable
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 {
		return nil, ErrSpaceOwnerInvalid
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: identityport.PolicyResourceRef{Kind: "personal_space.space", ID: owner}}
	a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, err
	}
	if !a.Allowed {
		return nil, ErrSpaceAdminDenied
	}
	initial, err := s.loadAdminSpaceFacts(ctx, owner, false)
	if err != nil {
		return nil, err
	}
	r := adminSpaceRequest(initial, a, op, fmt.Sprint(idgen.New()), time.Now().UnixMilli())
	d, err := (identitypolicy.PersonalSpaceAdminPolicy{}).DecidePersonalSpaceAdmin(r)
	if err != nil {
		return nil, err
	}
	if d.Effect != "allow" {
		return nil, ErrSpaceAdminDenied
	}
	check := func(ctx context.Context, lock bool) (identityport.AdminExecutionAuthority, error) {
		current, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
		if err != nil {
			return current, err
		}
		if err := identityport.CheckAdminExecutionAuthority(a, current); err != nil {
			return current, err
		}
		if !current.Allowed || time.Now().UnixMilli() >= current.ExpiresAtMS {
			return current, identityport.ErrPolicyFactsChanged
		}
		return current, s.ensureEnabled()
	}
	var ack SpaceAdminMutationAck
	code := "feature.personal_space.admin_" + op
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: a.Principal.Actor.ID, ActorType: "admin", ResourceType: "personal_space", ResourceID: owner, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("personal_space.availability_updated", "personal_space", owner, ack)
	}}, func(tx context.Context) error {
		currentAuthority, err := check(tx, true)
		if err != nil {
			return err
		}
		current, err := s.loadAdminSpaceFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := identitypolicy.CheckPersonalSpaceAdminDecision(r, d, adminSpaceRequest(current, currentAuthority, op, r.RequestID, r.EvaluatedAtMS)); err != nil {
			return err
		}
		expected := canonicalOwnerSpace(current.Space.Space)
		// Enabling an absent default is a metadata no-op; never create a row.
		changed := op == "disable" || expected.DisabledAt != nil
		if changed {
			at := time.Now().UTC().Truncate(time.Microsecond)
			if current.Space.Raw == nil {
				expected.ID, expected.CreatedAt, expected.UpdatedAt = fmt.Sprint(idgen.New()), at, at
				expected.IsDefault = false
				write := canonicalOwnerSpace(expected)
				if err := s.repo.Upsert(tx, write); err != nil {
					return err
				}
				created, err := s.loadAdminSpaceFacts(tx, owner, true)
				if err != nil {
					return err
				}
				if write.FactsVersion < 1 {
					return identityport.ErrPolicyFactsChanged
				}
				expected.FactsVersion = write.FactsVersion
				if !reflect.DeepEqual(created.Space.Space, expected) || !reflect.DeepEqual(current.User, created.User) {
					return identityport.ErrPolicyFactsChanged
				}
			}
			expected.DisabledAt, expected.DisabledBy, expected.DisabledReason = nil, "", ""
			expected.DisabledActorKind, expected.DisabledActorID = "legacy_unknown", ""
			if op == "disable" {
				expected.DisabledAt, expected.DisabledReason = &at, reason
				expected.DisabledActorKind, expected.DisabledActorID = "admin", a.Principal.Actor.ID
			}
			expected.UpdatedAt = at
			receipt, err := writer.UpdateAdminSpaceState(tx, owner, expected.FactsVersion, expected.DisabledAt, a.Principal.Actor.ID, expected.DisabledReason, at)
			if err != nil {
				return err
			}
			if receipt <= expected.FactsVersion {
				return identityport.ErrPolicyFactsChanged
			}
			expected.FactsVersion = receipt
		}
		result, err := s.loadAdminSpaceFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(expected, result.Space.Space) || !reflect.DeepEqual(current.User, result.User) {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := check(tx, true); err != nil {
			return err
		}
		final, err := s.loadAdminSpaceFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := check(tx, true); err != nil {
			return err
		}
		ack = SpaceAdminMutationAck{OwnerID: owner, SpaceID: result.Space.Space.ID, Disabled: result.Space.Space.DisabledAt != nil}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ack, nil
}
