package service

import (
	"context"
	"errors"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

var ErrAdminThreadMetadataInvalid = errors.New("invalid admin metadata filter")
var adminMetadataID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type adminThreadReadSnapshot struct {
	Inventory []repository.AdminThreadMetadata
	Authority map[string]identityport.AdminExecutionAuthority
	History   []repository.AdminModerationMetadata
}

func (s *ThreadService) adminMetadataAuthority(ctx context.Context, q identityport.AdminExecutionQuery, resolve identityport.SessionPrincipalResolver) (identityport.AdminExecutionAuthority, error) {
	a, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return a, threadGovernanceError(err)
	}
	if a.Query != q || a.Principal.Audience != "admin" || identityport.CheckAdminExecutionAuthority(a, a) != nil || time.Now().UnixMilli() >= a.ExpiresAtMS {
		return a, identityport.ErrPolicyFactsChanged
	}
	return a, nil
}
func (s *ThreadService) loadAdminThreadRead(ctx context.Context, target string, resolve identityport.SessionPrincipalResolver) (adminThreadReadSnapshot, error) {
	var snapshot adminThreadReadSnapshot
	if s == nil || nilGovernancePort(s.adminExecution) {
		return snapshot, ErrThreadGovernanceUnavailable
	}
	reader, ok := s.repo.(repository.AdminThreadMetadataReader)
	if !ok || nilGovernancePort(reader) {
		return snapshot, ErrThreadGovernanceUnavailable
	}
	aggregate := identityport.AdminExecutionQuery{PermissionCode: "community.thread.read", Scope: identityport.PolicyResourceRef{Kind: "community.thread_inventory", ID: "all"}}
	initial, err := s.adminMetadataAuthority(ctx, aggregate, resolve)
	if err != nil {
		return snapshot, err
	}
	if !initial.Allowed {
		return snapshot, ErrThreadGovernanceDenied
	}
	snapshot.Authority = map[string]identityport.AdminExecutionAuthority{"": initial}
	snapshot.Inventory, err = reader.ListAdminThreadMetadata(ctx)
	if err != nil {
		return snapshot, threadGovernanceError(err)
	}
	boards := map[string]bool{}
	seen := map[string]bool{}
	found := false
	for _, row := range snapshot.Inventory {
		if !adminMetadataID.MatchString(row.ID) || seen[row.ID] || row.FactsVersion < 1 {
			return snapshot, ErrThreadGovernanceUnavailable
		}
		seen[row.ID] = true
		facts := adminMetadataThreadFacts(row)
		if facts.Validate() != nil {
			continue
		}
		boards[row.CategoryID] = true
		if row.ID == target {
			found = true
		}
	}
	keys := make([]string, 0, len(boards))
	for board := range boards {
		keys = append(keys, board)
	}
	sort.Strings(keys)
	for _, board := range keys {
		q := identityport.AdminExecutionQuery{PermissionCode: "community.thread.read", Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: board}}
		a, err := s.adminMetadataAuthority(ctx, q, resolve)
		if err != nil {
			return snapshot, err
		}
		if !reflect.DeepEqual(a.Principal, initial.Principal) {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
		snapshot.Authority[board] = a
	}
	if target != "" {
		if !found {
			return snapshot, repository.ErrThreadNotFound
		}
		for _, row := range snapshot.Inventory {
			if row.ID == target {
				allowed, err := adminMetadataAllowed(row, snapshot.Authority[row.CategoryID], "history")
				if err != nil {
					return snapshot, err
				}
				if !allowed {
					return snapshot, ErrThreadGovernanceDenied
				}
			}
		}
		history, ok := s.governance.(repository.AdminModerationMetadataReader)
		if !ok || nilGovernancePort(history) {
			return snapshot, ErrThreadGovernanceUnavailable
		}
		snapshot.History, err = history.ReadAdminModerationMetadata(ctx, target)
		if err != nil {
			return snapshot, threadGovernanceError(err)
		}
		ids := map[string]bool{}
		for _, row := range snapshot.History {
			if row.ThreadID != target || !adminMetadataID.MatchString(row.ID) || ids[row.ID] {
				return snapshot, ErrThreadGovernanceUnavailable
			}
			ids[row.ID] = true
		}
	}
	// A later resource/history adapter must not change an earlier item silently.
	current, err := reader.ListAdminThreadMetadata(ctx)
	if err != nil {
		return snapshot, threadGovernanceError(err)
	}
	if !reflect.DeepEqual(snapshot.Inventory, current) {
		return snapshot, identityport.ErrPolicyFactsChanged
	}
	for _, key := range append([]string{""}, keys...) {
		expected := snapshot.Authority[key]
		current, err := s.adminMetadataAuthority(ctx, expected.Query, resolve)
		if err != nil {
			return snapshot, err
		}
		if identityport.CheckAdminExecutionAuthority(expected, current) != nil {
			return snapshot, identityport.ErrPolicyFactsChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
func adminMetadataThreadFacts(row repository.AdminThreadMetadata) identityport.ThreadPublicReadFacts {
	return identityport.ThreadPublicReadFacts{Resource: identityport.PolicyResourceRef{Kind: "community.thread", ID: row.ID}, Owner: identityport.PrincipalRef{Kind: "user", ID: row.AuthorID}, Board: identityport.PolicyResourceRef{Kind: "community.board", ID: row.CategoryID}, FactsVersion: strconv.FormatInt(row.FactsVersion, 10), PublicationStatus: string(row.PublicationStatus), ModerationStatus: string(row.ModerationStatus), DeletionStatus: string(row.DeletionStatus)}
}
func adminMetadataAllowed(row repository.AdminThreadMetadata, a identityport.AdminExecutionAuthority, operation string) (bool, error) {
	facts := adminMetadataThreadFacts(row)
	if facts.Validate() != nil {
		return false, nil
	}
	r := identityport.ThreadAdminMetadataRequest{PolicyVersion: identityport.ThreadAdminMetadataPolicyVersion, RequestID: "admin-metadata-read", Operation: operation, Principal: a.Principal, Thread: facts, ExecutionQuery: a.Query, ExecutionAllowed: a.Allowed, ExecutionExpiresAtMS: a.ExpiresAtMS, EvaluatedAtMS: time.Now().UnixMilli()}
	d, err := (policy.ThreadAdminMetadataPolicy{}).DecideThreadAdminMetadata(r)
	if err != nil {
		return false, err
	}
	if d.Effect != "allow" {
		return false, nil
	}
	return true, identityport.CheckThreadAdminMetadataDecision(r, d, r)
}
func validateAdminMetadataFilter(f domain.ThreadListFilter, operation string) (domain.ThreadListFilter, error) {
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 || int64(f.Page-1) > math.MaxInt64/int64(f.PageSize) || len(f.Keyword) > 128 || f.Tag != "" || len(f.AnyTags) > 0 || f.CategoryIDs != nil || f.Status != "" && f.Status != "all" {
		return f, ErrAdminThreadMetadataInvalid
	}
	for _, id := range []string{f.CategoryID, f.AuthorID, f.Keyword} {
		if id != "" {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n < 1 {
				return f, ErrAdminThreadMetadataInvalid
			}
		}
	}
	if f.PublicationStatus != "" && f.PublicationStatus != "published" && f.PublicationStatus != "draft" && f.PublicationStatus != "private" {
		return f, ErrAdminThreadMetadataInvalid
	}
	if f.ModerationStatus != "" && f.ModerationStatus != "clear" && f.ModerationStatus != "pending" && f.ModerationStatus != "rejected" && f.ModerationStatus != "taken_down" {
		return f, ErrAdminThreadMetadataInvalid
	}
	if f.ContentFormat != "" && f.ContentFormat != "markdown" && f.ContentFormat != "richtext_article" {
		return f, ErrAdminThreadMetadataInvalid
	}
	deletion := "active"
	if operation == "trash" {
		deletion = "trashed"
	}
	if f.DeletionStatus != "" && f.DeletionStatus != deletion {
		return f, ErrAdminThreadMetadataInvalid
	}
	f.DeletionStatus = deletion
	return f, nil
}
func (s *ThreadService) ListAdminThreadMetadata(ctx context.Context, resolve identityport.SessionPrincipalResolver, filter domain.ThreadListFilter, trash bool) ([]repository.AdminThreadMetadata, int64, error) {
	operation := "list"
	if trash {
		operation = "trash"
	}
	filter, err := validateAdminMetadataFilter(filter, operation)
	if err != nil {
		return nil, 0, err
	}
	first, err := s.loadAdminThreadRead(ctx, "", resolve)
	if err != nil {
		return nil, 0, err
	}
	rows := []repository.AdminThreadMetadata{}
	for _, row := range first.Inventory {
		allowed, err := adminMetadataAllowed(row, first.Authority[row.CategoryID], operation)
		if err != nil {
			return nil, 0, err
		}
		if !allowed || row.PhysicalDeleted || filter.CategoryID != "" && row.CategoryID != filter.CategoryID || filter.AuthorID != "" && row.AuthorID != filter.AuthorID || filter.Keyword != "" && !strings.Contains(row.ID, filter.Keyword) || filter.ContentFormat != "" && row.ContentFormat != filter.ContentFormat || filter.ThreadType != "" && row.ThreadType != filter.ThreadType || filter.PublicationStatus != "" && string(row.PublicationStatus) != filter.PublicationStatus || filter.ModerationStatus != "" && string(row.ModerationStatus) != filter.ModerationStatus {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsPinned != rows[j].IsPinned {
			return rows[i].IsPinned
		}
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	total := int64(len(rows))
	offset := int64(filter.Page-1) * int64(filter.PageSize)
	page := []repository.AdminThreadMetadata{}
	if offset < total {
		end := total
		if int64(filter.PageSize) < total-offset {
			end = offset + int64(filter.PageSize)
		}
		page = append(page, rows[int(offset):int(end)]...)
	}
	// Decisions/count/page are one projection; a whole/empty final proof is required.
	for range 2 {
		current, err := s.loadAdminThreadRead(ctx, "", resolve)
		if err != nil {
			return nil, 0, err
		}
		if !reflect.DeepEqual(first, current) {
			return nil, 0, identityport.ErrPolicyFactsChanged
		}
	}
	return page, total, nil
}
func (s *ThreadService) AdminModerationMetadata(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver, page, pageSize int) ([]repository.AdminModerationMetadata, int64, error) {
	if !adminMetadataID.MatchString(id) || page < 1 || pageSize < 1 || pageSize > 100 || int64(page-1) > math.MaxInt64/int64(pageSize) {
		return nil, 0, ErrAdminThreadMetadataInvalid
	}
	first, err := s.loadAdminThreadRead(ctx, id, resolve)
	if err != nil {
		return nil, 0, err
	}
	total := int64(len(first.History))
	offset := int64(page-1) * int64(pageSize)
	result := []repository.AdminModerationMetadata{}
	actions := map[string]bool{}
	for _, op := range []string{"create", "edit", "update", "publish", "unpublish", "trash", "restore", "restore_version", "submit_review", "approve", "reject", "take_down", "direct_restore", "restore_trash", "purge", "pin", "unpin", "lock", "unlock"} {
		actions[op] = true
	}
	if offset < total {
		end := total
		if int64(pageSize) < total-offset {
			end = offset + int64(pageSize)
		}
		for _, row := range first.History[int(offset):int(end)] {
			if !actions[row.Action] {
				row.Action = "legacy_unknown"
			}
			if row.ActorKind != "user" && row.ActorKind != "admin" || !adminMetadataID.MatchString(row.ActorRefID) {
				row.ActorKind = "legacy_unknown"
				row.ActorRefID = ""
			}
			result = append(result, row)
		}
	}
	for range 2 {
		current, err := s.loadAdminThreadRead(ctx, id, resolve)
		if err != nil {
			return nil, 0, err
		}
		if !reflect.DeepEqual(first, current) {
			return nil, 0, identityport.ErrPolicyFactsChanged
		}
	}
	return result, total, nil
}
