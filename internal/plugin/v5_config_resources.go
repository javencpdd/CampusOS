package plugin

import (
	"context"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/response"
	"github.com/gin-gonic/gin"
)

// Compiled host readers expose currently visible references only. This port
// never creates a Profile/Knowledge collection or reads Secret plaintext.
// Absent business providers return empty choices and cannot authorize a value.
type V5ConfigOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type V5ConfigResourceReader interface {
	ConfigurationOptions(context.Context, V5Coordinate, string, string, string) ([]V5ConfigOption, error)
}
type V5ConfigOptions struct {
	Profiles    []V5ConfigOption            `json:"profiles"`
	Collections []V5ConfigOption            `json:"public_collections"`
	Secrets     map[string][]V5ConfigOption `json:"secret_refs"`
}

func (s *V5Platform) configOptions(ctx context.Context, c V5Coordinate, owner, kind, name string) ([]V5ConfigOption, error) {
	if s.ConfigResources == nil {
		return []V5ConfigOption{}, nil
	}
	items, err := s.ConfigResources.ConfigurationOptions(ctx, c, owner, kind, name)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	if len(items) > 128 {
		return nil, ErrV5PlatformUnavailable
	}
	out := []V5ConfigOption{}
	seen := map[string]bool{}
	for _, item := range items {
		if len(item.ID) < 1 || len(item.ID) > 128 || len(item.Title) < 1 || len(item.Title) > 256 || seen[item.ID] || kind == "secret_ref" && !pluginV5SecretRefPattern.MatchString(item.ID) {
			return nil, ErrV5PlatformUnavailable
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out, nil
}
func (s *V5Platform) configResourceAllowed(ctx context.Context, c V5Coordinate, owner, kind, name, value string) (bool, error) {
	options, err := s.configOptions(ctx, c, owner, kind, name)
	if err != nil {
		return false, err
	}
	for _, o := range options {
		if o.ID == value {
			return true, nil
		}
	}
	return false, nil
}
func (s *V5Platform) ConfigurationOptions(ctx context.Context, c V5Coordinate, admin bool, resolve identity.SessionPrincipalResolver) (V5ConfigOptions, error) {
	out := V5ConfigOptions{Profiles: []V5ConfigOption{}, Collections: []V5ConfigOption{}, Secrets: map[string][]V5ConfigOption{}}
	if !c.valid() {
		return out, ErrV5Invalid
	}
	err := s.read(ctx, admin, resolve, func(tx context.Context, a v5Actor) error {
		if err := s.store.lock(tx, c.family()); err != nil {
			return err
		}
		entry, err := s.entry(tx, c, a, true)
		if err != nil {
			return err
		}
		if entry.Configuration == nil {
			return nil
		}
		d, err := parsePluginV5ConfigDefinition(entry.Release.Manifest)
		if err != nil {
			return err
		}
		owner := ""
		if !admin {
			owner = a.p.Actor.ID
		}
		for _, sel := range d.Selectors {
			switch sel.Kind {
			case "profile":
				out.Profiles, err = s.configOptions(tx, c, owner, sel.Kind, "")
			case "public_collection":
				out.Collections, err = s.configOptions(tx, c, owner, sel.Kind, "")
			}
			if err != nil {
				return err
			}
		}
		for _, name := range d.SecretNames {
			out.Secrets[name], err = s.configOptions(tx, c, owner, "secret_ref", name)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
func (h *V5HTTPHandler) ConfigOptions(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !v5HTTPStart(c) {
			return
		}
		if h == nil || h.Platform == nil {
			v5HTTPError(c, ErrV5PlatformUnavailable)
			return
		}
		out, err := h.Platform.ConfigurationOptions(c.Request.Context(), V5Coordinate{c.Param("publisher"), c.Param("key"), c.Param("version")}, admin, h.resolver(c, admin))
		if err != nil {
			v5HTTPError(c, err)
			return
		}
		response.Success(c, out)
	}
}
