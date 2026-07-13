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

	platformroute "github.com/campusos/CampusOS/internal/platform/route"
)

const APIPrefix = "/api/v1"

var (
	routePattern      = regexp.MustCompile(`^\s*(public|authenticated|admin)(?:\.Permission\("([^"]+)",\s*"([^"]+)"\))?\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)",(.*)$`)
	permissionPattern = regexp.MustCompile(`RequirePermission\([^,]+,\s*"([^"]+)",\s*"([^"]+)"\)`)
	selectorPattern   = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)\.([A-Za-z][A-Za-z0-9_]*)`)
)

// RouteContract is the machine-readable authorization record for one Gin route.
type RouteContract struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Handler     string `json:"handler"`
	ModuleOwner string `json:"module_owner"`
	Audience    string `json:"audience"`
	Auth        string `json:"auth"`
	Permission  string `json:"permission,omitempty"`
	Ownership   string `json:"ownership"`
	Scope       string `json:"scope"`
	Audit       string `json:"audit"`
	Stability   string `json:"stability"`
	SourceLine  int    `json:"-"`
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
		group, permissionResource, permissionAction := matches[1], matches[2], matches[3]
		method, routePath, arguments := matches[4], matches[5], matches[6]
		selectors := selectorPattern.FindAllStringSubmatch(arguments, -1)
		if len(selectors) == 0 {
			return nil, fmt.Errorf("route at line %d has no handler selector", lineNumber)
		}
		handler := selectors[len(selectors)-1][1] + "." + selectors[len(selectors)-1][2]
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
		if permissionResource != "" && permissionAction != "" {
			route.Permission = permissionResource + ":" + permissionAction
		}
		if permission := permissionPattern.FindStringSubmatch(arguments); len(permission) > 0 {
			route.Permission = permission[1] + ":" + permission[2]
		}
		if group == "admin" && route.Permission == "" {
			return nil, fmt.Errorf("admin route %s %s has no explicit permission", method, route.Path)
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

// ValidateRouteDescriptors registers the source-derived transport contract in
// the platform Route Registry before generated API artifacts are accepted.
func ValidateRouteDescriptors(routes []RouteContract) error {
	registry := platformroute.NewRegistry()
	for _, item := range routes {
		if strings.HasPrefix(item.ModuleOwner, "unowned:") || item.ModuleOwner == "" || item.ModuleOwner == "transport.httpapi" {
			return fmt.Errorf("route %s %s has no business module owner", item.Method, item.Path)
		}
		descriptor := platformroute.Descriptor{
			ID:         "httpapi." + strings.ToLower(item.Method) + "." + routeID(item.Path),
			Owner:      item.ModuleOwner,
			Method:     item.Method,
			Path:       item.Path,
			Audience:   platformroute.Audience(item.Audience),
			Auth:       item.Auth,
			Permission: item.Permission,
			FeatureID:  featureForRoute(item.Path),
			Audit:      item.Audit,
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
	case strings.HasPrefix(path, APIPrefix+"/moderation"):
		return "core.moderation"
	case strings.HasPrefix(path, APIPrefix+"/auth"), strings.HasPrefix(path, APIPrefix+"/users"), strings.HasPrefix(path, APIPrefix+"/roles"), path == APIPrefix+"/health":
		return "core.identity"
	case strings.HasPrefix(path, APIPrefix+"/categories"), strings.HasPrefix(path, APIPrefix+"/threads"), path == APIPrefix+"/events", strings.HasPrefix(path, APIPrefix+"/admin/threads"):
		return "core.community"
	case strings.HasPrefix(path, APIPrefix+"/spaces"), strings.HasPrefix(path, APIPrefix+"/space"), strings.HasPrefix(path, APIPrefix+"/u/"):
		return "feature.personal-space"
	case strings.HasPrefix(path, APIPrefix+"/richtext"):
		return "feature.controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/schedule"):
		return "feature.personal-schedule"
	case strings.HasPrefix(path, APIPrefix+"/home"), strings.HasPrefix(path, APIPrefix+"/web-themes"):
		return "feature.appearance"
	case strings.HasPrefix(path, APIPrefix+"/plugins"), strings.HasPrefix(path, APIPrefix+"/plugin-packages"), strings.HasPrefix(path, APIPrefix+"/extensions"), strings.HasPrefix(path, APIPrefix+"/ui/"):
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
	case strings.HasPrefix(path, APIPrefix+"/integrations"), strings.HasPrefix(path, APIPrefix+"/metrics"):
		return "feature.integration-overview"
	default:
		return "unowned:" + handler
	}
}

func routeID(path string) string {
	replacer := strings.NewReplacer("/", ".", ":", "", "*", "wildcard", "-", "_")
	return strings.Trim(replacer.Replace(path), ".")
}

func featureForRoute(path string) string {
	switch {
	case strings.HasPrefix(path, APIPrefix+"/space"), strings.HasPrefix(path, APIPrefix+"/u/"):
		return "personal-space"
	case strings.HasPrefix(path, APIPrefix+"/richtext"):
		return "controlled-richtext-article"
	case strings.HasPrefix(path, APIPrefix+"/schedule"):
		return "personal-schedule"
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
		route.Auth = "jwt+permission"
		route.Ownership = "none"
		route.Scope = "global"
		route.Audit = "request-log"
	}
	if method == "GET" && route.Audit == "request-log" {
		route.Audit = "request-log-read"
	}
}

func RoutesJSON(routes []RouteContract) ([]byte, error) {
	payload := struct {
		Version string          `json:"version"`
		Routes  []RouteContract `json:"routes"`
	}{Version: "v0.6", Routes: routes}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func RoutesMarkdown(routes []RouteContract) []byte {
	var out strings.Builder
	out.WriteString("# CampusOS HTTP 路由与授权矩阵 v0.6\n\n")
	out.WriteString("> 本文档由 `go run ./cmd/campusos-contracts --write` 根据 `internal/transport/httpapi/router.go` 生成，请勿手工编辑。\n\n")
	out.WriteString("当前接口均标记为 `experimental`；进入 stable 前不得承诺无弃用期的兼容性。`handler-enforced` 表示资源归属和字段过滤由对应 handler/service 负责。\n\n")
	out.WriteString("| Method | Path | Module | Handler | Auth | Permission | Ownership | Scope | Audit |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, route := range routes {
		permission := route.Permission
		if permission == "" {
			permission = "-"
		}
		fmt.Fprintf(&out, "| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` |\n",
			route.Method, route.Path, route.ModuleOwner, route.Handler, route.Auth, permission, route.Ownership, route.Scope, route.Audit)
	}
	return []byte(out.String())
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
	out.WriteString("openapi: 3.0.3\ninfo:\n  title: CampusOS Current HTTP API\n  version: 0.6.9-experimental\n  description: Generated route, authorization, request-body and core field contract. Operations using GenericObject remain experimental.\nservers:\n  - url: http://localhost:8080\npaths:\n")
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
			if route.Permission != "" {
				fmt.Fprintf(&out, "      x-campusos-permission: %s\n", route.Permission)
			}
			if route.Auth != "none" {
				out.WriteString("      security:\n        - bearerAuth: []\n")
			}
			parameters := openAPIPathParameters(path)
			if len(parameters) > 0 || profile.Paginated {
				out.WriteString("      parameters:\n")
				for _, parameter := range parameters {
					fmt.Fprintf(&out, "        - name: %s\n          in: path\n          required: true\n          schema:\n            type: string\n", parameter)
				}
				if profile.Paginated {
					out.WriteString("        - name: page\n          in: query\n          schema: { type: integer, minimum: 1, default: 1 }\n")
					out.WriteString("        - name: page_size\n          in: query\n          schema: { type: integer, minimum: 1, maximum: 100, default: 20 }\n")
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
			fmt.Fprintf(&out, "        '%s':\n", profile.SuccessStatus)
			if profile.SuccessStatus == "204" {
				out.WriteString("          description: No Content\n")
			} else {
				out.WriteString("          description: Success\n          content:\n            application/json:\n              schema:\n")
				if profile.ResponseSchema == "" {
					out.WriteString("                $ref: '#/components/schemas/Envelope'\n")
				} else {
					out.WriteString("                allOf:\n                  - $ref: '#/components/schemas/Envelope'\n                  - type: object\n                    properties:\n                      data:\n")
					fmt.Fprintf(&out, "                        $ref: '#/components/schemas/%s'\n", profile.ResponseSchema)
				}
			}
			out.WriteString("        '400':\n          $ref: '#/components/responses/BadRequest'\n")
			if route.Auth != "none" {
				out.WriteString("        '401':\n          $ref: '#/components/responses/Unauthorized'\n")
			}
			if route.Auth != "none" {
				out.WriteString("        '403':\n          $ref: '#/components/responses/Forbidden'\n")
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
