package service

import (
	"context"
	"reflect"
	"sort"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

const MaxAdminThreadPreviewItems = 20

// IDs are the only transport input. Results cannot be replayed as execution proof.
type AdminThreadPermissionsPreview struct {
	ID          string                     `json:"id"`
	Permissions AdminThreadPermissionFlags `json:"permissions"`
}
type AdminThreadPermissionFlags struct {
	Pin           bool `json:"pin"`
	Unpin         bool `json:"unpin"`
	Lock          bool `json:"lock"`
	Unlock        bool `json:"unlock"`
	Approve       bool `json:"approve"`
	Reject        bool `json:"reject"`
	TakeDown      bool `json:"take_down"`
	DirectRestore bool `json:"direct_restore"`
	Trash         bool `json:"trash"`
	RestoreTrash  bool `json:"restore_trash"`
	Purge         bool `json:"purge"`
}
type adminPreviewSnapshot struct {
	Read       adminThreadReadSnapshot
	Execution  map[string]identityport.AdminExecutionAuthority
	Governance map[string]identityport.BoardGovernanceAuthority
}

var adminPreviewCodes = []string{"community.thread.pin", "community.thread.lock", "community.thread.review", "community.thread.trash", "community.thread.restore", "community.thread.direct_restore", "community.thread.purge"}

func validateAdminThreadPreview(ids []string) error {
	if len(ids) < 1 || len(ids) > MaxAdminThreadPreviewItems {
		return ErrAdminThreadMetadataInvalid
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !adminMetadataID.MatchString(id) || seen[id] {
			return ErrAdminThreadMetadataInvalid
		}
		seen[id] = true
	}
	return nil
}
func metadataAsPolicyThread(row repository.AdminThreadMetadata) *domain.Thread {
	result := &domain.Thread{ID: row.ID, AuthorID: row.AuthorID, CategoryID: row.CategoryID, ThreadType: row.ThreadType, ContentFormat: row.ContentFormat, PublicationStatus: row.PublicationStatus, ModerationStatus: row.ModerationStatus, DeletionStatus: row.DeletionStatus, IsPinned: row.IsPinned, IsLocked: row.IsLocked, FactsVersion: row.FactsVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if row.PhysicalDeleted {
		at := row.UpdatedAt
		result.DeletedAt = &at
	}
	return result
}
func (s *ThreadService) loadAdminThreadPreview(ctx context.Context, ids []string, resolve identityport.SessionPrincipalResolver) (adminPreviewSnapshot, error) {
	var snapshot adminPreviewSnapshot
	read, err := s.loadAdminThreadRead(ctx, "", resolve)
	if err != nil {
		return snapshot, err
	}
	snapshot.Read = read
	if nilGovernancePort(s.governanceAuthority) {
		return snapshot, ErrThreadGovernanceUnavailable
	}
	rows := map[string]repository.AdminThreadMetadata{}
	for _, row := range read.Inventory {
		rows[row.ID] = row
	}
	boards := map[string]bool{}
	for _, id := range ids {
		row, exists := rows[id]
		if !exists {
			return snapshot, repository.ErrThreadNotFound
		}
		allowed, err := adminMetadataAllowed(row, read.Authority[row.CategoryID], "history")
		if err != nil {
			return snapshot, err
		}
		if !allowed {
			return snapshot, ErrThreadGovernanceDenied
		}
		boards[row.CategoryID] = true
	}
	keys := []string{}
	for board := range boards {
		keys = append(keys, board)
	}
	sort.Strings(keys)
	snapshot.Execution = map[string]identityport.AdminExecutionAuthority{}
	snapshot.Governance = map[string]identityport.BoardGovernanceAuthority{}
	load := func(q identityport.AdminExecutionQuery) (identityport.AdminExecutionAuthority, error) {
		a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
		if err != nil {
			return a, threadGovernanceError(err)
		}
		if a.Query != q || identityport.CheckAdminExecutionAuthority(a, a) != nil || !reflect.DeepEqual(a.Principal, read.Authority[""].Principal) {
			return a, identityport.ErrPolicyFactsChanged
		}
		return a, nil
	}
	for _, board := range keys {
		for _, code := range adminPreviewCodes {
			q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: board}}
			a, err := load(q)
			if err != nil {
				return snapshot, err
			}
			snapshot.Execution[board+"/"+code] = a
		}
		q := identityport.BoardGovernanceQuery{BoardID: board, Action: "community.thread.take_down"}
		a, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, q, resolve, false)
		if err != nil {
			return snapshot, threadGovernanceError(err)
		}
		if identityport.CheckBoardGovernanceAuthority(a, a) != nil || !reflect.DeepEqual(a.Principal, read.Authority[""].Principal) {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
		snapshot.Governance[board] = a
	}
	for _, board := range keys {
		for _, code := range adminPreviewCodes {
			a := snapshot.Execution[board+"/"+code]
			current, err := load(a.Query)
			if err != nil {
				return snapshot, err
			}
			if identityport.CheckAdminExecutionAuthority(a, current) != nil {
				return snapshot, identityport.ErrPolicyFactsChanged
			}
		}
		a := snapshot.Governance[board]
		current, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, identityport.BoardGovernanceQuery{BoardID: board, Action: "community.thread.take_down"}, resolve, false)
		if err != nil {
			return snapshot, threadGovernanceError(err)
		}
		if identityport.CheckBoardGovernanceAuthority(a, current) != nil {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
	}
	// Prove resources and the full current role/session witness after every action loader.
	current, err := s.loadAdminThreadRead(ctx, "", resolve)
	if err != nil {
		return snapshot, err
	}
	if !reflect.DeepEqual(read, current) {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	for _, a := range snapshot.Execution {
		if a.Allowed && time.Now().UnixMilli() >= a.ExpiresAtMS {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
func previewAdminThreadFlags(row repository.AdminThreadMetadata, snapshot adminPreviewSnapshot) (AdminThreadPermissionFlags, error) {
	var result AdminThreadPermissionFlags
	if row.PhysicalDeleted {
		return result, nil
	}
	thread := metadataAsPolicyThread(row)
	now := time.Now().UnixMilli()
	requestID := "admin-current-preview"
	for _, op := range []string{"pin", "unpin", "lock", "unlock"} {
		code, _ := adminFlagCode(op)
		a := snapshot.Execution[row.CategoryID+"/"+code]
		r, err := adminFlagRequest(thread, a, op, requestID, now)
		if err != nil {
			return result, err
		}
		d, err := (policy.ThreadAdminFlagPolicy{}).DecideThreadAdminFlag(r)
		if err != nil {
			return result, err
		}
		allowed := d.Effect == "allow"
		switch op {
		case "pin":
			result.Pin = allowed
		case "unpin":
			result.Unpin = allowed
		case "lock":
			result.Lock = allowed
		case "unlock":
			result.Unlock = allowed
		}
	}
	for _, op := range []string{"approve", "reject"} {
		a := snapshot.Execution[row.CategoryID+"/community.thread.review"]
		r, err := adminReviewRequest(thread, a, op, requestID, now)
		if err != nil {
			return result, err
		}
		d, err := (policy.ThreadAdminReviewPolicy{}).DecideThreadAdminReview(r)
		if err != nil {
			return result, err
		}
		if op == "approve" {
			result.Approve = d.Effect == "allow"
		} else {
			result.Reject = d.Effect == "allow"
		}
	}
	for _, op := range []string{"trash", "restore", "direct_restore"} {
		a := snapshot.Execution[row.CategoryID+"/community.thread."+op]
		r, err := adminLifecycleRequest(thread, a, op, requestID, now)
		if err != nil {
			return result, err
		}
		d, err := (policy.ThreadAdminLifecyclePolicy{}).DecideThreadAdminLifecycle(r)
		if err != nil {
			return result, err
		}
		switch op {
		case "trash":
			result.Trash = d.Effect == "allow"
		case "restore":
			result.RestoreTrash = d.Effect == "allow"
		case "direct_restore":
			result.DirectRestore = d.Effect == "allow"
		}
	}
	a := snapshot.Execution[row.CategoryID+"/community.thread.purge"]
	r, err := adminPurgeRequest(thread, a, "purge", requestID, now)
	if err != nil {
		return result, err
	}
	d, err := (policy.ThreadAdminPurgePolicy{}).DecideThreadAdminPurge(r)
	if err != nil {
		return result, err
	}
	result.Purge = d.Effect == "allow"
	governance := snapshot.Governance[row.CategoryID]
	request, err := takeDownPolicyRequest(thread, governance, requestID)
	if err != nil {
		return result, err
	}
	decision, err := (policy.ResourcePolicy{}).DecideResource(request)
	if err != nil {
		return result, err
	}
	result.TakeDown = decision.Effect == "allow" && thread.ModerationStatus != domain.ModerationStatusTakenDown
	return result, nil
}
func (s *ThreadService) PreviewAdminThreadPermissions(ctx context.Context, ids []string, resolve identityport.SessionPrincipalResolver) ([]AdminThreadPermissionsPreview, error) {
	if err := validateAdminThreadPreview(ids); err != nil {
		return nil, err
	}
	ids = append([]string(nil), ids...)
	first, err := s.loadAdminThreadPreview(ctx, ids, resolve)
	if err != nil {
		return nil, err
	}
	rows := map[string]repository.AdminThreadMetadata{}
	for _, row := range first.Read.Inventory {
		rows[row.ID] = row
	}
	result := make([]AdminThreadPermissionsPreview, 0, len(ids))
	for _, id := range ids {
		permissions, err := previewAdminThreadFlags(rows[id], first)
		if err != nil {
			return nil, err
		}
		result = append(result, AdminThreadPermissionsPreview{ID: id, Permissions: permissions})
	}
	for range 2 {
		current, err := s.loadAdminThreadPreview(ctx, ids, resolve)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(first, current) {
			return nil, identityport.ErrPolicyFactsChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
