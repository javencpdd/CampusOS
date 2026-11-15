package identity

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestModuleAdminBootstrapProofLengthBoundaries(t *testing.T) {
	for _, size := range []int{0, 7, 8, 15, 16, 4096, 4097} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			app := platformmodule.NewAppContext()
			if err := BindMemoryAdapters(app); err != nil {
				t.Fatal(err)
			}
			if err := app.Provide(portEventBus, eventbus.NewMemoryEventBus()); err != nil {
				t.Fatal(err)
			}
			module := NewModule(Config{
				JWT:                 auth.NewJWTManager(auth.JWTConfig{Secret: "test-user-secret", AccessTTL: time.Hour, RefreshTTL: 2 * time.Hour}),
				PasswordHashEnabled: true, BootstrapAdminSecret: "legacy-user-bootstrap-secret",
				AdminBootstrapSecret: strings.Repeat("x", size),
				ChallengeActiveKeyID: "test-v1", ChallengeHMACKeys: map[string]string{"test-v1": "test-challenge-signing-secret"},
				ChallengeIPHashSecret: "test-challenge-ip-hash-secret",
			})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			if err := module.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = module.Stop(context.Background()) })
			_, err := module.adminDomainService.BootstrapSecurityAdmin(context.Background(), "operator", "operator@example.test", "synthetic-password", "test")
			wantEnabled := size >= 8 && size <= 4096
			if (err == nil) != wantEnabled {
				t.Fatalf("bootstrap enabled=%v, error=%v", wantEnabled, err)
			}
			count, err := module.adminDomains.CountActiveSecurityAdmins(context.Background())
			wantCount := 0
			if wantEnabled {
				wantCount = 1
			}
			if err != nil || count != wantCount {
				t.Fatalf("administrator count=%d, want %d, error=%v", count, wantCount, err)
			}
		})
	}
}

func TestMemoryProfileAndModuleExposePublicPorts(t *testing.T) {
	app := platformmodule.NewAppContext()
	if err := BindMemoryAdapters(app); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.NewMemoryEventBus()
	if err := app.Provide(portEventBus, bus); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "test-secret", AccessTTL: time.Hour, RefreshTTL: 2 * time.Hour})
	module := NewModule(Config{
		JWT:                   jwt,
		PasswordHashEnabled:   true,
		ChallengeActiveKeyID:  "test-v1",
		ChallengeHMACKeys:     map[string]string{"test-v1": "test-challenge-signing-secret"},
		ChallengeIPHashSecret: "test-challenge-ip-hash-secret",
	})
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if value, ok := app.Lookup(portBoardGovernanceAuthority); !ok {
		t.Fatal("missing checked board governance authority")
	} else if _, ok := value.(identityport.BoardGovernanceAuthorityReader); !ok {
		t.Fatalf("incompatible governance authority %T", value)
	}
	if value, ok := app.Lookup(portAdminSessionAuthority); !ok {
		t.Fatal("missing current Admin session authority")
	} else if _, ok := value.(identityport.AdminSessionAuthorityReader); !ok {
		t.Fatalf("incompatible current Admin authority %T", value)
	}
	if value, ok := app.Lookup(portAdminExecutionAuthority); !ok {
		t.Fatal("missing named Admin execution authority")
	} else if _, ok := value.(identityport.AdminExecutionAuthorityReader); !ok {
		t.Fatalf("incompatible named Admin execution authority %T", value)
	}
	if value, ok := app.Lookup(portUserScopedExecutionAuthority); !ok {
		t.Fatal("missing named User scoped execution authority")
	} else if _, ok := value.(identityport.UserScopedExecutionAuthorityReader); !ok {
		t.Fatalf("incompatible named User scoped authority %T", value)
	}
	if module.Handlers().User == nil || module.Handlers().Role == nil || module.Handlers().ChallengePolicy == nil || module.Handlers().AdminAdmission == nil || module.Permissions() == nil || module.AdminAccess() == nil || module.AdminAdmissions() == nil || module.ChallengePolicies() == nil {
		t.Fatal("identity module did not construct its HTTP/application components")
	}
	value, ok := app.Lookup(portUserReader)
	if !ok {
		t.Fatal("identity user reader port is missing")
	}
	if _, ok := value.(identityport.UserReader); !ok {
		t.Fatalf("unexpected user reader port type %T", value)
	}
	value, ok = app.Lookup(portAccountReader)
	if !ok {
		t.Fatal("identity account reader port is missing")
	}
	if _, ok := value.(identityport.AccountReader); !ok {
		t.Fatalf("unexpected account reader port type %T", value)
	}
	value, ok = app.Lookup(portChallengeDispatchReader)
	if !ok {
		t.Fatal("identity challenge dispatch reader port is missing")
	}
	if _, ok := value.(identityport.ChallengeDispatchReader); !ok {
		t.Fatalf("unexpected challenge dispatch reader port type %T", value)
	}
	value, ok = app.Lookup(portAuthorization)
	if !ok {
		t.Fatal("identity authorization port is missing")
	}
	if _, ok := value.(identityport.Authorization); !ok {
		t.Fatalf("unexpected authorization port type %T", value)
	}
	value, ok = app.Lookup(portAdminAccess)
	if !ok || value != module.AdminAccess() {
		t.Fatalf("identity administrator access port is missing or inconsistent: %T", value)
	}
	value, ok = app.Lookup(portAdminAdmission)
	if !ok || value != module.AdminAdmissions() {
		t.Fatalf("identity administrator admission port is missing or inconsistent: %T", value)
	}
	value, ok = app.Lookup(portMFA)
	if !ok || value == nil {
		t.Fatalf("identity MFA port is missing: %T", value)
	}
}
