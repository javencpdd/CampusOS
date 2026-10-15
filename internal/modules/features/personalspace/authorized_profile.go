package space

import (
	"context"
	"encoding/json"
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

var ErrSpaceOwnerDenied = errors.New("space owner command denied")
var ErrSpaceOwnerInvalid = errors.New("space owner command invalid")

type SpaceMutationAck struct {
	SpaceID string `json:"space_id"`
	OwnerID string `json:"owner_id"`
}
type spaceOwnerFacts struct {
	Raw   json.RawMessage
	Space *Space
	Owner Owner
}

func (s *Service) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable != nil {
		if snapshot, ok := s.repo.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
}
func canonicalOwnerSpace(s *Space) *Space {
	s = cloneSpace(s)
	if s == nil {
		return nil
	}
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.DisabledAt != nil {
		at := s.DisabledAt.UTC()
		s.DisabledAt = &at
	}
	if s.LastSyncAt != nil {
		at := s.LastSyncAt.UTC()
		s.LastSyncAt = &at
	}
	return s
}
func (s *Service) loadSpaceOwnerFacts(ctx context.Context, owner string, lock bool) (*spaceOwnerFacts, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	reader, ok := s.repo.(SpaceAuthorityReader)
	if !ok || corestorage.NilImageAdapter(reader) || corestorage.NilImageAdapter(s.users) {
		return nil, ErrSpaceFactsUnavailable
	}
	user, err := s.users.GetByID(ctx, owner)
	if err != nil || user == nil || user.ID != owner || user.Status != identitydomain.UserStatusActive {
		return nil, errors.Join(ErrSpaceOwnerDenied, err)
	}
	raw, err := reader.LoadSpaceAuthority(ctx, owner, lock)
	var space *Space
	if errors.Is(err, ErrSpaceNotFound) {
		if err := reader.CheckSpaceAuthority(ctx); err != nil {
			return nil, err
		}
		space = defaultSpace(user)
		space.CreatedAt, space.UpdatedAt = time.Time{}, time.Time{}
		normalizeSpaceActor(space)
		raw = nil
	} else if err != nil {
		return nil, err
	} else {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil || len(fields["deleted_at"]) == 0 || string(fields["deleted_at"]) != "null" {
			return nil, ErrSpaceFactsUnavailable
		}
		for _, name := range []string{"id", "user_id"} {
			if field := fields[name]; len(field) > 0 && field[0] != '"' {
				fields[name], _ = json.Marshal(string(field))
			}
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		space = &Space{}
		if json.Unmarshal(encoded, space) != nil || json.Unmarshal(fields["facts_version"], &space.FactsVersion) != nil || json.Unmarshal(fields["disabled_actor_kind"], &space.DisabledActorKind) != nil || json.Unmarshal(fields["disabled_actor_id"], &space.DisabledActorID) != nil || space.FactsVersion < 1 || space.UserID != owner || space.ID == "" {
			return nil, ErrSpaceFactsUnavailable
		}
		if space.StyleManifest != nil && reflect.DeepEqual(space.StyleManifest, &StyleManifest{}) {
			space.StyleManifest = nil
		}
	}
	f := &spaceOwnerFacts{Raw: append(json.RawMessage(nil), raw...), Space: canonicalOwnerSpace(space), Owner: Owner{ID: user.ID, Username: user.Username, Nickname: user.Nickname, Avatar: user.Avatar, Bio: user.Bio}}
	if err := reader.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	return f, nil
}
func spaceOwnerRequest(f *spaceOwnerFacts, p identityport.PrincipalContext, op, id string) identityport.PersonalSpaceOwnerRequest {
	return identityport.PersonalSpaceOwnerRequest{RequestID: id, PolicyVersion: identityport.PersonalSpaceOwnerPolicyVersion, Operation: op, OwnerID: f.Space.UserID, Principal: p, Exists: f.Raw != nil, Disabled: f.Space.DisabledAt != nil, FactsVersion: f.Space.FactsVersion}
}
func spaceOwnerPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (identityport.PrincipalContext, error) {
	if resolve == nil {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return identityport.PrincipalContext{}, err
	}
	p, err := resolve(ctx)
	if err != nil || p.Validate() != nil {
		return p, errors.Join(identityport.ErrUserSessionPrincipalUnavailable, err)
	}
	if p.Actor.Kind != "user" || p.Audience != "user" {
		return p, ErrSpaceOwnerDenied
	}
	return p, nil
}
func (s *Service) GetOwnSpaceForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*PublicSpace, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false, func(ctx context.Context, p identityport.PrincipalContext) (*spaceOwnerFacts, error) {
		f, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
		if err != nil {
			return nil, err
		}
		r := spaceOwnerRequest(f, p, "read_profile", "space-owner-read")
		d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
		if err != nil || d.Effect != "allow" {
			return nil, errors.Join(ErrSpaceOwnerDenied, err)
		}
		return f, nil
	}, func(ctx context.Context, p identityport.PrincipalContext, f *spaceOwnerFacts) (*PublicSpace, error) {
		return buildPublicSpace(&identitydomain.User{ID: f.Owner.ID, Username: f.Owner.Username, Nickname: f.Owner.Nickname, Avatar: f.Owner.Avatar, Bio: f.Owner.Bio}, f.Space), nil
	}, nil)
}
func normalizeSpaceProfileRequest(req UpsertSpaceRequest) (UpsertSpaceRequest, error) {
	for _, field := range []struct {
		value **string
		max   int
	}{{&req.Title, 120}, {&req.Bio, 500}, {&req.Avatar, 512}, {&req.CoverImage, 512}, {&req.Theme, 64}, {&req.Layout, 64}, {&req.Visibility, 20}} {
		if *field.value != nil {
			value := strings.TrimSpace(**field.value)
			if len([]rune(value)) > field.max {
				return req, ErrSpaceOwnerInvalid
			}
			*field.value = &value
		}
	}
	if req.Visibility != nil && !validVisibility(Visibility(*req.Visibility)) {
		return req, ErrInvalidVisibility
	}
	if req.SyncEnabled != nil {
		value := *req.SyncEnabled
		req.SyncEnabled = &value
	}
	if req.SyncCategories != nil {
		req.SyncCategories = append([]string{}, req.SyncCategories...)
	}
	if req.SyncTags != nil {
		req.SyncTags = append([]string{}, req.SyncTags...)
	}
	return req, nil
}
func (s *Service) UpsertOwnSpaceForPrincipal(ctx context.Context, req UpsertSpaceRequest, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeSpaceOwnerMutation(ctx, req, "profile", resolve, nil, nil, nil)
}

func (s *Service) executeSpaceOwnerMutation(ctx context.Context, req UpsertSpaceRequest, operation string, resolve identityport.SessionPrincipalResolver, preflight *spaceOwnerFacts, check func(context.Context) error, writeAvatar func(context.Context) (string, error)) (*SpaceMutationAck, error) {
	if operation != "profile" && operation != "avatar_select" && operation != "avatar_upload" {
		return nil, ErrSpaceOwnerInvalid
	}
	if (operation == "avatar_upload") != (writeAvatar != nil) {
		return nil, ErrSpaceOwnerInvalid
	}
	if s.reliable == nil || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	p, err := spaceOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	req, err = normalizeSpaceProfileRequest(req)
	if err != nil {
		return nil, err
	}
	initial, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	if preflight != nil && !reflect.DeepEqual(preflight, initial) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if check != nil {
		if err := check(ctx); err != nil {
			return nil, err
		}
	}
	request := spaceOwnerRequest(initial, p, operation, fmt.Sprint(idgen.New()))
	decision, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrSpaceOwnerDenied
	}
	bound := func(ctx context.Context) error {
		current, err := spaceOwnerPrincipal(ctx, resolve)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(p, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if check != nil {
			if err := check(ctx); err != nil {
				return err
			}
		}
		return s.ensureEnabled()
	}
	var ack SpaceMutationAck
	code := "feature.personal_space." + operation
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "personal_space", ResourceID: p.Actor.ID, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("personal_space.updated", "personal_space", p.Actor.ID, ack)
	}}, func(tx context.Context) error {
		if err := bound(tx); err != nil {
			return err
		}
		current, err := s.loadSpaceOwnerFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := identitypolicy.CheckPersonalSpaceOwnerDecision(request, decision, spaceOwnerRequest(current, p, operation, request.RequestID)); err != nil {
			return err
		}
		if writeAvatar != nil {
			url, err := writeAvatar(tx)
			if err != nil {
				return err
			}
			req.Avatar = &url
		}
		expected := canonicalOwnerSpace(current.Space)
		if err := applyUpdate(expected, req); err != nil {
			return err
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		if current.Raw == nil {
			expected.ID = fmt.Sprint(idgen.New())
			expected.CreatedAt = at
		}
		expected.UpdatedAt = at
		expected.IsDefault = false
		write := canonicalOwnerSpace(expected)
		if err := s.repo.Upsert(tx, write); err != nil {
			return err
		}
		result, err := s.loadSpaceOwnerFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		changed := current.Raw == nil || !sameSpaceMetadata(current.Space, expected)
		if result.Space.FactsVersion < 1 || changed && result.Space.FactsVersion <= current.Space.FactsVersion || !changed && result.Space.FactsVersion != current.Space.FactsVersion {
			return identityport.ErrPolicyFactsChanged
		}
		expected.FactsVersion = result.Space.FactsVersion
		if !reflect.DeepEqual(canonicalOwnerSpace(expected), result.Space) || !reflect.DeepEqual(current.Owner, result.Owner) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := bound(tx); err != nil {
			return err
		}
		final, err := s.loadSpaceOwnerFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := bound(tx); err != nil {
			return err
		}
		ack = SpaceMutationAck{SpaceID: result.Space.ID, OwnerID: p.Actor.ID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ack, nil
}
