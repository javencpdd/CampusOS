package server

import (
	"context"
	"testing"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/config"
)

type v5DirectoryAssemblyAdmins struct{}

func (v5DirectoryAssemblyAdmins) LoadAdminExecutionAuthority(context.Context, identity.AdminExecutionQuery, identity.SessionPrincipalResolver, bool) (identity.AdminExecutionAuthority, error) {
	return identity.AdminExecutionAuthority{}, plugin.ErrV5Denied
}

func TestV5ContributionDirectoryAssembly(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "reconciled", false: "startup-failure-closed"}[healthy], func(t *testing.T) {
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
			var state plugin.V5StateStore
			if healthy {
				state = plugin.NewMemoryV5StateStore()
			}
			m := newPluginPlatformModule(owner, nil, features, nil, nil, nil, nil, &v5IdentityAssemblyStore{}, state)
			if err := m.Register(app); err != nil {
				t.Fatal(err)
			}
			if err := m.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Stop(context.Background()) })
			value, ok := app.Lookup("plugin.v5.contributions")
			directory, typed := value.(*plugin.V5ContributionDirectory)
			if !ok || !typed {
				t.Fatal("typed directory absent")
			}
			_, err := directory.Resolve(context.Background(), "", plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, nil)
			want := plugin.ErrV5PlatformUnavailable
			if healthy {
				want = plugin.ErrV5ContributionUnpublished
			}
			if err != want {
				t.Fatal("startup fabricated a provider or ignored failure", err)
			}
		})
	}
}
