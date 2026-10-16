package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// This is the actual Appearance forwarding seam. A bare user ID is not proof.
type AuthenticatedSpaceStyleCommands interface {
	ApplyOwnSpaceStylePackage(context.Context, StylePackage, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
	RestoreOwnSpaceDefaultStyle(context.Context, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
	RollbackOwnSpaceStyle(context.Context, identityport.SessionPrincipalResolver) (*SpaceMutationAck, error)
}

type spaceStyleFacts struct {
	Owner     *spaceOwnerFacts
	Raw       map[string]json.RawMessage
	Snapshots map[string]*StyleSnapshot
}

// A builder/check pair is trusted first-party composition, never an HTTP DTO.
type spaceStyleBuilder func(context.Context, *spaceStyleFacts) (StylePackage, func(context.Context) error, error)

func canonicalStyleSnapshot(snapshot *StyleSnapshot) *StyleSnapshot {
	snapshot = cloneSnapshot(snapshot)
	if snapshot == nil {
		return nil
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	if snapshot.StyleManifest != nil && reflect.DeepEqual(snapshot.StyleManifest, &StyleManifest{}) {
		snapshot.StyleManifest = nil
	}
	return snapshot
}
func (s *Service) loadSpaceStyleFacts(ctx context.Context, owner string, lock bool) (*spaceStyleFacts, error) {
	facts, err := s.loadSpaceOwnerFacts(ctx, owner, lock)
	if err != nil {
		return nil, err
	}
	reader, ok := s.repo.(SpaceAuthorityReader)
	if !ok || corestorage.NilImageAdapter(reader) {
		return nil, ErrSpaceFactsUnavailable
	}
	rows, err := reader.LoadStyleSnapshotAuthority(ctx, owner, lock)
	if err != nil {
		return nil, err
	}
	f := &spaceStyleFacts{Owner: facts, Raw: map[string]json.RawMessage{}, Snapshots: map[string]*StyleSnapshot{}}
	for _, raw := range rows {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, ErrSpaceFactsUnavailable
		}
		for _, name := range []string{"id", "user_id"} {
			if v := fields[name]; len(v) > 0 && v[0] != '"' {
				fields[name], _ = json.Marshal(string(v))
			}
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		var snapshot StyleSnapshot
		if json.Unmarshal(encoded, &snapshot) != nil || json.Unmarshal(fields["facts_version"], &snapshot.FactsVersion) != nil || snapshot.FactsVersion < 1 || snapshot.ID == "" || snapshot.UserID != owner || snapshot.CreatedAt.IsZero() {
			return nil, ErrSpaceFactsUnavailable
		}
		if _, duplicate := f.Raw[snapshot.ID]; duplicate {
			return nil, ErrSpaceFactsUnavailable
		}
		f.Raw[snapshot.ID] = append(json.RawMessage(nil), raw...)
		f.Snapshots[snapshot.ID] = canonicalStyleSnapshot(&snapshot)
	}
	if err := reader.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Service) ApplyOwnSpaceStylePackage(ctx context.Context, pkg StylePackage, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_apply", pkg, resolve, nil)
}
func (s *Service) RestoreOwnSpaceDefaultStyle(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_default", StylePackage{}, resolve, nil)
}
func (s *Service) RollbackOwnSpaceStyle(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	return s.executeOwnSpaceStyle(ctx, "style_rollback", StylePackage{}, resolve, nil)
}

func latestCheckedStyleSnapshot(f *spaceStyleFacts) *StyleSnapshot {
	var latest *StyleSnapshot
	for _, snapshot := range f.Snapshots {
		if latest == nil || snapshot.CreatedAt.After(latest.CreatedAt) || snapshot.CreatedAt.Equal(latest.CreatedAt) && snapshot.FactsVersion > latest.FactsVersion {
			latest = snapshot
		}
	}
	return canonicalStyleSnapshot(latest)
}

func (s *Service) executeOwnSpaceStyle(ctx context.Context, operation string, pkg StylePackage, resolve identityport.SessionPrincipalResolver, build spaceStyleBuilder) (*SpaceMutationAck, error) {
	if s == nil || s.reliable == nil || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	variant := operation == "style_html" || operation == "style_zip" || operation == "style_source"
	if operation != "style_apply" && operation != "style_default" && operation != "style_rollback" && !variant {
		return nil, ErrSpaceOwnerInvalid
	}
	if variant != (build != nil) {
		return nil, ErrSpaceOwnerInvalid
	}
	snapshots, ok := s.repo.(StyleSnapshotRepository)
	if !ok || corestorage.NilImageAdapter(snapshots) {
		return nil, ErrSpaceFactsUnavailable
	}
	p, err := spaceOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.loadSpaceStyleFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	request := spaceOwnerRequest(initial.Owner, p, operation, fmt.Sprint(idgen.New()))
	decision, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(request)
	if err != nil || decision.Effect != "allow" {
		return nil, errors.Join(ErrSpaceOwnerDenied, err)
	}
	var manifest *StyleManifest
	var check func(context.Context) error
	if build != nil {
		pkg, check, err = build(ctx, initial)
		if err != nil {
			return nil, err
		}
	}
	if operation == "style_apply" || variant {
		encoded, err := json.Marshal(pkg)
		var detached StylePackage
		if err != nil || json.Unmarshal(encoded, &detached) != nil {
			return nil, ErrSpaceOwnerInvalid
		}
		pkg = detached
		built := BuildStyleApply(initial.Owner.Owner, initial.Owner.Space, pkg)
		if !built.Validation.Valid || built.Applied == nil {
			return nil, ErrInvalidStyleExport
		}
		manifest = cloneManifest(built.Applied)
	}
	if operation == "style_rollback" && latestCheckedStyleSnapshot(initial) == nil {
		return nil, ErrStyleSnapshotNotFound
	}
	if operation == "style_rollback" {
		latest := latestCheckedStyleSnapshot(initial)
		if latest.StyleManifest != nil && !ValidateStylePackage(StylePackage{Manifest: *latest.StyleManifest}).Valid {
			return nil, ErrInvalidStyleExport
		}
	}
	bound := func(ctx context.Context) error {
		current, err := spaceOwnerPrincipal(ctx, resolve)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(p, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := s.ensureEnabled(); err != nil {
			return err
		}
		if check != nil {
			return check(ctx)
		}
		return nil
	}
	var ack SpaceMutationAck
	code := "feature.personal_space." + operation
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "personal_space", ResourceID: p.Actor.ID, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("personal_space.style.updated", "personal_space", p.Actor.ID, ack)
	}}, func(tx context.Context) error {
		if err := bound(tx); err != nil {
			return err
		}
		current, err := s.loadSpaceStyleFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := identitypolicy.CheckPersonalSpaceOwnerDecision(request, decision, spaceOwnerRequest(current.Owner, p, operation, request.RequestID)); err != nil {
			return err
		}
		expected := canonicalOwnerSpace(current.Owner.Space)
		at := time.Now().UTC().Truncate(time.Microsecond)
		if current.Owner.Raw == nil {
			expected.ID = fmt.Sprint(idgen.New())
			expected.CreatedAt = at
		}
		expected.UpdatedAt = at
		expected.IsDefault = false
		var added *StyleSnapshot
		if operation != "style_rollback" {
			reason := "before_apply"
			if operation == "style_default" {
				reason = "before_restore_default"
			}
			old := current.Owner.Space
			added = &StyleSnapshot{ID: fmt.Sprint(idgen.New()), UserID: p.Actor.ID, SnapshotType: reason, Theme: old.Theme, Layout: old.Layout, StyleName: old.StyleName, StyleVersion: old.StyleVersion, StyleManifest: cloneManifest(old.StyleManifest), CreatedAt: at}
			if err := snapshots.SaveStyleSnapshot(tx, canonicalStyleSnapshot(added)); err != nil {
				return err
			}
		}
		switch operation {
		case "style_apply", "style_html", "style_zip", "style_source":
			expected.Theme, expected.Layout, expected.StyleName, expected.StyleVersion = manifest.Name, manifest.Layout, manifest.Name, manifest.Version
			expected.StyleManifest = cloneManifest(manifest)
		case "style_default":
			expected.Theme, expected.Layout = "default", "blog"
			clearAppliedStyle(expected)
		case "style_rollback":
			snapshot := latestCheckedStyleSnapshot(current)
			if snapshot == nil {
				return ErrStyleSnapshotNotFound
			}
			expected.Theme, expected.Layout, expected.StyleName, expected.StyleVersion = snapshot.Theme, snapshot.Layout, snapshot.StyleName, snapshot.StyleVersion
			expected.StyleManifest = cloneManifest(snapshot.StyleManifest)
		}
		if err := s.repo.Upsert(tx, canonicalOwnerSpace(expected)); err != nil {
			return err
		}
		result, err := s.loadSpaceStyleFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		changed := current.Owner.Raw == nil || !sameSpaceMetadata(current.Owner.Space, expected)
		if result.Owner.Space.FactsVersion < 1 || changed && result.Owner.Space.FactsVersion <= current.Owner.Space.FactsVersion || !changed && result.Owner.Space.FactsVersion != current.Owner.Space.FactsVersion {
			return identityport.ErrPolicyFactsChanged
		}
		expected.FactsVersion = result.Owner.Space.FactsVersion
		if !reflect.DeepEqual(expected, result.Owner.Space) || !reflect.DeepEqual(current.Owner.Owner, result.Owner.Owner) {
			return identityport.ErrPolicyFactsChanged
		}
		want := len(current.Raw)
		if added != nil {
			want++
		}
		if len(result.Raw) != want {
			return identityport.ErrPolicyFactsChanged
		}
		for id, raw := range current.Raw {
			if !reflect.DeepEqual(raw, result.Raw[id]) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if added != nil {
			actual := result.Snapshots[added.ID]
			if actual == nil || actual.FactsVersion < 1 {
				return identityport.ErrPolicyFactsChanged
			}
			maxVersion := current.Owner.Space.FactsVersion
			for _, snapshot := range current.Snapshots {
				if snapshot.FactsVersion > maxVersion {
					maxVersion = snapshot.FactsVersion
				}
			}
			if actual.FactsVersion <= maxVersion {
				return identityport.ErrPolicyFactsChanged
			}
			added.FactsVersion = actual.FactsVersion
			if !reflect.DeepEqual(canonicalStyleSnapshot(added), actual) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if err := bound(tx); err != nil {
			return err
		}
		final, err := s.loadSpaceStyleFacts(tx, p.Actor.ID, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := bound(tx); err != nil {
			return err
		}
		ack = SpaceMutationAck{SpaceID: result.Owner.Space.ID, OwnerID: p.Actor.ID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ack, nil
}
