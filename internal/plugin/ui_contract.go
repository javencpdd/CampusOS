package plugin

import (
	"fmt"
	"regexp"
	"strings"
)

var contributionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

type UIContribution struct {
	ContractVersion string         `yaml:"contract_version,omitempty" json:"contract_version,omitempty"`
	Routes          []UIRoute      `yaml:"routes,omitempty" json:"routes,omitempty"`
	Navigation      []UINavigation `yaml:"navigation,omitempty" json:"navigation,omitempty"`
	Slots           []UISlot       `yaml:"slots,omitempty" json:"slots,omitempty"`
	Surfaces        []UISurface    `yaml:"surfaces,omitempty" json:"surfaces,omitempty"`
	Actions         []UIAction     `yaml:"actions,omitempty" json:"actions,omitempty"`
}

type UIRoute struct {
	ID           string `yaml:"id" json:"id"`
	Path         string `yaml:"path" json:"path"`
	SurfaceID    string `yaml:"surface_id" json:"surface_id"`
	Title        string `yaml:"title,omitempty" json:"title,omitempty"`
	RequiresAuth bool   `yaml:"requires_auth,omitempty" json:"requires_auth,omitempty"`
}

type UINavigation struct {
	ID       string `yaml:"id" json:"id"`
	Label    string `yaml:"label" json:"label"`
	RouteID  string `yaml:"route_id" json:"route_id"`
	Location string `yaml:"location,omitempty" json:"location,omitempty"`
	Order    int    `yaml:"order,omitempty" json:"order,omitempty"`
}

type UISlot struct {
	ID        string `yaml:"id" json:"id"`
	Slot      string `yaml:"slot" json:"slot"`
	SurfaceID string `yaml:"surface_id" json:"surface_id"`
	Order     int    `yaml:"order,omitempty" json:"order,omitempty"`
}

type UISurface struct {
	ID           string                 `yaml:"id" json:"id"`
	Version      string                 `yaml:"version" json:"version"`
	Type         string                 `yaml:"type" json:"type"`
	LayoutRole   string                 `yaml:"layout_role" json:"layout_role"`
	Renderer     string                 `yaml:"renderer,omitempty" json:"renderer,omitempty"`
	ModuleID     string                 `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	Schema       map[string]interface{} `yaml:"schema,omitempty" json:"schema,omitempty"`
	DataContract map[string]interface{} `yaml:"data_contract,omitempty" json:"data_contract,omitempty"`
	ActionIDs    []string               `yaml:"action_ids,omitempty" json:"action_ids,omitempty"`
	PublicTokens []string               `yaml:"public_tokens,omitempty" json:"public_tokens,omitempty"`
	Regions      []string               `yaml:"regions,omitempty" json:"regions,omitempty"`
}

type UIAction struct {
	ID         string                 `yaml:"id" json:"id"`
	Label      string                 `yaml:"label" json:"label"`
	Method     string                 `yaml:"method" json:"method"`
	Path       string                 `yaml:"path" json:"path"`
	Permission string                 `yaml:"permission,omitempty" json:"permission,omitempty"`
	Confirm    bool                   `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	Audit      bool                   `yaml:"audit,omitempty" json:"audit,omitempty"`
	Body       map[string]interface{} `yaml:"body,omitempty" json:"body,omitempty"`
}

func (ui UIContribution) Empty() bool {
	return len(ui.Routes)+len(ui.Navigation)+len(ui.Slots)+len(ui.Surfaces)+len(ui.Actions) == 0
}

func (m *Manifest) validateUI() error {
	if m.UI.Empty() {
		return nil
	}
	if m.UI.ContractVersion == "" {
		m.UI.ContractVersion = CurrentUIContract
	}
	if m.UI.ContractVersion != CurrentUIContract {
		return fmt.Errorf("manifest: unsupported ui.contract_version %q", m.UI.ContractVersion)
	}
	ids := map[string]string{}
	add := func(kind, id string) error {
		if !contributionIDPattern.MatchString(id) {
			return fmt.Errorf("manifest: ui %s id %q is invalid", kind, id)
		}
		if previous, ok := ids[id]; ok {
			return fmt.Errorf("manifest: ui id %q is duplicated by %s and %s", id, previous, kind)
		}
		ids[id] = kind
		return nil
	}
	surfaces := map[string]bool{}
	actions := map[string]bool{}
	routes := map[string]bool{}
	for _, action := range m.UI.Actions {
		if err := add("action", action.ID); err != nil {
			return err
		}
		method := strings.ToUpper(action.Method)
		if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
			return fmt.Errorf("manifest: ui action %q has unsupported method", action.ID)
		}
		if action.Path == "" || !strings.HasPrefix(action.Path, "/") || strings.Contains(action.Path, "..") {
			return fmt.Errorf("manifest: ui action %q has invalid extension path", action.ID)
		}
		actions[action.ID] = true
	}
	for _, surface := range m.UI.Surfaces {
		if err := add("surface", surface.ID); err != nil {
			return err
		}
		if surface.Version == "" || surface.Type == "" || surface.LayoutRole == "" {
			return fmt.Errorf("manifest: ui surface %q requires version, type and layout_role", surface.ID)
		}
		if surface.Renderer == "" || (surface.Renderer == "schema" && len(surface.Schema) == 0) || (surface.Renderer == "trusted-module" && surface.ModuleID == "") {
			return fmt.Errorf("manifest: ui surface %q requires a usable default renderer", surface.ID)
		}
		if surface.Renderer != "schema" && surface.Renderer != "trusted-module" {
			return fmt.Errorf("manifest: ui surface %q renderer must be schema or trusted-module", surface.ID)
		}
		for _, actionID := range surface.ActionIDs {
			if !actions[actionID] {
				return fmt.Errorf("manifest: ui surface %q references unknown action %q", surface.ID, actionID)
			}
		}
		surfaces[surface.ID] = true
	}
	for _, route := range m.UI.Routes {
		if err := add("route", route.ID); err != nil {
			return err
		}
		if !strings.HasPrefix(route.Path, "/") || strings.Contains(route.Path, "..") {
			return fmt.Errorf("manifest: ui route %q has invalid path", route.ID)
		}
		if !surfaces[route.SurfaceID] {
			return fmt.Errorf("manifest: ui route %q references unknown surface %q", route.ID, route.SurfaceID)
		}
		routes[route.ID] = true
	}
	for _, nav := range m.UI.Navigation {
		if err := add("navigation", nav.ID); err != nil {
			return err
		}
		if !routes[nav.RouteID] {
			return fmt.Errorf("manifest: ui navigation %q references unknown route %q", nav.ID, nav.RouteID)
		}
	}
	for _, slot := range m.UI.Slots {
		if err := add("slot", slot.ID); err != nil {
			return err
		}
		if !allowedUISlot(slot.Slot) || !surfaces[slot.SurfaceID] {
			return fmt.Errorf("manifest: ui slot %q has an invalid host slot or surface", slot.ID)
		}
	}
	return nil
}

func allowedUISlot(value string) bool {
	switch value {
	case "header", "primary-navigation", "secondary-navigation", "hero", "left-sidebar", "right-sidebar", "floating-action", "footer":
		return true
	default:
		return false
	}
}
