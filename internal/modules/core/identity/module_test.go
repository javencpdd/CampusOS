package identity

import (
	"context"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

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
	if module.Handlers().User == nil || module.Handlers().Role == nil || module.Handlers().ChallengePolicy == nil || module.Permissions() == nil || module.AdminAccess() == nil || module.ChallengePolicies() == nil {
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
}
