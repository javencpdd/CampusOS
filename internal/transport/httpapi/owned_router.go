package httpapi

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/permissioncode"
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
	group          *gin.RouterGroup
	registry       *platformroute.Registry
	audience       platformroute.Audience
	permissions    middleware.PermissionChecker
	permission     string
	permissionCode string
	operation      string
	scopeType      string
}

func newOwnedGroup(group *gin.RouterGroup, registry *platformroute.Registry, audience platformroute.Audience, permissions middleware.PermissionChecker) *ownedGroup {
	return &ownedGroup{group: group, registry: registry, audience: audience, permissions: permissions}
}

func (g *ownedGroup) Use(handlers ...gin.HandlerFunc) { g.group.Use(handlers...) }

func (g *ownedGroup) Permission(resource, action string) *ownedGroup {
	copy := *g
	copy.permission = strings.TrimSpace(resource) + ":" + strings.TrimSpace(action)
	copy.permissionCode = permissioncode.FromLegacy(resource, action)
	return &copy
}

// PermissionCode binds a v10 stable permission while retaining the legacy
// resource:action pair for rolling upgrades and legacy audit correlation.
func (g *ownedGroup) PermissionCode(code string) *ownedGroup {
	copy := *g
	code = strings.TrimSpace(code)
	copy.permissionCode = code
	if resource, action, ok := permissioncode.LegacyForCode(code); ok {
		copy.permission = resource + ":" + action
	}
	return &copy
}

// Operation lets new or renamed routes declare a stable transport operation.
// Existing routes receive an equally stable handler-based operation until they
// are explicitly named in their owning module.
func (g *ownedGroup) Operation(code string) *ownedGroup {
	copy := *g
	copy.operation = strings.TrimSpace(code)
	return &copy
}

// Scoped declares that a route's concrete authorization scope is derived by
// its service from the persisted resource, rather than from request input.
func (g *ownedGroup) Scoped(scopeType string) *ownedGroup {
	copy := *g
	copy.scopeType = strings.TrimSpace(scopeType)
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
	fullPath := strings.TrimSuffix(g.group.BasePath(), "/")
	if strings.TrimSpace(path) != "" {
		fullPath += "/" + strings.TrimPrefix(path, "/")
	}
	handlerName := runtimeFunctionName(handlers[len(handlers)-1])
	owner := moduleOwner(handlerName, fullPath)
	operation := g.operation
	if operation == "" {
		// Existing routes retain their URL-derived operation as a compatibility
		// baseline. New/high-risk routes declare Operation explicitly; no new
		// route may rely on a URL-derived identifier after the v10 transition.
		operation = routeDescriptorID(method, fullPath)
	}
	if operation == "" {
		panic(fmt.Sprintf("HTTP route %s %s has no operation code", method, fullPath))
	}
	permission := g.permission
	permissionCode := g.permissionCode
	if g.audience == platformroute.AudienceAdmin {
		parts := strings.SplitN(permission, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			panic(fmt.Sprintf("admin route %s %s requires permission metadata", method, path))
		}
		if permissionCode == "" {
			permissionCode = permissioncode.FromLegacy(parts[0], parts[1])
		}
		if permissionCode == "" {
			panic(fmt.Sprintf("admin route %s %s requires permission code metadata", method, path))
		}
		permissionMiddleware := middleware.RequirePermissionForOperation(g.permissions, parts[0], parts[1], permissionCode, operation)
		if g.scopeType != "" {
			permissionMiddleware = middleware.RequireScopedPermissionForOperation(g.permissions, parts[0], parts[1], permissionCode, operation, g.scopeType)
		}
		handlers = append([]gin.HandlerFunc{permissionMiddleware}, handlers...)
	}
	descriptor := platformroute.Descriptor{
		ID:             routeDescriptorID(method, fullPath),
		OperationCode:  operation,
		LegacyAliases:  []string{routeDescriptorID(method, fullPath), "httpapi." + strings.TrimPrefix(routeDescriptorID(method, fullPath), "http.")},
		Owner:          owner,
		Method:         method,
		Path:           fullPath,
		Audience:       g.audience,
		Permission:     permission,
		PermissionCode: permissionCode,
		FeatureID:      routeFeature(fullPath),
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
	case strings.Contains(handlerName, "/internal/transport/httpapi/"), strings.Contains(handlerName, "/internal/transport/httpapi."):
		return "core.platform-api"
	case strings.Contains(handlerName, "/internal/modules/core/identity/"):
		return "core.identity"
	case strings.Contains(handlerName, "/internal/modules/core/community/"):
		return "core.community"
	case strings.Contains(handlerName, "/internal/modules/core/moderation.") || strings.Contains(handlerName, "/internal/modules/core/moderation/"):
		return "core.moderation"
	case strings.Contains(handlerName, "/internal/modules/features/personalspace.") || strings.Contains(handlerName, "/internal/modules/features/personalspace/"):
		return "feature.personal-space"
	case strings.Contains(handlerName, "/internal/modules/features/richtext.") || strings.Contains(handlerName, "/internal/modules/features/richtext/"):
		return "feature.controlled-richtext-article"
	case strings.Contains(handlerName, "/internal/modules/features/schedule.") || strings.Contains(handlerName, "/internal/modules/features/schedule/"):
		return "feature.personal-schedule"
	case strings.Contains(handlerName, "/internal/modules/features/appearance/homepage.") || strings.Contains(handlerName, "/internal/modules/features/appearance/homepage/"), strings.Contains(handlerName, "/internal/modules/features/appearance/webtheme.") || strings.Contains(handlerName, "/internal/modules/features/appearance/webtheme/"):
		return "feature.appearance"
	case strings.Contains(handlerName, "/internal/platform/feature.") || strings.Contains(handlerName, "/internal/platform/feature/"):
		return "core.feature-registry"
	case strings.Contains(handlerName, "/internal/platform/reliability.") || strings.Contains(handlerName, "/internal/platform/reliability/"):
		return "core.reliability"
	case strings.Contains(handlerName, "/internal/plugin.") || strings.Contains(handlerName, "/internal/plugin/"):
		return "core.plugin-platform"
	case strings.Contains(handlerName, "/internal/modules/features/ai.") || strings.Contains(handlerName, "/internal/modules/features/ai/"):
		return "feature.ai-gateway"
	case strings.Contains(handlerName, "/internal/modules/features/webhook.") || strings.Contains(handlerName, "/internal/modules/features/webhook/"):
		return "feature.webhook"
	case strings.Contains(handlerName, "/internal/modules/features/mcp.") || strings.Contains(handlerName, "/internal/modules/features/mcp/"):
		return "feature.mcp"
	case strings.Contains(handlerName, "/internal/modules/features/message.") || strings.Contains(handlerName, "/internal/modules/features/message/"):
		return "feature.message"
	case strings.Contains(handlerName, "/internal/modules/features/platformlog.") || strings.Contains(handlerName, "/internal/modules/features/platformlog/"):
		return "feature.platform-log"
	case strings.Contains(handlerName, "/internal/modules/features/integration.") || strings.Contains(handlerName, "/internal/modules/features/integration/"):
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
