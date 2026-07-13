package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

type moduleFixture struct {
	id     string
	events *[]string
	start  error
}

func (m moduleFixture) ID() string             { return m.id }
func (m moduleFixture) Dependencies() []string { return nil }
func (m moduleFixture) Register(*platformmodule.AppContext) error {
	*m.events = append(*m.events, "register:"+m.id)
	return nil
}
func (m moduleFixture) Start(context.Context) error {
	*m.events = append(*m.events, "start:"+m.id)
	return m.start
}
func (m moduleFixture) Stop(context.Context) error {
	*m.events = append(*m.events, "stop:"+m.id)
	return nil
}

func TestRuntimeBindsProfileBeforeStartingAndClosesAfterStopping(t *testing.T) {
	var events []string
	profile := NewStaticProfile(ProfilePostgreSQL, func(app *platformmodule.AppContext) error {
		events = append(events, "bind:postgresql")
		return app.Provide("profile.name", "postgresql")
	}, func(context.Context) error {
		events = append(events, "close:postgresql")
		return nil
	})
	runtime, err := New(Config{Profile: profile, Modules: []Registration{{Module: moduleFixture{id: "core", events: &events}, Kind: platformmodule.KindCore, Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.AppContext().Lookup("profile.name"); !ok {
		t.Fatal("profile port was not bound")
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"bind:postgresql", "register:core", "start:core", "stop:core", "close:postgresql"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRuntimeClosesProfileAfterModuleStartFailure(t *testing.T) {
	var closed bool
	profile := NewStaticProfile(ProfileMemory, nil, func(context.Context) error {
		closed = true
		return nil
	})
	events := []string{}
	runtime, err := New(Config{Profile: profile, Modules: []Registration{{Module: moduleFixture{id: "broken", events: &events, start: errors.New("boom")}, Kind: platformmodule.KindCore, Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err == nil {
		t.Fatal("expected startup failure")
	}
	if !closed {
		t.Fatal("profile was not closed after startup failure")
	}
}

func TestRuntimeRequiresProfile(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected missing profile error")
	}
}
