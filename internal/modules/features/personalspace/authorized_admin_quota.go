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
	"time"
)

type SpaceQuotaMutationAck struct {
	OwnerID    string `json:"owner_id"`
	QuotaBytes int64  `json:"quota_bytes"`
}
type spaceQuotaAdminFacts struct {
	User         identityport.User
	Quota        corestorage.QuotaRecord
	Exists       bool
	DefaultQuota int64
}

func canonicalQuotaRecord(q corestorage.QuotaRecord) corestorage.QuotaRecord {
	q.CreatedAt = q.CreatedAt.UTC()
	q.UpdatedAt = q.UpdatedAt.UTC()
	return q
}
func (s *Service) loadQuotaAdminFacts(ctx context.Context, owner string, lock bool) (*spaceQuotaAdminFacts, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if corestorage.NilImageAdapter(s.users) || corestorage.NilImageAdapter(s.quotaManager) {
		return nil, ErrSpaceFactsUnavailable
	}
	var user *identitydomain.User
	var err error
	if lock {
		reader, ok := s.users.(interface {
			GetByIDForUpdate(context.Context, string) (*identitydomain.User, error)
		})
		if !ok || corestorage.NilImageAdapter(reader) {
			return nil, ErrSpaceFactsUnavailable
		}
		user, err = reader.GetByIDForUpdate(ctx, owner)
	} else {
		user, err = s.users.GetByID(ctx, owner)
	}
	if err != nil || user == nil || user.ID != owner {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	f := &spaceQuotaAdminFacts{User: spaceAdminUser(user), DefaultQuota: s.quotaManager.DefaultQuotaBytes()}
	if f.DefaultQuota < 1 {
		return nil, ErrStorageQuotaUnavailable
	}
	if lock {
		reader, ok := s.quotaManager.(corestorage.ManagedQuotaCommands)
		if !ok || corestorage.NilImageAdapter(reader) {
			return nil, ErrStorageQuotaUnavailable
		}
		f.Quota, f.Exists, err = reader.CurrentQuotaForAdminCommand(ctx, owner)
	} else {
		reader, ok := s.quotaManager.(corestorage.CurrentQuotaReader)
		if !ok || corestorage.NilImageAdapter(reader) {
			return nil, ErrStorageQuotaUnavailable
		}
		f.Quota, f.Exists, err = reader.CurrentQuota(ctx, owner)
	}
	if err != nil {
		return nil, err
	}
	f.Quota = canonicalQuotaRecord(f.Quota)
	if f.Exists && (f.Quota.UserID != owner || f.Quota.QuotaBytes < 1 || f.Quota.FactsVersion < 1) || !f.Exists && f.Quota != (corestorage.QuotaRecord{}) {
		return nil, ErrStorageQuotaUnavailable
	}
	current, err := s.users.GetByID(ctx, owner)
	if err != nil || current == nil {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	if !reflect.DeepEqual(f.User, spaceAdminUser(current)) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	return f, nil
}
func spaceQuotaAdminRequest(f *spaceQuotaAdminFacts, a identityport.AdminExecutionAuthority, limit int64, id string, at int64) identityport.PersonalSpaceQuotaAdminRequest {
	return identityport.PersonalSpaceQuotaAdminRequest{RequestID: id, PolicyVersion: identityport.PersonalSpaceQuotaAdminPolicyVersion, OwnerID: f.User.ID, UserStatus: f.User.Status, Principal: a.Principal, Exists: f.Exists, FactsVersion: f.Quota.FactsVersion, RequestedQuotaBytes: limit, ExecutionQuery: a.Query, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: at}
}
func (s *Service) SetStorageQuotaForPrincipal(ctx context.Context, owner string, limit int64, resolve identityport.SessionPrincipalResolver) (*SpaceQuotaMutationAck, error) {
	if s == nil || s.reliable == nil || corestorage.NilImageAdapter(s.adminExecution) || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrAdminSessionPrincipalUnavailable
	}
	manager := s.quotaManager
	writer, ok := manager.(corestorage.ManagedQuotaCommands)
	if !ok || corestorage.NilImageAdapter(writer) {
		return nil, ErrStorageQuotaUnavailable
	}
	if limit < 1048576 || limit > 107374182400 {
		return nil, ErrStorageQuotaInvalid
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: identityport.PolicyResourceRef{Kind: "personal_space.storage_quota", ID: owner}}
	a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, err
	}
	if !a.Allowed {
		return nil, ErrSpaceAdminDenied
	}
	initial, err := s.loadQuotaAdminFacts(ctx, owner, false)
	if err != nil {
		return nil, err
	}
	r := spaceQuotaAdminRequest(initial, a, limit, fmt.Sprint(idgen.New()), time.Now().UnixMilli())
	d, err := (identitypolicy.PersonalSpaceQuotaAdminPolicy{}).DecidePersonalSpaceQuotaAdmin(r)
	if err != nil {
		return nil, err
	}
	if d.Effect != "allow" {
		return nil, ErrSpaceAdminDenied
	}
	check := func(ctx context.Context, lock bool) (identityport.AdminExecutionAuthority, error) {
		before, currentSource := reflect.ValueOf(manager), reflect.ValueOf(s.quotaManager)
		if !before.IsValid() || !currentSource.IsValid() || !before.Comparable() || !currentSource.Comparable() || before.Interface() != currentSource.Interface() {
			return identityport.AdminExecutionAuthority{}, identityport.ErrPolicyFactsChanged
		}
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
	ack := SpaceQuotaMutationAck{OwnerID: owner, QuotaBytes: limit}
	var receipt corestorage.QuotaCommandReceipt
	code := "feature.personal_space.admin_set_quota"
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: a.Principal.Actor.ID, ActorType: "admin", ResourceType: "storage_quota", ResourceID: owner, PermissionCode: q.PermissionCode, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("personal_space.quota_updated", "storage_quota", owner, ack)
	}}, func(tx context.Context) error {
		currentAuthority, err := check(tx, true)
		if err != nil {
			return err
		}
		current, err := s.loadQuotaAdminFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := identitypolicy.CheckPersonalSpaceQuotaAdminDecision(r, d, spaceQuotaAdminRequest(current, currentAuthority, limit, r.RequestID, r.EvaluatedAtMS)); err != nil {
			return err
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		expected := corestorage.QuotaRecord{UserID: owner, QuotaBytes: limit, CreatedAt: at, UpdatedAt: at, UpdatedActorKind: "admin", UpdatedActorID: a.Principal.Actor.ID}
		if current.Exists {
			expected.CreatedAt = current.Quota.CreatedAt
		}
		receipt, err = writer.WriteAdminQuotaForCommand(tx, owner, current.Quota.FactsVersion, limit, a.Principal.Actor.ID, at)
		if err != nil {
			return err
		}
		written := canonicalQuotaRecord(receipt.Record())
		changed := expected
		changed.FactsVersion = current.Quota.FactsVersion
		if written.FactsVersion < 1 || !reflect.DeepEqual(changed, current.Quota) && written.FactsVersion <= current.Quota.FactsVersion {
			return identityport.ErrPolicyFactsChanged
		}
		expected.FactsVersion = written.FactsVersion
		if !reflect.DeepEqual(expected, written) {
			return identityport.ErrPolicyFactsChanged
		}
		result, err := s.loadQuotaAdminFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !result.Exists || !reflect.DeepEqual(result.Quota, expected) || !reflect.DeepEqual(current.User, result.User) || current.DefaultQuota != result.DefaultQuota {
			return identityport.ErrPolicyFactsChanged
		}
		if _, err := check(tx, true); err != nil {
			return err
		}
		final, err := s.loadQuotaAdminFacts(tx, owner, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, final) {
			return identityport.ErrPolicyFactsChanged
		}
		_, err = check(tx, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := writer.PublishCommittedQuota(context.WithoutCancel(ctx), receipt); err != nil {
		return nil, err
	}
	return &ack, nil
}
