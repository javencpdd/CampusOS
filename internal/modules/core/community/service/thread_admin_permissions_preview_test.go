package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
)

var previewTestCodes = []string{"community.thread.read", "community.thread.pin", "community.thread.lock", "community.thread.review", "community.thread.trash", "community.thread.restore", "community.thread.direct_restore", "community.thread.purge", "community.thread.take_down"}

func TestAdminThreadPreviewCurrentSamePoliciesBoundedBatchSafeAndNoSideEffects(t *testing.T) {
	ctx := context.Background()
	for _, del := range []string{"active", "trashed", "purged"} {
		for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
			t.Run(del+"/"+mod, func(t *testing.T) {
				s, repo, gov, store, admins, _, proof := adminFlagFixture(t)
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", previewTestCodes, "preview"); err != nil {
					t.Fatal(err)
				}
				s.SetGovernanceAuthority(s.adminExecution.(identityport.BoardGovernanceAuthorityReader))
				row, _ := repo.GetByID(ctx, "190101")
				row.DeletionStatus = domain.DeletionStatus(del)
				row.ModerationStatus = domain.ModerationStatus(mod)
				if err := repo.Update(ctx, row); err != nil {
					t.Fatal(err)
				}
				before, history := repo.Snapshot(), gov.Snapshot()
				rows, err := s.PreviewAdminThreadPermissions(ctx, []string{row.ID}, proof)
				if err != nil || len(rows) != 1 {
					t.Fatal(rows, err)
				}
				want := AdminThreadPermissionFlags{Pin: del == "active", Unpin: del == "active", Lock: del == "active", Unlock: del == "active", Approve: del == "active" && mod == "pending", Reject: del == "active" && mod == "pending", TakeDown: del == "active" && mod != "taken_down", DirectRestore: del == "active" && mod == "taken_down", Trash: del == "active", RestoreTrash: del == "trashed", Purge: del == "trashed"}
				if rows[0].Permissions != want {
					t.Fatal(rows[0], want)
				}
				raw, _ := json.Marshal(rows)
				for _, secret := range []string{row.Content, "facts_version", "credential", "grants", "expires_at", "author_id", "title", "reason"} {
					if strings.Contains(string(raw), secret) {
						t.Fatal(string(raw))
					}
				}
				if !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(history, gov.Snapshot()) {
					t.Fatal("preview wrote resource")
				}
				audits, _, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
				if err != nil || len(audits) != 0 {
					t.Fatal(audits, err)
				}
				for _, ids := range [][]string{nil, {}, {row.ID, row.ID}, {"wrong space"}, make([]string, 21)} {
					if rows, err := s.PreviewAdminThreadPermissions(ctx, ids, proof); err == nil || rows != nil {
						t.Fatal("invalid batch", ids, rows, err)
					}
				}
				if rows, err := s.PreviewAdminThreadPermissions(ctx, []string{row.ID, "missing"}, proof); err == nil || rows != nil {
					t.Fatal("partial batch", rows, err)
				}
				if rows, err := s.PreviewAdminThreadPermissions(ctx, []string{row.ID}, authorUpdateProof("190102")); err == nil || rows != nil {
					t.Fatal("same ID user", rows, err)
				}
				if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "remove execution"); err != nil {
					t.Fatal(err)
				}
				rows, err = s.PreviewAdminThreadPermissions(ctx, []string{row.ID}, proof)
				if err != nil || len(rows) != 1 || rows[0].Permissions != (AdminThreadPermissionFlags{}) {
					t.Fatal("read permission became write authority", rows, err)
				}
			})
		}
	}
}

type previewExecutionHook struct {
	identityport.AdminExecutionAuthorityReader
	changed bool
	after   func()
}

func (r *previewExecutionHook) LoadAdminExecutionAuthority(ctx context.Context, q identityport.AdminExecutionQuery, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	a, e := r.AdminExecutionAuthorityReader.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
	if q.PermissionCode == "community.thread.lock" && !r.changed {
		r.changed = true
		r.after()
	}
	return a, e
}
func TestAdminThreadPreviewWholeBatchCurrentChangesAndDetachedInput(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"permission_aba", "session", "earlier_row_aba", "caller_input"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, _, _, admins, sessions, proof := adminFlagFixture(t)
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", previewTestCodes, "preview"); err != nil {
				t.Fatal(err)
			}
			s.SetGovernanceAuthority(s.adminExecution.(identityport.BoardGovernanceAuthorityReader))
			ids := []string{"190101"}
			source := s.adminExecution
			s.adminExecution = &previewExecutionHook{AdminExecutionAuthorityReader: source, after: func() {
				switch mode {
				case "permission_aba":
					if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read"}, "revoke"); err != nil {
						t.Fatal(err)
					}
					if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", previewTestCodes, "restore"); err != nil {
						t.Fatal(err)
					}
				case "session":
					if _, _, err := sessions.RevokeSessionCAS(ctx, "adms.flags", 1, "test", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "earlier_row_aba":
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
				case "caller_input":
					ids[0] = "missing"
				}
			}}
			rows, err := s.PreviewAdminThreadPermissions(ctx, ids, proof)
			if mode == "caller_input" {
				if err != nil || len(rows) != 1 || rows[0].ID != "190101" {
					t.Fatal("input aliased", rows, err)
				}
			} else if err == nil || rows != nil {
				t.Fatal("stale batch", mode, rows, err)
			}
		})
	}
}
