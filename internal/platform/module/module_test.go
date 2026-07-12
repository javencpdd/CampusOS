package module

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type testModule struct {
	id           string
	dependencies []string
	events       *[]string
	startErr     error
	stopErr      error
}

func (m *testModule) ID() string                    { return m.id }
func (m *testModule) Dependencies() []string        { return m.dependencies }
func (m *testModule) Health(context.Context) Health { return Health{Status: HealthHealthy} }
func (m *testModule) Register(*AppContext) error {
	*m.events = append(*m.events, "register:"+m.id)
	return nil
}
func (m *testModule) Start(context.Context) error {
	*m.events = append(*m.events, "start:"+m.id)
	return m.startErr
}
func (m *testModule) Stop(context.Context) error {
	*m.events = append(*m.events, "stop:"+m.id)
	return m.stopErr
}

func TestRegistryStartsInDependencyOrderAndStopsInReverse(t *testing.T) {
	var events []string
	registry := NewRegistry(nil)
	for _, mod := range []*testModule{
		{id: "feature", dependencies: []string{"community"}, events: &events},
		{id: "identity", events: &events},
		{id: "community", dependencies: []string{"identity"}, events: &events},
	} {
		if err := registry.Add(mod, KindCore, true); err != nil {
			t.Fatalf("add %s: %v", mod.id, err)
		}
	}
	if err := registry.StartAll(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := registry.StopAll(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	want := []string{
		"register:identity", "register:community", "register:feature",
		"start:identity", "start:community", "start:feature",
		"stop:feature", "stop:community", "stop:identity",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRegistryRejectsInvalidDependencies(t *testing.T) {
	tests := []struct {
		name    string
		modules []*testModule
	}{
		{name: "missing", modules: []*testModule{{id: "a", dependencies: []string{"missing"}}}},
		{name: "cycle", modules: []*testModule{{id: "a", dependencies: []string{"b"}}, {id: "b", dependencies: []string{"a"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var events []string
			registry := NewRegistry(nil)
			for _, mod := range test.modules {
				mod.events = &events
				if err := registry.Add(mod, KindCore, true); err != nil {
					t.Fatalf("add: %v", err)
				}
			}
			if err := registry.RegisterAll(); err == nil {
				t.Fatal("expected dependency validation error")
			}
		})
	}
}

func TestRegistryRollsBackStartedModules(t *testing.T) {
	var events []string
	registry := NewRegistry(nil)
	first := &testModule{id: "first", events: &events}
	second := &testModule{id: "second", dependencies: []string{"first"}, events: &events, startErr: errors.New("boom")}
	if err := registry.Add(first, KindCore, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(second, KindCore, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartAll(context.Background()); err == nil {
		t.Fatal("expected start failure")
	}
	if events[len(events)-1] != "stop:first" {
		t.Fatalf("expected rollback stop, got %#v", events)
	}
}

func TestRegistryKeepsDisabledFeatureOutOfLifecycle(t *testing.T) {
	var events []string
	registry := NewRegistry(nil)
	feature := &testModule{id: "optional", events: &events}
	if err := registry.Add(feature, KindBuiltinFeature, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("disabled feature entered lifecycle: %#v", events)
	}
	snapshots := registry.Snapshots(context.Background())
	if len(snapshots) != 1 || snapshots[0].State != StateDisabled {
		t.Fatalf("unexpected snapshots: %#v", snapshots)
	}
}

func TestRegistryReportsRunningModuleHealth(t *testing.T) {
	var events []string
	registry := NewRegistry(nil)
	if err := registry.Add(&testModule{id: "identity", events: &events}, KindCore, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshots := registry.Snapshots(context.Background())
	if len(snapshots) != 1 || snapshots[0].State != StateRunning || snapshots[0].Health.Status != HealthHealthy {
		t.Fatalf("unexpected snapshots: %#v", snapshots)
	}
}

func TestRegistryRejectsDisabledCoreAndDisabledDependency(t *testing.T) {
	var events []string
	registry := NewRegistry(nil)
	if err := registry.Add(&testModule{id: "core", events: &events}, KindCore, false); err == nil {
		t.Fatal("expected disabled core rejection")
	}
	if err := registry.Add(&testModule{id: "base", events: &events}, KindBuiltinFeature, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(&testModule{id: "consumer", dependencies: []string{"base"}, events: &events}, KindBuiltinFeature, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterAll(); err == nil {
		t.Fatal("expected enabled module dependency rejection")
	}
}

func TestAppContextRejectsDuplicatePorts(t *testing.T) {
	app := NewAppContext()
	if err := app.Provide("identity.users", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide("identity.users", struct{}{}); err == nil {
		t.Fatal("expected duplicate port rejection")
	}
	if _, ok := app.Lookup("identity.users"); !ok {
		t.Fatal("expected registered port")
	}
}
