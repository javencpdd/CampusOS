package projectaudit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/permissioncode"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	platformversion "github.com/campusos/CampusOS/internal/platform/version"
)

const (
	APIPrefix              = "/api/v1"
	OpenAPIContractVersion = platformversion.OpenAPI
)

var (
	routePattern                 = regexp.MustCompile(`^\s*(public|authenticated|admin|integration)(.*?)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)",(.*)$`)
	packageHandlerPattern        = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z_0-9]*)\s*\)`)
	legacyRoutePermissionPattern = regexp.MustCompile(`\.Permission\("([^"]+)",\s*"([^"]+)"\)`)
	permissionCodePattern        = regexp.MustCompile(`\.PermissionCode\("([^"]+)"\)`)
	operationPattern             = regexp.MustCompile(`\.Operation\("([^"]+)"\)`)
	legacyOperationAliasPattern  = regexp.MustCompile(`\.LegacyOperationAlias\("([^"]+)"\)`)
	scopedPattern                = regexp.MustCompile(`\.Scoped\("([^"]+)"\)`)
	permissionPattern            = regexp.MustCompile(`RequirePermission\([^,]+,\s*"([^"]+)",\s*"([^"]+)"\)`)
	selectorPattern              = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*)\.([A-Za-z][A-Za-z0-9_]*)`)
)

// RouteContract is the machine-readable authorization record for one Gin route.
type RouteContract struct {
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Handler        string   `json:"handler"`
	ModuleOwner    string   `json:"module_owner"`
	Audience       string   `json:"audience"`
	Auth           string   `json:"auth"`
	Permission     string   `json:"permission,omitempty"`
	PermissionCode string   `json:"permission_code,omitempty"`
	OperationCode  string   `json:"operation_code"`
	LegacyAliases  []string `json:"legacy_aliases,omitempty"`
	Ownership      string   `json:"ownership"`
	Scope          string   `json:"scope"`
	Audit          string   `json:"audit"`
	Stability      string   `json:"stability"`
	SourceLine     int      `json:"-"`
}

func ParseServerRoutes(path string) ([]RouteContract, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open route source: %w", err)
	}
	defer file.Close()

	var routes []RouteContract
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		matches := routePattern.FindStringSubmatch(scanner.Text())
		if len(matches) == 0 {
			continue
		}
		group, prefix := matches[1], matches[2]
		method, routePath, arguments := matches[3], matches[4], matches[5]
		selectors := selectorPattern.FindAllStringSubmatch(arguments, -1)
		var handler string
		if len(selectors) > 0 {
			handler = selectors[len(selectors)-1][1] + "." + selectors[len(selectors)-1][2]
			base := selectors[len(selectors)-1][1]
			if strings.HasPrefix(base, "d.") && strings.Count(base, ".") == 1 {
				name := strings.TrimPrefix(base, "d.")
				handler = strings.ToLower(name[:1]) + name[1:] + "Handler." + selectors[len(selectors)-1][2]
			}
		} else if match := packageHandlerPattern.FindStringSubmatch(arguments); len(match) > 0 {
			handler = "httpapi." + match[1]
		} else {
			return nil, fmt.Errorf("route at line %d has no handler", lineNumber)
		}
		route := RouteContract{
			Method:      method,
			Path:        APIPrefix + routePath,
			Handler:     handler,
			ModuleOwner: moduleOwnerFor(handler, APIPrefix+routePath),
			Audience:    group,
			Stability:   "experimental",
			SourceLine:  lineNumber,
		}
		applyAuthorization(&route, group, method)
		if scope := scopedPattern.FindStringSubmatch(prefix); group == "admin" && len(scope) > 0 {
			route.Auth = "jwt+admin-account-or-scope+permission"
			route.Ownership = scope[1] + "-membership"
			route.Scope = "assigned-" + scope[1] + "-or-global-admin"
		}
		if permission := legacyRoutePermissionPattern.FindStringSubmatch(prefix); len(permission) > 0 {
			route.Permission = permission[1] + ":" + permission[2]
			route.PermissionCode = permissionCodeFor(route.Permission)
		}
		if permission := permissionCodePattern.FindStringSubmatch(prefix); len(permission) > 0 {
			route.PermissionCode = permission[1]
			if resource, action, ok := legacyPermissionForCode(route.PermissionCode); ok {
				route.Permission = resource + ":" + action
			}
		}
		if permission := permissionPattern.FindStringSubmatch(arguments); len(permission) > 0 {
			route.Permission = permission[1] + ":" + permission[2]
			route.PermissionCode = permissionCodeFor(route.Permission)
		}
		if group == "admin" && route.Permission == "" {
			return nil, fmt.Errorf("admin route %s %s has no explicit permission", method, route.Path)
		}
		if group == "admin" && route.PermissionCode == "" {
			return nil, fmt.Errorf("admin route %s %s has no permission code", method, route.Path)
		}
		if operation := operationPattern.FindStringSubmatch(prefix); len(operation) > 0 {
			route.OperationCode = operation[1]
		} else {
			route.OperationCode = "http." + strings.ToLower(method) + "." + routeID(route.Path)
		}
		applyResourceGovernanceContract(&route)
		route.LegacyAliases = []string{
			"http." + strings.ToLower(method) + "." + routeID(route.Path),
			"httpapi." + strings.ToLower(method) + "." + routeID(route.Path),
		}
		for _, alias := range legacyOperationAliasPattern.FindAllStringSubmatch(prefix, -1) {
			if len(alias) > 1 && !containsRouteAlias(route.LegacyAliases, alias[1]) {
				route.LegacyAliases = append(route.LegacyAliases, alias[1])
			}
		}
		routes = append(routes, route)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan route source: %w", err)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found in %s", path)
	}
	if err := ValidateRouteDescriptors(routes); err != nil {
		return nil, err
	}
	return routes, nil
}

func containsRouteAlias(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// ValidateRouteDescriptors registers the source-derived transport contract in
// the platform Route Registry before generated API artifacts are accepted.
func ValidateRouteDescriptors(routes []RouteContract) error {
	registry := platformroute.NewRegistry()
	for _, item := range routes {
		if strings.HasPrefix(item.ModuleOwner, "unowned:") || item.ModuleOwner == "" || item.ModuleOwner == "transport.httpapi" {
			return fmt.Errorf("route %s %s has no business module owner", item.Method, item.Path)
		}
		operationCode := item.OperationCode
		if operationCode == "" {
			operationCode = "http." + strings.ToLower(item.Method) + "." + routeID(item.Path)
		}
		permissionCode := item.PermissionCode
		if permissionCode == "" && item.Permission != "" {
			permissionCode = permissionCodeFor(item.Permission)
		}
		descriptor := platformroute.Descriptor{
			ID:             "httpapi." + strings.ToLower(item.Method) + "." + routeID(item.Path),
			OperationCode:  operationCode,
			LegacyAliases:  append([]string(nil), item.LegacyAliases...),
			Owner:          item.ModuleOwner,
			Method:         item.Method,
			Path:           item.Path,
			Audience:       platformroute.Audience(item.Audience),
			Auth:           item.Auth,
			Permission:     item.Permission,
			PermissionCode: permissionCode,
			FeatureID:      featureForRoute(item.Path),
			Audit:          item.Audit,
		}
		if err := registry.Add(descriptor); err != nil {
			return fmt.Errorf("register route descriptor %s %s: %w", item.Method, item.Path, err)
		}
	}
	if len(registry.Descriptors()) != len(routes) {
		return fmt.Errorf("route descriptor count mismatch: got %d, want %d", len(registry.Descriptors()), len(routes))
	}
	return nil
}

func moduleOwnerFor(handler, path string) string {
	switch {
	case path == APIPrefix:
		return "core.platform-api"
	case strings.HasPrefix(path, APIPrefix+"/moderation"):
		return "core.moderation"
	case strings.HasPrefix(path, APIPrefix+"/auth"), strings.HasPrefix(path, APIPrefix+"/identity"), strings.HasPrefix(path, APIPrefix+"/admin/identity"), strings.HasPrefix(path, APIPrefix+"/users"), strings.HasPrefix(path, APIPrefix+"/admin/users"), strings.HasPrefix(path, APIPrefix+"/roles"), strings.HasPrefix(path, APIPrefix+"/permissions"), strings.HasPrefix(path, APIPrefix+"/authorization-audits"), path == APIPrefix+"/health":
		return "core.identity"
	case strings.HasPrefix(path, APIPrefix+"/categories"), strings.HasPrefix(path, APIPrefix+"/threads"), strings.HasPrefix(path, APIPrefix+"/notifications"), path == APIPrefix+"/events", strings.HasPrefix(path, APIPrefix+"/admin/threads"), strings.HasPrefix(path, APIPrefix+"/admin/posts"), strings.HasPrefix(path, APIPrefix+"/admin/categories"):
		return "core.community"
	case path == APIPrefix+"/content/preview":
		return "core.community"
	case strings.HasPrefix(path, APIPrefix+"/content/assets"):
		return "core.user-storage"
	case strings.HasPrefix(path, APIPrefix+"/admin/academic-terms"), path == APIPrefix+"/schedule/terms":
		return "core.academic-term"
	case strings.HasPrefix(path, APIPrefix+"/spaces"), strings.HasPrefix(path, APIPrefix+"/space"), strings.HasPrefix(path, APIPrefix+"/u/"), strings.HasPrefix(path, APIPrefix+"/appearance/space-style-packs"):
		return "feature.personal-space"
	case strings.HasPrefix(path, APIPrefix+"/richtext"):
		return "feature.controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/admin/assets"):
		return "feature.controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/assets"):
		return "feature.controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/plugin-ui"):
		return "core.plugin-platform"
	case strings.HasPrefix(path, APIPrefix+"/mutual-aid"):
		return "feature.mutual-aid"
	case strings.HasPrefix(path, APIPrefix+"/secondhand"):
		return "feature.secondhand"
	case strings.HasPrefix(path, APIPrefix+"/schedule"):
		return "feature.personal-schedule"
	case strings.HasPrefix(path, APIPrefix+"/knowledge"), strings.HasPrefix(path, APIPrefix+"/admin/knowledge"), strings.HasPrefix(path, APIPrefix+"/integration/knowledge/"):
		return "feature.knowledge"
	case strings.HasPrefix(path, APIPrefix+"/documents"):
		return "feature.personal-documents"
	case strings.HasPrefix(path, APIPrefix+"/home"), strings.HasPrefix(path, APIPrefix+"/web-themes"):
		return "feature.appearance"
	case strings.HasPrefix(path, APIPrefix+"/features"):
		return "core.feature-registry"
	case strings.HasPrefix(path, APIPrefix+"/platform/reliability"):
		return "core.reliability"
	case strings.HasPrefix(path, APIPrefix+"/platform/email-delivery"):
		return "core.email-delivery"
	case strings.HasPrefix(path, APIPrefix+"/plugin-v5"), strings.HasPrefix(path, APIPrefix+"/admin/plugin-v5"), strings.HasPrefix(path, APIPrefix+"/plugins"), strings.HasPrefix(path, APIPrefix+"/plugin-packages"), strings.HasPrefix(path, APIPrefix+"/plugin-market"), strings.HasPrefix(path, APIPrefix+"/plugin-capabilities"), strings.HasPrefix(path, APIPrefix+"/plugin-authorizations"), strings.HasPrefix(path, APIPrefix+"/extensions"), strings.HasPrefix(path, APIPrefix+"/ui/"):
		return "core.plugin-platform"
	case strings.HasPrefix(path, APIPrefix+"/ai"):
		return "feature.ai-gateway"
	case strings.HasPrefix(path, APIPrefix+"/webhooks"):
		return "feature.webhook"
	case strings.HasPrefix(path, APIPrefix+"/mcp"):
		return "feature.mcp"
	case strings.HasPrefix(path, APIPrefix+"/messages"):
		return "feature.message"
	case strings.HasPrefix(path, APIPrefix+"/platform/logs"):
		return "feature.platform-log"
	case strings.HasPrefix(path, APIPrefix+"/integration/identity"):
		return "core.identity"
	case strings.HasPrefix(path, APIPrefix+"/integration/"), strings.HasPrefix(path, APIPrefix+"/integrations"), strings.HasPrefix(path, APIPrefix+"/metrics"):
		return "feature.integration-overview"
	default:
		return "unowned:" + handler
	}
}

func permissionCodeFor(permission string) string {
	parts := strings.SplitN(permission, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return permissioncode.FromLegacy(parts[0], parts[1])
}

func legacyPermissionForCode(code string) (string, string, bool) {
	return permissioncode.LegacyForCode(code)
}

func routeID(path string) string {
	replacer := strings.NewReplacer("/", ".", ":", "", "*", "wildcard", "-", "_")
	return strings.Trim(replacer.Replace(path), ".")
}

func featureForRoute(path string) string {
	switch {
	case strings.HasPrefix(path, APIPrefix+"/space"), strings.HasPrefix(path, APIPrefix+"/u/"), strings.HasPrefix(path, APIPrefix+"/appearance/space-style-packs"):
		return "personal-space"
	case strings.HasPrefix(path, APIPrefix+"/richtext"):
		return "controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/admin/assets"):
		return "controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/assets"):
		return "controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/plugin-ui"):
		return "pdf-viewer"
	case strings.HasPrefix(path, APIPrefix+"/schedule"):
		return "personal-schedule"
	case strings.HasPrefix(path, APIPrefix+"/knowledge"), strings.HasPrefix(path, APIPrefix+"/admin/knowledge"), strings.HasPrefix(path, APIPrefix+"/integration/knowledge/"):
		return "knowledge"
	case strings.HasPrefix(path, APIPrefix+"/documents"):
		return "personal-documents"
	case strings.HasPrefix(path, APIPrefix+"/mutual-aid"):
		return "mutual-aid"
	case strings.HasPrefix(path, APIPrefix+"/secondhand"):
		return "secondhand"
	case strings.HasPrefix(path, APIPrefix+"/home"), strings.HasPrefix(path, APIPrefix+"/web-themes"):
		return "appearance"
	default:
		return ""
	}
}

func applyAuthorization(route *RouteContract, group, method string) {
	switch group {
	case "public":
		route.Auth = "none"
		route.Ownership = "none"
		route.Scope = "public"
		route.Audit = "request-log"
	case "authenticated":
		route.Auth = "jwt"
		route.Ownership = "handler-enforced"
		route.Scope = "self-or-resource-owner"
		route.Audit = "request-log"
		if strings.Contains(route.Path, "/moderation/") {
			route.Ownership = "category-membership"
			route.Scope = "assigned-category"
			route.Audit = "moderation-audit"
		}
	case "admin":
		route.Auth = "jwt+admin-account+permission"
		route.Ownership = "none"
		route.Scope = "global"
		route.Audit = "request-log"
	case "integration":
		route.Auth = "integration-digest+current-facts+whitelist+source-acl"
		route.Ownership = "source-module-public-policy"
		route.Scope = "exact-channel-endpoint-resource"
		route.Audit = "required-command+outbox"
	}
	if method == "GET" && route.Audit == "request-log" {
		route.Audit = "request-log-read"
	}
	if strings.HasPrefix(route.OperationCode, "http.plugin.v5.") {
		route.Auth = "jwt+current-user-session"
		route.Ownership = "current-user-derived"
		route.Scope = "own-installation"
		if route.Audience == "admin" {
			route.Auth = "admin-jwt+current-admin-session+current-permission"
			route.Scope = "plugin-installation-inventory"
			route.Ownership = "current-admin-domain"
		}
		if method != "GET" {
			route.Auth += "+browser-csrf"
			if route.Audience == "admin" {
				route.Auth += "+recent-mfa"
			}
			route.Audit = "required-command+outbox"
		}
	}
	if route.Path == APIPrefix+"/auth/refresh" {
		route.Auth = "refresh-cookie+csrf"
		route.Ownership = "session-cookie"
		route.Scope = "current-session"
		route.Audit = "identity-session-audit"
	}
	if strings.HasPrefix(route.Path, APIPrefix+"/auth/password-reset/") || route.Path == APIPrefix+"/auth/recovery/complete" {
		route.Audit = "identity-recovery-audit"
	}
	if strings.HasPrefix(route.Path, APIPrefix+"/auth/email-binding/") {
		route.Auth = "jwt+csrf"
		route.Ownership = "current-session"
		route.Scope = "self"
		route.Audit = "identity-recovery-audit"
	}
	if strings.HasPrefix(route.Path, APIPrefix+"/auth/mfa/") {
		route.Audit = "identity-session-audit"
		if route.Path == APIPrefix+"/auth/mfa/login/complete" {
			route.Ownership = "mfa-ticket"
			route.Scope = "single-use-ticket"
		} else if method != "GET" {
			route.Auth = "jwt+csrf"
			route.Ownership = "current-session"
			route.Scope = "self"
		}
	}
	if strings.HasPrefix(route.Path, APIPrefix+"/identity/") {
		route.Audit = "identity-recovery-audit"
	}
	if route.Path == APIPrefix+"/auth/logout" || route.Path == APIPrefix+"/auth/logout-all" || strings.HasPrefix(route.Path, APIPrefix+"/auth/sessions/") && method != "GET" {
		route.Auth = "jwt+csrf"
		route.Audit = "identity-session-audit"
	}
}

func RoutesJSON(routes []RouteContract) ([]byte, error) {
	payload := struct {
		Version string          `json:"version"`
		Routes  []RouteContract `json:"routes"`
	}{Version: routeContractVersion(), Routes: routes}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func RoutesMarkdown(routes []RouteContract) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "# CampusOS HTTP 路由与授权矩阵 %s\n\n", routeContractVersion())
	out.WriteString("> 本文档由 `go run ./cmd/campusos-contracts --write` 根据 `internal/transport/httpapi/router.go` 生成，请勿手工编辑。\n\n")
	out.WriteString("当前接口均标记为 `experimental`；进入 stable 前不得承诺无弃用期的兼容性。`handler-enforced` 表示资源归属和字段过滤由对应 handler/service 负责。\n\n")
	out.WriteString("| Method | Path | Operation | Module | Handler | Auth | Permission Code | Ownership | Scope | Audit |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, route := range routes {
		permission := route.PermissionCode
		if permission == "" {
			permission = "-"
		}
		fmt.Fprintf(&out, "| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` |\n",
			route.Method, route.Path, route.OperationCode, route.ModuleOwner, route.Handler, route.Auth, permission, route.Ownership, route.Scope, route.Audit)
	}
	return []byte(out.String())
}

func routeContractVersion() string {
	parts := strings.Split(platformversion.Number, ".")
	if len(parts) < 2 {
		return "v" + platformversion.Number
	}
	return "v" + parts[0] + "." + parts[1]
}

func OpenAPI(routes []RouteContract) []byte {
	grouped := make(map[string][]RouteContract)
	var paths []string
	for _, route := range routes {
		path := openAPIPath(route.Path)
		if _, exists := grouped[path]; !exists {
			paths = append(paths, path)
		}
		grouped[path] = append(grouped[path], route)
	}
	sort.Strings(paths)
	var out strings.Builder
	fmt.Fprintf(&out, "openapi: 3.0.3\ninfo:\n  title: CampusOS Current HTTP API\n  version: %s\n  description: Generated route, authorization, request-body and core field contract. Operations using GenericObject remain experimental.\nservers:\n  - url: http://localhost:8080\npaths:\n", OpenAPIContractVersion)
	for _, path := range paths {
		fmt.Fprintf(&out, "  %s:\n", path)
		sort.Slice(grouped[path], func(i, j int) bool { return grouped[path][i].Method < grouped[path][j].Method })
		for _, route := range grouped[path] {
			profile := profileFor(route)
			fmt.Fprintf(&out, "    %s:\n", strings.ToLower(route.Method))
			fmt.Fprintf(&out, "      operationId: %s\n", strings.ReplaceAll(route.Handler, ".", "_"))
			fmt.Fprintf(&out, "      summary: %s %s\n", route.Method, route.Path)
			fmt.Fprintf(&out, "      tags: [%s]\n", route.Audience)
			fmt.Fprintf(&out, "      x-campusos-stability: %s\n", route.Stability)
			fmt.Fprintf(&out, "      x-campusos-ownership: %s\n", route.Ownership)
			fmt.Fprintf(&out, "      x-campusos-module-owner: %s\n", route.ModuleOwner)
			fmt.Fprintf(&out, "      x-campusos-scope: %s\n", route.Scope)
			fmt.Fprintf(&out, "      x-campusos-operation: %s\n", route.OperationCode)
			if strings.Contains(route.Auth, "admin-account") || strings.Contains(route.Auth, "current-admin-session") || route.OperationCode == "http.knowledge.private.source.preflight" || route.Audience == "integration" || strings.Contains(route.OperationCode, "http.identity.admin_") || route.Auth == "none+current-origin-policy" {
				fmt.Fprintf(&out, "      x-campusos-auth: %s\n", route.Auth)
			}
			if route.PermissionCode != "" {
				fmt.Fprintf(&out, "      x-campusos-permission: %s\n", route.PermissionCode)
			}
			authenticated := route.Auth != "none" && route.Auth != "none+current-origin-policy"
			if authenticated {
				if route.Auth == "admin-refresh-cookie+csrf" {
					out.WriteString("      security:\n        - adminRefreshCookie: []\n")
				} else if route.Audience == "integration" {
					out.WriteString("      security:\n        - integrationCredential: []\n")
				} else if route.Auth == "refresh-cookie+csrf" {
					out.WriteString("      security:\n        - refreshCookie: []\n")
				} else {
					out.WriteString("      security:\n        - bearerAuth: []\n")
				}
			}
			parameters := openAPIPathParameters(path)
			knowledgeQuery := knowledgeOpenAPIQueryParameters(route.OperationCode)
			requiresAdminCSRF := (strings.HasPrefix(route.OperationCode, "http.identity.admin_") || strings.Contains(route.Auth, "current-admin-session")) && strings.Contains(route.Auth, "csrf")
			requiresCSRF := strings.Contains(route.Auth, "csrf") && !requiresAdminCSRF
			if len(parameters) > 0 || profile.Paginated || requiresCSRF || requiresAdminCSRF || knowledgeQuery != "" {
				out.WriteString("      parameters:\n")
				for _, parameter := range parameters {
					if contract := knowledgeOpenAPIPathParameter(route.OperationCode, parameter); contract != "" {
						out.WriteString(contract)
						continue
					}
					fmt.Fprintf(&out, "        - name: %s\n          in: path\n          required: true\n          schema:\n            type: string\n", parameter)
				}
				if profile.Paginated {
					out.WriteString("        - name: page\n          in: query\n          schema: { type: integer, minimum: 1, default: 1 }\n")
					out.WriteString("        - name: page_size\n          in: query\n          schema: { type: integer, minimum: 1, maximum: 100, default: 20 }\n")
				}
				out.WriteString(knowledgeQuery)
				if requiresAdminCSRF {
					out.WriteString("        - name: X-Admin-CSRF-Token\n          in: header\n          required: false\n          description: Required for browser/cookie requests; CLI body refresh requires explicit server compatibility configuration.\n          schema: {type: string}\n")
				}
				if requiresCSRF {
					out.WriteString("        - name: X-CSRF-Token\n          in: header\n          required: true\n          schema: { type: string }\n")
				}
			}
			if profile.RequestSchema != "" && !profile.NoBody {
				fmt.Fprintf(&out, "      requestBody:\n        required: %t\n        content:\n", !profile.RequestOptional)
				fmt.Fprintf(&out, "          %s:\n            schema:\n              $ref: '#/components/schemas/%s'\n", profile.ContentType, profile.RequestSchema)
				if profile.RequestSchema == "GenericObject" || profile.RequestSchema == "MultipartRequest" {
					out.WriteString("      x-campusos-schema-level: generic-experimental\n")
				} else {
					out.WriteString("      x-campusos-schema-level: field-contract\n")
				}
			}
			out.WriteString("      responses:\n")
			if profile.DirectJSON {
				// MCP has direct JSON-RPC and empty notification/unsupported-stream
				// replies. Middleware may still reject with the platform envelope.
				if profile.SuccessStatus == "405" {
					out.WriteString("        '405':\n          description: SSE is unsupported; empty body, Allow POST\n")
				} else {
					fmt.Fprintf(&out, "        '%s':\n          description: Direct JSON-RPC result or protocol/tool error\n          content:\n            application/json:\n              schema:\n                $ref: '#/components/schemas/%s'\n", profile.SuccessStatus, profile.ResponseSchema)
				}
				seen := map[string]bool{profile.SuccessStatus: true}
				for _, status := range append([]string{"400"}, profile.AdditionalErrors...) {
					if seen[status] {
						continue
					}
					seen[status] = true
					if status == "202" {
						out.WriteString("        '202':\n          description: Notification accepted; empty body\n")
					} else {
						fmt.Fprintf(&out, "        '%s':\n          description: JSON-RPC transport rejection or platform ingress/Feature rejection\n          content:\n            application/json:\n              schema:\n                oneOf:\n                  - $ref: '#/components/schemas/%s'\n                  - $ref: '#/components/schemas/Envelope'\n", status, profile.ResponseSchema)
					}
				}
				continue
			}
			fmt.Fprintf(&out, "        '%s':\n", profile.SuccessStatus)
			if profile.SuccessStatus == "204" {
				out.WriteString("          description: No Content\n")
			} else if profile.ResponseContentType != "" {
				fmt.Fprintf(&out, "          description: Success\n          content:\n            %s:\n              schema: { type: string, format: binary }\n", profile.ResponseContentType)
			} else {
				out.WriteString("          description: Success\n          content:\n            application/json:\n              schema:\n")
				if profile.DirectJSON {
					fmt.Fprintf(&out, "                $ref: '#/components/schemas/%s'\n", profile.ResponseSchema)
				} else if profile.ResponseSchema == "" {
					out.WriteString("                $ref: '#/components/schemas/Envelope'\n")
				} else {
					out.WriteString("                allOf:\n                  - $ref: '#/components/schemas/Envelope'\n                  - type: object\n                    properties:\n                      data:\n")
					fmt.Fprintf(&out, "                        $ref: '#/components/schemas/%s'\n", profile.ResponseSchema)
				}
			}
			out.WriteString("        '400':\n          $ref: '#/components/responses/BadRequest'\n")
			statuses := append([]string(nil), profile.AdditionalErrors...)
			if authenticated {
				statuses = append(statuses, "401", "403")
			}
			seen := map[string]bool{"400": true, profile.SuccessStatus: true}
			for _, status := range statuses {
				if seen[status] {
					continue
				}
				seen[status] = true
				switch status {
				case "202":
					out.WriteString("        '202':\n          description: Notification accepted; no response body\n")
				case "405", "406", "415":
					fmt.Fprintf(&out, "        '%s':\n          description: Unsupported transport request\n", status)
				case "401":
					out.WriteString("        '401':\n          $ref: '#/components/responses/Unauthorized'\n")
				case "429":
					out.WriteString("        '429':\n          $ref: '#/components/responses/TooManyRequests'\n")
				case "503":
					out.WriteString("        '503':\n          $ref: '#/components/responses/ServiceUnavailable'\n")
				case "403":
					out.WriteString("        '403':\n          $ref: '#/components/responses/Forbidden'\n")
				case "404":
					out.WriteString("        '404':\n          $ref: '#/components/responses/NotFound'\n")
				case "409":
					out.WriteString("        '409':\n          $ref: '#/components/responses/Conflict'\n")
				case "413":
					out.WriteString("        '413':\n          $ref: '#/components/responses/PayloadTooLarge'\n")
				}
			}
		}
	}
	out.WriteString(openAPIComponents())
	return []byte(out.String())
}

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "{" + strings.TrimPrefix(part, "*") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func openAPIPathParameters(path string) []string {
	var parameters []string
	for _, part := range strings.Split(path, "/") {
		if len(part) > 2 && strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parameters = append(parameters, strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}"))
		}
	}
	return parameters
}

func FindRepositoryRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("repository root not found from %s", start)
		}
		current = parent
	}
}

// Explicit governance metadata follows operation extraction and scope parsing;
// execution grants do not imply the generic authenticated owner rule.
func applyResourceGovernanceContract(route *RouteContract) {
	if strings.HasPrefix(route.OperationCode, "http.knowledge.") {
		route.Ownership = "current-origin-and-collection"
		route.Scope = "public-collection+current-origin"
		route.Auth = "none+current-origin-policy"
		route.Audit = "none"
		if strings.HasPrefix(route.Path, APIPrefix+"/admin/knowledge") {
			route.Scope = "knowledge.collection_inventory/all"
			route.Ownership = "knowledge-managed"
			route.Auth = "jwt+current-admin-session+permission"
			route.Audit = "none"
			if route.Method != "GET" {
				route.Auth += "+recent-mfa+browser-csrf"
				route.Audit = "required-command+outbox"
			}
		}
		if route.OperationCode == "http.knowledge.private.source.preflight" {
			route.Auth = "jwt+current-user-session+document-owner"
			route.Ownership = "current-document-owner"
			route.Scope = "self+current-document-version"
			route.Audit = "none"
		}
	}

	if strings.HasPrefix(route.OperationCode, "http.identity.integration.") {
		route.Auth = "jwt+current-admin-session+permission+recent-mfa+browser-csrf"
		route.Scope = "identity.integration_inventory/all"
		route.Ownership = "identity-managed"
		route.Audit = "required-command+outbox"
		if route.Method == "GET" {
			route.Auth = "jwt+current-admin-session+permission"
			route.Audit = "none"
		}
	}
	if route.OperationCode == "http.identity.integration_self.read" {
		route.Auth = "integration-digest+current-facts"
		route.Scope = "current-integration-self"
		route.Ownership = "current-integration-self"
		route.Audit = "none"
	}
	if route.OperationCode == "http.integration.knowledge.source.read" || route.OperationCode == "http.integration.knowledge.search" || strings.HasPrefix(route.OperationCode, "http.integration.knowledge.mcp") {
		route.Auth = "integration-digest+current-facts+whitelist+source-acl"
		route.Scope = "endpoint+public-collection+current-origin"
		route.Ownership = "current-origin-and-collection"
		route.Audit = "required-command+outbox"
	}

	switch route.OperationCode {
	case "http.identity.admin_session.login":
		route.Audit = "required-command+outbox"
		return
	case "http.identity.admin_session.refresh":
		route.Auth = "admin-refresh-cookie+csrf"
		route.Ownership = "current-admin-session"
		route.Scope = "admin-refresh-family"
		route.Audit = "required-command+outbox"
		return
	case "http.identity.admin_self.read", "http.identity.admin_self.logout", "http.identity.admin_self.mfa.enroll", "http.identity.admin_self.mfa.confirm", "http.identity.admin_self.mfa.step_up":
		route.Auth = "jwt+current-admin-session+browser-csrf"
		route.Ownership = "resource-policy"
		route.Scope = "current-admin-self"
		route.Audit = "required-command+outbox"
		if route.OperationCode == "http.identity.admin_self.read" {
			route.Auth = "jwt+current-admin-session"
			route.Audit = "none"
		}
		return
	}

	if strings.HasPrefix(route.OperationCode, "http.personal_space.owner_style_read.") {
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-owned-space+current-user"
		route.Audit = "none"
		return
	}
	switch route.OperationCode {
	case "http.personal_space.public_profile.by_id", "http.personal_space.public_profile.by_username", "http.personal_space.public_contents.by_id", "http.personal_space.public_contents.by_username":
		route.Auth = "public"
		route.Ownership = "resource-policy"
		route.Scope = "actual-public-space+current-user"
		route.Audit = "none"
	case "http.personal_space.owner_contents.read", "http.personal_space.owner_sync_status.read":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-owned-space+current-user"
		route.Audit = "none"
	case "http.personal_space.admin.summary", "http.personal_space.admin.storage":
		route.Auth = "jwt+admin-account+permission+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "current-space-aggregate+current-admin-execution"
		if route.OperationCode == "http.personal_space.admin.storage" {
			route.Scope = "actual-target-user+space+current-quota+current-admin-execution"
		}
		route.Audit = "none"
	case "http.personal_space.admin.quota":
		route.Auth = "jwt+admin-account+permission+current-mfa"
		route.Ownership = "resource-policy"
		route.Scope = "actual-target-user+current-quota+current-admin-execution"
		route.Audit = "required-command-audit"
	case "http.personal_space.admin.disable", "http.personal_space.admin.enable":
		route.Auth = "jwt+admin-account+permission+current-mfa"
		route.Ownership = "resource-policy"
		route.Scope = "actual-target-user+space+current-admin-execution"
		route.Audit = "required-command-audit"
	case "http.personal_space.owner_profile.read", "http.personal_space.owner_profile.update", "http.personal_space.owner_avatar.select", "http.personal_space.owner_avatar.upload", "http.personal_space.owner_avatar.cleanup", "http.personal_space.owner_style.apply", "http.personal_space.owner_style.rollback", "http.personal_space.owner_style.default", "http.personal_space.owner_style.html", "http.personal_space.owner_style.zip", "http.personal_space.owner_style.source", "http.personal_space.owner_style.catalog":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-owned-space+current-user"
		route.Audit = "none"
		if route.OperationCode != "http.personal_space.owner_profile.read" && route.OperationCode != "http.personal_space.owner_style.catalog" {
			route.Audit = "required-command-audit"
		}
	case "http.storage.inline_image.author_upload", "http.feature.richtext.inline_image.author_upload":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-owner-inventory-or-core-author+current-quota"
		route.Audit = "required-command-audit"
	case "http.personal_space.asset.purge":
		route.Auth = "jwt+admin-account+permission+current-mfa"
		route.Ownership = "resource-policy"
		route.Scope = "actual-user-asset+current-admin-execution+owned-object-receipt"
		route.Audit = "required-command-audit"
	case "http.personal_space.asset.governance_summary":
		route.Auth = "jwt+admin-account+permission+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "asset-governance-aggregate+current-admin-execution"
		route.Audit = "none"
	case "http.personal_space.asset.purge_preview":
		route.Auth = "jwt+admin-account+permission+current-mfa"
		route.Ownership = "resource-policy"
		route.Scope = "actual-user-asset+current-admin-execution"
		route.Audit = "none"
	case "http.personal_space.asset.quarantine", "http.personal_space.asset.restore":
		route.Auth = "admin-domain-jwt+current-session+mfa"
		route.Ownership = "resource-policy"
		route.Scope = "actual-user-asset+current-admin-execution"
		route.Audit = "required-command-audit"
	case "http.feature.richtext.asset.owner_trash", "http.feature.richtext.asset.owner_restore":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-owned-asset+reference-integrity"
		route.Audit = "required-command-audit"
	case "http.feature.richtext.attachment.copy_document":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-core-author+owned-current-document+article-asset-binding"
		route.Audit = "required-command-audit"
	case "http.feature.richtext.attachment.upload", "http.feature.richtext.attachment.bind", "http.feature.richtext.attachment.remove", "http.feature.richtext.attachment.rename", "http.feature.richtext.attachment.reorder":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "actual-core-author+article-asset-binding"
		route.Audit = "required-command-audit"
	case "http.community.event.metadata":
		route.Auth = "jwt+admin-account+current-session+permission"
		route.Ownership = "resource-policy"
		route.Scope = "event-metadata-only"
	case "http.get.api.v1.threads.id.posts":
		route.Ownership = "resource-policy"
		route.Scope = "parent-public+published-posts"
	case "http.get.api.v1.threads.id.posts.me":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "parent-public-or-current-user-owner+published-posts"
	case "http.community.thread.user_take_down", "http.community.post.user_governance_delete":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "single-board-execution-grant"
		route.Audit = "required-command-audit"
	case "http.community.thread.user_pin", "http.community.thread.user_unpin", "http.community.thread.user_lock", "http.community.thread.user_unlock":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "explicit-user-role+resource-board"
		route.Audit = "required-command-and-module-audit"
	case "http.community.moderation.current_preview", "http.community.moderation.current_preview_batch":
		route.Auth = "jwt+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "current-user-named-policy+actual-thread-and-posts"
	case "http.community.thread.admin_permissions_preview":
		route.Auth = "jwt+admin-account+permission+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "current-admin+same-command-policies+bounded-actual-threads"
	case "http.community.thread.admin_list", "http.community.thread.admin_trash_list", "http.community.thread.admin_history":
		route.Auth = "jwt+admin-account+permission+current-session"
		route.Ownership = "resource-policy"
		route.Scope = "current-admin+actual-board+metadata-only-before-count-page"
	case "http.community.thread.take_down", "http.community.post.admin_delete", "http.post.api.v1.threads.id.pin", "http.post.api.v1.threads.id.unpin", "http.post.api.v1.threads.id.lock", "http.post.api.v1.threads.id.unlock", "http.community.thread.review_approve", "http.community.thread.review_reject", "http.community.thread.trash", "http.community.thread.direct_restore", "http.community.thread.restore_trash", "http.community.thread.purge":
		route.Auth = "jwt+admin-account+permission+current-mfa"
		route.Ownership = "resource-policy"
		route.Scope = "resource-board"
		route.Audit = "required-command-audit"
	}
}
