package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

func TestAdminThreadMetadataListPolicyBeforeCountPageAndPrivateProjection(t *testing.T) {
	ctx := context.Background()
	s, repo, gov, store, admins, _, proof := adminFlagFixture(t)
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "metadata"); err != nil {
		t.Fatal(err)
	}
	for i, state := range []string{"active", "trashed", "active", "active"} {
		row := &domain.Thread{ID: fmt.Sprint(2580101 + i), AuthorID: "190102", CategoryID: "190103", ThreadType: "discussion", ContentFormat: "markdown", Title: "PRIVATE_TITLE", Content: "PRIVATE_BODY", PublicationStatus: "draft", ModerationStatus: "pending", DeletionStatus: domain.DeletionStatus(state), CreatedAt: time.Unix(int64(i+100), 0), UpdatedAt: time.Unix(int64(i+100), 0)}
		if i == 2 {
			row.ModerationStatus = "unknown"
		}
		if i == 3 {
			row.CategoryID = "190104"
		}
		if err := repo.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	s.adminExecution = metadataBoardDeny{AdminExecutionAuthorityReader: s.adminExecution, board: "190104"}
	before, gBefore := repo.Snapshot(), gov.Snapshot()
	for _, tc := range []struct {
		filter domain.ThreadListFilter
		trash  bool
		total  int64
		ids    []string
	}{
		{domain.ThreadListFilter{Page: 1, PageSize: 1}, false, 2, []string{"2580101"}},
		{domain.ThreadListFilter{Page: 2, PageSize: 1}, false, 2, []string{"190101"}},
		{domain.ThreadListFilter{Page: 9, PageSize: 1}, false, 2, []string{}},
		{domain.ThreadListFilter{Page: 1, PageSize: 1}, true, 1, []string{"2580102"}},
		{domain.ThreadListFilter{Keyword: "2580101"}, false, 1, []string{"2580101"}},
		{domain.ThreadListFilter{CategoryID: "190104"}, false, 0, []string{}},
	} {
		rows, total, err := s.ListAdminThreadMetadata(ctx, proof, tc.filter, tc.trash)
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if err != nil || total != tc.total || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatal(tc, rows, total, err)
		}
		raw, _ := json.Marshal(rows)
		for _, secret := range []string{"PRIVATE_TITLE", "PRIVATE_BODY", "before-body", "facts_version", "reason", "credential", "content\"", "title\"", "tags\""} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("private metadata", string(raw))
			}
		}
	}
	for _, f := range []domain.ThreadListFilter{{Keyword: "PRIVATE_BODY"}, {Page: -1}, {PageSize: 101}, {PublicationStatus: "unknown"}, {Tag: "private"}, {DeletionStatus: "purged"}, {Status: "published"}, {ContentFormat: "raw"}} {
		if _, _, err := s.ListAdminThreadMetadata(ctx, proof, f, false); err == nil {
			t.Fatal("invalid filter", f)
		}
	}
	if !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(gBefore, gov.Snapshot()) {
		t.Fatal("read wrote resource")
	}
	audits, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil || len(audits) != 0 {
		t.Fatal("read audit write", audits, err)
	}
}

type metadataBoardDeny struct {
	identityport.AdminExecutionAuthorityReader
	board string
}

func (r metadataBoardDeny) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, p identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	a, e := r.AdminExecutionAuthorityReader.LoadAdminExecutionAuthority(ctx, q, p, lock)
	if q.Scope.Kind == "community.board" && q.Scope.ID == r.board {
		a.Allowed = false
	}
	return a, e
}
func TestAdminThreadMetadataHistorySafeTypedActorsPaginationAndReadOnly(t *testing.T) {
	ctx := context.Background()
	s, repo, gov, _, admins, _, proof := adminFlagFixture(t)
	if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "metadata"); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"admin", "user"} {
		actor := "opaque.admin"
		if kind == "user" {
			actor = "190102"
		}
		if err := gov.CreateModerationAction(ctx, &domain.ModerationAction{ID: fmt.Sprint(2580201 + i), ThreadID: "190101", Action: "pin", ActorKind: kind, ActorRefID: actor, Reason: "PRIVATE_REASON", BeforeState: "PRIVATE_BEFORE", AfterState: "PRIVATE_AFTER", CreatedAt: time.Unix(int64(i+100), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	before, gBefore := repo.Snapshot(), gov.Snapshot()
	rows, total, err := s.AdminModerationMetadata(ctx, "190101", proof, 1, 1)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].ActorKind != "user" || rows[0].ActorRefID != "190102" {
		t.Fatal(rows, total, err)
	}
	raw, _ := json.Marshal(rows)
	for _, secret := range []string{"PRIVATE_", "reason", "before_state", "after_state", "title", "content", "actor_id\"", "facts_version"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal(string(raw))
		}
	}
	empty, total, err := s.AdminModerationMetadata(ctx, "190101", proof, 10, 1)
	if err != nil || total != 2 || len(empty) != 0 {
		t.Fatal(empty, total, err)
	}
	if !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(gBefore, gov.Snapshot()) {
		t.Fatal("history wrote resource")
	}
	if _, _, err := s.AdminModerationMetadata(ctx, "missing", proof, 1, 20); err == nil {
		t.Fatal("missing parent")
	}
	if _, _, err := s.ListAdminThreadMetadata(ctx, authorUpdateProof("190102"), domain.ThreadListFilter{}, false); err == nil {
		t.Fatal("same ID user admitted")
	}
}

type metadataInventoryHook struct {
	*repository.MemoryThreadRepository
	calls int
	at    int
	after func()
}

func (r *metadataInventoryHook) ListAdminThreadMetadata(ctx context.Context) ([]repository.AdminThreadMetadata, error) {
	rows, e := r.MemoryThreadRepository.ListAdminThreadMetadata(ctx)
	r.calls++
	if r.calls == r.at && r.after != nil {
		r.after()
	}
	return rows, e
}
func TestAdminThreadMetadataWholeAndEmptyFinalAuthorityAndFactsChangesClose(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"list", "empty", "history"} {
		for _, mode := range []string{"permission_aba", "session", "row_aba", "new_row", "final_row"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, repo, _, _, admins, sessions, proof := adminFlagFixture(t)
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "metadata"); err != nil {
					t.Fatal(err)
				}
				hook := &metadataInventoryHook{MemoryThreadRepository: repo, at: 1}
				if mode == "final_row" {
					hook.at = 5
				}
				hook.after = func() {
					switch mode {
					case "permission_aba":
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", nil, "revoke"); err != nil {
							t.Fatal(err)
						}
						if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "restore"); err != nil {
							t.Fatal(err)
						}
					case "session":
						if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
							t.Fatal(err)
						}
					case "row_aba", "final_row":
						row, _ := repo.GetByID(ctx, "190101")
						old := row.Content
						row.Content = "changed"
						if err := repo.Update(ctx, row); err != nil {
							t.Fatal(err)
						}
						row.Content = old
						if err := repo.Update(ctx, row); err != nil {
							t.Fatal(err)
						}
					case "new_row":
						row := &domain.Thread{ID: "2580999", AuthorID: "190102", CategoryID: "190103", PublicationStatus: "private", ModerationStatus: "clear", DeletionStatus: "active"}
						if err := repo.Create(ctx, row); err != nil {
							t.Fatal(err)
						}
					}
				}
				s.repo = hook
				var err error
				if op == "history" {
					_, _, err = s.AdminModerationMetadata(ctx, "190101", proof, 1, 20)
				} else {
					f := domain.ThreadListFilter{}
					if op == "empty" {
						f.Page = 100
					}
					_, _, err = s.ListAdminThreadMetadata(ctx, proof, f, false)
				}
				if err == nil {
					t.Fatal("stale whole projection accepted", hook.calls)
				}
			})
		}
	}
}
