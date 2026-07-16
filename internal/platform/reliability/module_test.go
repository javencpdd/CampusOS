package reliability

import (
	"context"
	"testing"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestModulePublishesStablePortsDuringRegistration(t *testing.T) {
	app := platformmodule.NewAppContext()
	if err := BindMemoryAdapter(app); err != nil {
		t.Fatal(err)
	}
	module := NewModule()
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if module.Service() == nil || module.Handler() == nil {
		t.Fatal("reliability ports were not constructed during registration")
	}
	if value, ok := app.Lookup(portService); !ok || value != module.Service() {
		t.Fatalf("service port = %T, want registered reliability service", value)
	}
	if value, ok := app.Lookup(portHandler); !ok || value != module.Handler() {
		t.Fatalf("handler port = %T, want registered reliability handler", value)
	}

	bus := eventbus.NewMemoryEventBus()
	if err := app.Provide("platform.event-bus", bus); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := module.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestModuleRegistrationRequiresProfileAdapters(t *testing.T) {
	module := NewModule()
	if err := module.Register(platformmodule.NewAppContext()); err == nil {
		t.Fatal("expected missing profile adapter error")
	}
}
