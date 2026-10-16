package service

import (
	"context"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
	"time"
)

func TestAdminSPAPreviewPartialReadAndPinRoleDoesNotRequireUnrelatedPermissions(t *testing.T) {
	ctx := context.Background()
	for _, strength := range []string{"password", "mfa"} {
		t.Run(strength, func(t *testing.T) {
			s, _, _, _, admins, sessions, proof := adminFlagFixture(t)
			if err := admins.ReplaceAdminRolePermissionFacts(ctx, "security_admin", []string{"community.thread.read", "community.thread.pin"}, "spa-role"); err != nil {
				t.Fatal(err)
			}
			if strength == "password" {
				if _, _, err := sessions.UpgradeSessionStrengthCAS(ctx, "adms.flags", 1, "mfa", time.Now().Add(-16*time.Minute), time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			s.SetGovernanceAuthority(s.adminExecution.(identityport.BoardGovernanceAuthorityReader))
			rows, err := s.PreviewAdminThreadPermissions(ctx, []string{"190101"}, proof)
			if err != nil || len(rows) != 1 {
				t.Fatal("partial role preview failed", err)
			}
			want := AdminThreadPermissionFlags{Pin: strength == "mfa", Unpin: strength == "mfa"}
			if rows[0].Permissions != want {
				t.Fatal("unrelated permission granted", rows[0].Permissions)
			}
		})
	}
}
