package plugin

import (
	"testing"
	"time"
)

func TestRunningPluginHostTokenRotatesAndRevokes(t *testing.T) {
	manager := NewManager()
	runtime := newFakeRuntime()
	manager.RegisterRuntime("wasm", runtime)
	dir := writePackablePlugin(t, t.TempDir(), "identity-plugin", "0.1.0")
	installed, err := manager.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RequestEnable(installed.ID); err != nil {
		t.Fatal(err)
	}
	first := installed.HostToken
	if first == "" || installed.HostTokenExpiresAt.Before(time.Now()) {
		t.Fatalf("expected current host token, plugin=%#v", installed)
	}
	if _, ok := manager.AuthorizeHostAPI(installed.ID, first); !ok {
		t.Fatal("expected current token authorization")
	}
	if _, ok := manager.AuthorizeHostAPI(installed.ID, "wrong"); ok {
		t.Fatal("wrong token must be rejected")
	}
	if err := manager.ReloadUserPlugin(installed.ID); err != nil {
		t.Fatal(err)
	}
	if installed.HostToken == first {
		t.Fatal("reload must rotate the token")
	}
	if _, ok := manager.AuthorizeHostAPI(installed.ID, first); ok {
		t.Fatal("rotated token must be revoked")
	}
	if err := manager.RequestDisable(installed.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.AuthorizeHostAPI(installed.ID, installed.HostToken); ok {
		t.Fatal("stopped plugin must not call Host API")
	}
}
