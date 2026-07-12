package feature

import "testing"

func TestRegistryActivationModes(t *testing.T) {
	r := NewRegistry(func(id string) bool { return id == "legacy-space" })
	if err := r.Register(Definition{ID: "identity", Mode: AlwaysOn}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Definition{ID: "space", Mode: Restart, LegacyPlugin: "legacy-space"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Definition{ID: "appearance", Mode: HotGated}); err != nil {
		t.Fatal(err)
	}
	if !r.Enabled("identity") || !r.Enabled("space") {
		t.Fatal("expected enabled core and legacy feature")
	}
	state, err := r.Request("space", false)
	if err != nil || !state.PendingRestart {
		t.Fatalf("expected pending restart: %#v %v", state, err)
	}
	state, err = r.Request("appearance", true)
	if err != nil || !state.Enabled || state.PendingRestart {
		t.Fatalf("expected hot gate: %#v %v", state, err)
	}
	if _, err := r.Request("identity", false); err == nil {
		t.Fatal("expected always-on rejection")
	}
}

func TestRestartFeatureUsesBootstrapSnapshot(t *testing.T) {
	running := true
	r := NewRegistry(func(string) bool { return running })
	if err := r.Register(Definition{ID: "schedule", Mode: Restart, LegacyPlugin: "schedule"}); err != nil {
		t.Fatal(err)
	}
	r.SyncLegacy()
	running = false
	if !r.Enabled("schedule") {
		t.Fatal("restart feature changed without restart")
	}
}
