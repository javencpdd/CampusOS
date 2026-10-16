package space

import (
	"context"
	"errors"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"reflect"
	"time"
)

func (s *Service) spaceAdminReadAuthority(ctx context.Context, p identityport.PrincipalContext, operation, owner, status string, resolve identityport.SessionPrincipalResolver) (identityport.AdminExecutionAuthority, error) {
	if s == nil || corestorage.NilImageAdapter(s.adminExecution) {
		return identityport.AdminExecutionAuthority{}, ErrSpaceFactsUnavailable
	}
	if err := s.ensureEnabled(); err != nil {
		return identityport.AdminExecutionAuthority{}, err
	}
	ref := identityport.PolicyResourceRef{Kind: "personal_space." + operation, ID: owner}
	if operation == "space_summary" {
		ref.ID = "summary"
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: ref}
	a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return a, err
	}
	if a.Query != q {
		return a, identityport.ErrPolicyFactsChanged
	}
	if err := identityport.CheckAdminExecutionAuthority(a, a); err != nil {
		return a, err
	}
	if !reflect.DeepEqual(p, a.Principal) {
		return a, identityport.ErrPolicyFactsChanged
	}
	r := identityport.PersonalSpaceAdminReadRequest{RequestID: "space-admin-metadata-read", PolicyVersion: identityport.PersonalSpaceAdminReadPolicyVersion, Operation: operation, OwnerID: owner, UserStatus: status, Principal: p, ExecutionQuery: q, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: time.Now().UnixMilli()}
	d, err := (identitypolicy.PersonalSpaceAdminReadPolicy{}).DecidePersonalSpaceAdminRead(r)
	if err != nil {
		return a, err
	}
	if d.Effect != "allow" {
		return a, ErrSpaceAdminDenied
	}
	return a, nil
}

type spaceAdminSummarySnapshot struct {
	Authority identityport.AdminExecutionAuthority
	Rows      []spaceAdminSummaryRow
}

func (s *Service) AdminSummaryForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceAdminSummary, error) {
	return readCurrentSpaceAdminProjection(ctx, resolve, func(ctx context.Context, p identityport.PrincipalContext) (spaceAdminSummarySnapshot, error) {
		a, err := s.spaceAdminReadAuthority(ctx, p, "space_summary", "", "", resolve)
		if err != nil {
			return spaceAdminSummarySnapshot{}, err
		}
		reader, ok := s.repo.(spaceAdminInventoryReader)
		if !ok || corestorage.NilImageAdapter(reader) {
			return spaceAdminSummarySnapshot{}, ErrSpaceFactsUnavailable
		}
		rows, err := reader.LoadSpaceAdminInventory(ctx)
		if err != nil {
			return spaceAdminSummarySnapshot{}, err
		}
		seen := map[string]bool{}
		for _, row := range rows {
			if row.ID == "" || row.OwnerID == "" || row.FactsVersion < 1 || seen[row.OwnerID] || !validVisibility(row.Visibility) {
				return spaceAdminSummarySnapshot{}, ErrSpaceFactsUnavailable
			}
			seen[row.OwnerID] = true
		}
		final, err := s.spaceAdminReadAuthority(ctx, p, "space_summary", "", "", resolve)
		if err != nil {
			return spaceAdminSummarySnapshot{}, err
		}
		if err := identityport.CheckAdminExecutionAuthority(a, final); err != nil {
			return spaceAdminSummarySnapshot{}, err
		}
		return spaceAdminSummarySnapshot{Authority: a, Rows: rows}, nil
	}, func(_ context.Context, _ identityport.PrincipalContext, f spaceAdminSummarySnapshot) (*SpaceAdminSummary, error) {
		summary := &SpaceAdminSummary{TotalSpaces: int64(len(f.Rows))}
		for _, row := range f.Rows {
			if row.Visibility == VisibilityPublic && !row.Disabled {
				summary.PublicSpaces++
			}
			if row.Disabled {
				summary.DisabledSpaces++
			}
			if row.Styled {
				summary.StyledSpaces++
			}
			if row.SyncEnabled {
				summary.SyncEnabledSpaces++
			}
			if row.HasSyncError {
				summary.SyncErrorSpaces++
			}
			if row.LastSyncAt != nil && (summary.LastSyncAt == nil || row.LastSyncAt.After(*summary.LastSyncAt)) {
				at := *row.LastSyncAt
				summary.LastSyncAt = &at
			}
		}
		return summary, nil
	})
}

type spaceAdminStorageSnapshot struct {
	Authority identityport.AdminExecutionAuthority
	Owner     *spaceAdminFacts
	Quota     *spaceQuotaAdminFacts
	Status    *SpaceStorageStatus
	Config    FileStorageConfig
}

func (s *Service) AdminStorageStatusForPrincipal(ctx context.Context, owner string, resolve identityport.SessionPrincipalResolver) (*SpaceStorageStatus, error) {
	return readCurrentSpaceAdminProjection(ctx, resolve, func(ctx context.Context, p identityport.PrincipalContext) (spaceAdminStorageSnapshot, error) {
		// Authorize the fixed target before exposing its existence or storage.
		q := identityport.AdminExecutionQuery{PermissionCode: "personal_space.space.manage", Scope: identityport.PolicyResourceRef{Kind: "personal_space.storage_status", ID: owner}}
		if s == nil || corestorage.NilImageAdapter(s.adminExecution) {
			return spaceAdminStorageSnapshot{}, ErrSpaceFactsUnavailable
		}
		initial, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		if !initial.Allowed {
			return spaceAdminStorageSnapshot{}, ErrSpaceAdminDenied
		}
		facts, err := s.loadAdminSpaceFacts(ctx, owner, false)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		a, err := s.spaceAdminReadAuthority(ctx, p, "storage_status", owner, facts.User.Status, resolve)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		if err := identityport.CheckAdminExecutionAuthority(initial, a); err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		quota, err := s.loadQuotaAdminFacts(ctx, owner, false)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		if !reflect.DeepEqual(facts.User, quota.User) {
			return spaceAdminStorageSnapshot{}, identityport.ErrPolicyFactsChanged
		}
		if s.fileStore == nil {
			return spaceAdminStorageSnapshot{}, ErrSpaceFileStoreUnavailable
		}
		status, err := s.currentStorageStatus(ctx, owner)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		current, err := s.loadQuotaAdminFacts(ctx, owner, false)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		if !reflect.DeepEqual(quota, current) {
			return spaceAdminStorageSnapshot{}, identityport.ErrPolicyFactsChanged
		}
		if status == nil || status.UserID != owner || status.UsedBytes < 0 {
			return spaceAdminStorageSnapshot{}, errors.New("current storage metadata unavailable")
		}
		final, err := s.spaceAdminReadAuthority(ctx, p, "storage_status", owner, facts.User.Status, resolve)
		if err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		if err := identityport.CheckAdminExecutionAuthority(a, final); err != nil {
			return spaceAdminStorageSnapshot{}, err
		}
		return spaceAdminStorageSnapshot{Authority: a, Owner: facts, Quota: quota, Status: status, Config: s.fileStore.cfg}, nil
	}, func(_ context.Context, _ identityport.PrincipalContext, f spaceAdminStorageSnapshot) (*SpaceStorageStatus, error) {
		return f.Status, nil
	})
}

// This owning-module helper is fixed to the independent Admin domain. Each
// load still applies its named metadata policy and full execution authority.
// It never installs an Admin resolver as a User resolver in context.
func readCurrentSpaceAdminProjection[S, R any](ctx context.Context, resolve identityport.SessionPrincipalResolver, load func(context.Context, identityport.PrincipalContext) (S, error), read func(context.Context, identityport.PrincipalContext, S) (R, error)) (R, error) {
	var zero R
	if resolve == nil {
		return zero, identityport.ErrAdminSessionPrincipalUnavailable
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p, err := resolve(ctx)
	if err != nil {
		return zero, err
	}
	if p.Validate() != nil || p.Actor.Kind != "admin" || p.Audience != "admin" {
		return zero, identityport.ErrAdminSessionPrincipalUnavailable
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := resolve(ctx)
		if err != nil {
			return err
		}
		if current.Validate() != nil || !reflect.DeepEqual(p, current) {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	}
	initial, err := load(ctx, p)
	if err != nil {
		return zero, err
	}
	result, err := read(ctx, p, initial)
	if err != nil {
		return zero, err
	}
	if err := check(); err != nil {
		return zero, err
	}
	current, err := load(ctx, p)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(initial, current) {
		return zero, identityport.ErrPolicyFactsChanged
	}
	if err := check(); err != nil {
		return zero, err
	}
	return result, nil
}
