package appearance

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/resource"
	"testing"
)

type catalogStub struct{ item resource.Item }

func (c catalogStub) List(resource.Type) ([]resource.Item, error) {
	return []resource.Item{c.item}, nil
}
func (c catalogStub) Get(resource.Type, string) (resource.Item, error) { return c.item, nil }

type prefStub struct{ value string }

func (p *prefStub) Get(context.Context, string, resource.Type) (string, error) { return p.value, nil }
func (p *prefStub) Set(_ context.Context, _ string, _ resource.Type, v string) error {
	p.value = v
	return nil
}

type policyStub struct{ deny bool }

func (p policyStub) CanApply(context.Context, string, resource.Item) error {
	if p.deny {
		return errors.New("denied")
	}
	return nil
}
func TestFacadeAppliesThroughPolicyAndPreferencePort(t *testing.T) {
	prefs := &prefStub{}
	f := Facade{Catalog: catalogStub{item: resource.Item{Manifest: resource.Manifest{ID: "clean", Type: resource.Theme}}}, Preferences: prefs, Policy: policyStub{}}
	if err := f.Apply(context.Background(), "u", resource.Theme, "clean"); err != nil {
		t.Fatal(err)
	}
	if prefs.value != "clean" {
		t.Fatalf("preference=%s", prefs.value)
	}
}
