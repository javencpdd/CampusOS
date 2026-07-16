package moderation

import (
	"context"
	"testing"

	communitycore "github.com/campusos/CampusOS/internal/modules/core/community"
	identitycore "github.com/campusos/CampusOS/internal/modules/core/identity"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestModuleStartsWithIdentityAndCommunityPorts(t *testing.T) {
	app := platformmodule.NewAppContext()
	if err := identitycore.BindMemoryAdapters(app); err != nil {
		t.Fatal(err)
	}
	if err := communitycore.BindMemoryAdapters(app); err != nil {
		t.Fatal(err)
	}
	if err := BindMemoryAdapters(app); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.NewMemoryEventBus()
	for _, binding := range []struct {
		name  string
		value interface{}
	}{
		{"platform.event-bus", bus},
		{"platform.memory-event-bus", bus},
		{"platform.cache", cache.NewMemoryCache()},
	} {
		if err := app.Provide(binding.name, binding.value); err != nil {
			t.Fatal(err)
		}
	}
	identity := identitycore.NewModule(identitycore.Config{JWT: auth.NewJWTManager(auth.JWTConfig{Secret: "test"})})
	if err := identity.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := identity.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	community := communitycore.NewModule()
	if err := community.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := community.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	module := NewModule(ModuleConfig{})
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if module.Handler() == nil || module.Service() == nil {
		t.Fatal("moderation module did not construct its handler and service")
	}
}
