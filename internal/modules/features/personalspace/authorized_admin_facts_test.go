package space

import (
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
	"testing"
	"time"
)

type adminSpaceMutationHook struct {
	*MemoryRepository
	after func(context.Context)
}

func (r *adminSpaceMutationHook) UpdateAdminSpaceState(ctx context.Context, owner string, version int64, at *time.Time, actor, reason string, updated time.Time) (int64, error) {
	receipt, err := r.MemoryRepository.UpdateAdminSpaceState(ctx, owner, version, at, actor, reason, updated)
	if err != nil {
		return 0, err
	}
	if r.after != nil {
		r.after(ctx)
	}
	return receipt, nil
}

func TestSpaceAdminCommandsRejectActualExtraWritesAndFinalAuthorityChanges(t *testing.T) {
	for _, op := range []string{"disable", "enable"} {
		for _, mode := range []string{"space_title", "space_aba", "user_email", "user_status", "permission_aba", "admin_session"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				f := spaceAdminFixture(t, true, "adm.facts.space", "")
				ctx := context.Background()
				if op == "enable" {
					if _, err := f.s.DisableSpaceForPrincipal(ctx, "1001", "review", f.proof); err != nil {
						t.Fatal(err)
					}
				}
				before, events, users, admins, sessions := f.repo.Snapshot(), f.events.Snapshot(), f.users.Snapshot(), f.admins.Snapshot(), f.sessions.Snapshot()
				ran := false
				f.s.repo = &adminSpaceMutationHook{MemoryRepository: f.repo, after: func(tx context.Context) {
					ran = true
					switch mode {
					case "space_title", "space_aba":
						row, _ := f.repo.GetByUserID(tx, "1001")
						title := row.Title
						row.Title = "extra"
						if err := f.repo.Upsert(tx, row); err != nil {
							t.Fatal(err)
						}
						if mode == "space_aba" {
							row.Title = title
							if err := f.repo.Upsert(tx, row); err != nil {
								t.Fatal(err)
							}
						}
					case "user_email", "user_status":
						user, _ := f.users.GetByID(tx, "1001")
						if mode == "user_email" {
							user.Email = "changed@example.test"
						} else {
							user.Status = "deactivated"
						}
						if err := f.users.Update(tx, user); err != nil {
							t.Fatal(err)
						}
					case "permission_aba":
						if err := f.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", nil, "revoke"); err != nil {
							t.Fatal(err)
						}
						if err := f.admins.ReplaceAdminRolePermissionFacts(tx, "security_admin", []string{"personal_space.space.manage"}, "restore"); err != nil {
							t.Fatal(err)
						}
					case "admin_session":
						row, err := f.sessions.GetSessionByID(tx, f.claims.SessionID)
						if err != nil {
							t.Fatal(err)
						}
						if _, ok, err := f.sessions.RevokeSessionCAS(tx, row.ID, row.Version, "changed", time.Now()); err != nil || !ok {
							t.Fatal(ok, err)
						}
					}
				}}
				var ack *SpaceAdminMutationAck
				var err error
				if op == "disable" {
					ack, err = f.s.DisableSpaceForPrincipal(ctx, "1001", "review", f.proof)
				} else {
					ack, err = f.s.EnableSpaceForPrincipal(ctx, "1001", f.proof)
				}
				if !ran || err == nil || ack != nil || !reflect.DeepEqual(before, f.repo.Snapshot()) || !reflect.DeepEqual(events, f.events.Snapshot()) || !reflect.DeepEqual(users, f.users.Snapshot()) || !reflect.DeepEqual(admins, f.admins.Snapshot()) || !reflect.DeepEqual(sessions, f.sessions.Snapshot()) {
					t.Fatal("extra command mutation retained", ran, ack, err)
				}
			})
		}
	}
	var nilProof identityport.SessionPrincipalResolver
	f := spaceAdminFixture(t, false, "adm.closed.space", "")
	if _, err := f.s.EnableSpaceForPrincipal(context.Background(), "1001", nilProof); err == nil {
		t.Fatal("missing proof accepted")
	}
}
