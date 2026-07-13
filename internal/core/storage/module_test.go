package storage

import (
	"context"
	"testing"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

func TestModulePublishesStableStoragePorts(t *testing.T) {
	app := platformmodule.NewAppContext()
	module := NewModule(ModuleConfig{Root: t.TempDir(), QuotaBytes: 32})
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"storage.user", "storage.quota", "storage.safe-path", "storage.provider"} {
		if _, ok := app.Lookup(name); !ok {
			t.Fatalf("missing storage port %s", name)
		}
	}
	if _, err := module.Adapter().EnsureLayout("user-1"); err != nil {
		t.Fatal(err)
	}
}
