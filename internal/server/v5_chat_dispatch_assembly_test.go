package server

import (
	"context"
	"testing"

	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/config"
)

func TestV5ChatDispatcherAssemblyHasNoFakeRunner(t *testing.T) {
	t.Setenv("CAMPUSOS_PLUGIN_V5_TRUST_STORE", "")
	cfg := &config.Config{}
	cfg.Plugin.DataDir = t.TempDir()
	owner := &Server{cfg: cfg}
	app := platformmodule.NewAppContext()
	features := newFeatureRegistryModule(owner, platformfeature.NewMemoryStore())
	if err := features.Register(app); err != nil {
		t.Fatal(err)
	}
	events := reliability.NewMemoryStore()
	if err := app.Provide("platform.reliability.service", reliability.NewService(transaction.NewMemory(events), events)); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide("identity.admin-execution-authority", v5DirectoryAssemblyAdmins{}); err != nil {
		t.Fatal(err)
	}
	m := newPluginPlatformModule(owner, nil, features, nil, nil, nil, nil, &v5IdentityAssemblyStore{}, plugin.NewMemoryV5StateStore())
	if err := m.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	value, ok := app.Lookup("plugin.v5.chat-dispatcher")
	dispatcher, typed := value.(*plugin.V5ChatDispatcher)
	if !ok || !typed {
		t.Fatal("host dispatcher missing")
	}
	raw := []byte(`{"request_id":"r1","contribution_id":"chat","instance_generation":1,"deadline_ms":500,"prompt":"caller input","context_class":"none"}`)
	out, err := dispatcher.Chat(context.Background(), plugin.V5ContributionBinding{}, raw, nil)
	if err != plugin.ErrV5RuntimeMissing || out.Text != "" {
		t.Fatal("production assembly fabricated a runner", err)
	}
}
