package httpapi

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/pkg/middleware"
	"github.com/gin-gonic/gin"
)

// Router exposes the real Gin engine together with the route ownership
// inventory produced by the same calls that register handlers.
type Router struct {
	*gin.Engine
	registry *platformroute.Registry
}

func newRouter(engine *gin.Engine, registry *platformroute.Registry) *Router {
	if len(engine.Routes()) != len(registry.Descriptors()) {
		panic(fmt.Sprintf("HTTP route registry mismatch: gin=%d registry=%d", len(engine.Routes()), len(registry.Descriptors())))
	}
	return &Router{Engine: engine, registry: registry}
}

func (r *Router) RouteDescriptors() []platformroute.Descriptor {
	return r.registry.Descriptors()
}

type ownedGroup struct {
	group       *gin.RouterGroup
	registry    *platformroute.Registry
	audience    platformroute.Audience
	permissions middleware.PermissionChecker
	permission  string
}

func newOwnedGroup(group *gin.RouterGroup, registry *platformroute.Registry, audience platformroute.Audience, permissions middleware.PermissionChecker) *ownedGroup {
	return &ownedGroup{group: group, registry: registry, audience: audience, permissions: permissions}
}

func (g *ownedGroup) Use(handlers ...gin.HandlerFunc) { g.group.Use(handlers...) }

func (g *ownedGroup) Permission(resource, action string) *ownedGroup {
	copy := *g
	copy.permission = strings.TrimSpace(resource) + ":" + strings.TrimSpace(action)
	return &copy
}

func (g *ownedGroup) GET(path string, handlers ...gin.HandlerFunc) {
	g.handle("GET", path, handlers...)
}
func (g *ownedGroup) POST(path string, handlers ...gin.HandlerFunc) {
	g.handle("POST", path, handlers...)
}
func (g *ownedGroup) PUT(path string, handlers ...gin.HandlerFunc) {
	g.handle("PUT", path, handlers...)
}
func (g *ownedGroup) PATCH(path string, handlers ...gin.HandlerFunc) {
	g.handle("PATCH", path, handlers...)
}
func (g *ownedGroup) DELETE(path string, handlers ...gin.HandlerFunc) {
	g.handle("DELETE", path, handlers...)
}

func (g *ownedGroup) handle(method, path string, handlers ...gin.HandlerFunc) {
	if len(handlers) == 0 {
		panic("HTTP route requires a handler")
	}
	permission := g.permission
	if g.audience == platformroute.AudienceAdmin {
		parts := strings.SplitN(permission, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			panic(fmt.Sprintf("admin route %s %s requires permission metadata", method, path))
		}
		handlers = append([]gin.HandlerFunc{middleware.RequirePermission(g.permissions, parts[0], parts[1])}, handlers...)
	}
	fullPath := strings.TrimSuffix(g.group.BasePath(), "/") + "/" + strings.TrimPrefix(path, "/")
	handlerName := runtimeFunctionName(handlers[len(handlers)-1])
	descriptor := platformroute.Descriptor{
		ID:         routeDescriptorID(method, fullPath),
		Owner:      moduleOwner(handlerName, fullPath),
		Method:     method,
		Path:       fullPath,
		Audience:   g.audience,
		Permission: permission,
		FeatureID:  routeFeature(fullPath),
	}
	switch g.audience {
	case platformroute.AudiencePublic:
		descriptor.Auth = "none"
		descriptor.Audit = "request-log-read"
	case platformroute.AudienceAuthenticated:
		descriptor.Auth = "jwt"
		descriptor.Audit = "request-log"
	case platformroute.AudienceAdmin:
		descriptor.Auth = "jwt+permission"
		descriptor.Audit = "request-log"
	}
	if strings.HasPrefix(fullPath, "/api/v1/moderation/") {
		descriptor.Audit = "moderation-audit"
	}
	if err := g.registry.Add(descriptor); err != nil {
		panic(err)
	}
	g.group.Handle(method, path, handlers...)
}

func runtimeFunctionName(handler gin.HandlerFunc) string {
	value := reflect.ValueOf(handler)
	if !value.IsValid() {
		return ""
	}
	function := runtime.FuncForPC(value.Pointer())
	if function == nil {
		return ""
	}
	return function.Name()
}

func moduleOwner(handlerName, path string) string {
	switch {
	case strings.Contains(handlerName, "/internal/core/identity/"):
		return "core.identity"
	case strings.Contains(handlerName, "/internal/community/"):
		return "core.community"
	case strings.Contains(handlerName, "/internal/moderation.") || strings.Contains(handlerName, "/internal/moderation/"):
		return "core.moderation"
	case strings.Contains(handlerName, "/internal/space.") || strings.Contains(handlerName, "/internal/space/"):
		return "feature.personal-space"
	case strings.Contains(handlerName, "/internal/richtext.") || strings.Contains(handlerName, "/internal/richtext/"):
		return "feature.controlled-richtext-article"
	case strings.Contains(handlerName, "/internal/schedule.") || strings.Contains(handlerName, "/internal/schedule/"):
		return "feature.personal-schedule"
	case strings.Contains(handlerName, "/internal/homepage.") || strings.Contains(handlerName, "/internal/homepage/"), strings.Contains(handlerName, "/internal/webtheme.") || strings.Contains(handlerName, "/internal/webtheme/"):
		return "feature.appearance"
	case strings.Contains(handlerName, "/internal/plugin.") || strings.Contains(handlerName, "/internal/plugin/"):
		return "core.plugin-platform"
	case strings.Contains(handlerName, "/internal/ai.") || strings.Contains(handlerName, "/internal/ai/"):
		return "feature.ai-gateway"
	case strings.Contains(handlerName, "/internal/webhook.") || strings.Contains(handlerName, "/internal/webhook/"):
		return "feature.webhook"
	case strings.Contains(handlerName, "/internal/mcp.") || strings.Contains(handlerName, "/internal/mcp/"):
		return "feature.mcp"
	case strings.Contains(handlerName, "/internal/message.") || strings.Contains(handlerName, "/internal/message/"):
		return "feature.message"
	case strings.Contains(handlerName, "/internal/platformlog.") || strings.Contains(handlerName, "/internal/platformlog/"):
		return "feature.platform-log"
	case strings.Contains(handlerName, "/internal/integration.") || strings.Contains(handlerName, "/internal/integration/"):
		return "feature.integration-overview"
	default:
		panic(fmt.Sprintf("HTTP route %s has no module owner (handler %s)", path, handlerName))
	}
}

func routeDescriptorID(method, path string) string {
	value := strings.NewReplacer("/", ".", ":", "", "*", "wildcard", "-", "_").Replace(path)
	return "http." + strings.ToLower(method) + "." + strings.Trim(value, ".")
}

func routeFeature(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/spaces"), strings.HasPrefix(path, "/api/v1/space"), strings.HasPrefix(path, "/api/v1/u/"):
		return "personal-space"
	case strings.HasPrefix(path, "/api/v1/richtext"):
		return "controlled-richtext-article"
	case strings.HasPrefix(path, "/api/v1/schedule"):
		return "personal-schedule"
	case strings.HasPrefix(path, "/api/v1/home"), strings.HasPrefix(path, "/api/v1/web-themes"):
		return "appearance"
	default:
		return ""
	}
}
