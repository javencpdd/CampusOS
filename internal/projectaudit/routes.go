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
)

const APIPrefix = "/api/v1"

var (
	routePattern      = regexp.MustCompile(`^\s*(public|authenticated|admin)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)",(.*)$`)
	permissionPattern = regexp.MustCompile(`RequirePermission\(permSvc,\s*"([^"]+)",\s*"([^"]+)"\)`)
	selectorPattern   = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)\.([A-Za-z][A-Za-z0-9_]*)`)
)

// RouteContract is the machine-readable authorization record for one Gin route.
type RouteContract struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Handler    string `json:"handler"`
	Audience   string `json:"audience"`
	Auth       string `json:"auth"`
	Permission string `json:"permission,omitempty"`
	Ownership  string `json:"ownership"`
	Scope      string `json:"scope"`
	Audit      string `json:"audit"`
	Stability  string `json:"stability"`
	SourceLine int    `json:"-"`
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
		group, method, routePath, arguments := matches[1], matches[2], matches[3], matches[4]
		selectors := selectorPattern.FindAllStringSubmatch(arguments, -1)
		if len(selectors) == 0 {
			return nil, fmt.Errorf("route at line %d has no handler selector", lineNumber)
		}
		handler := selectors[len(selectors)-1][1] + "." + selectors[len(selectors)-1][2]
		route := RouteContract{
			Method:     method,
			Path:       APIPrefix + routePath,
			Handler:    handler,
			Audience:   group,
			Stability:  "experimental",
			SourceLine: lineNumber,
		}
		applyAuthorization(&route, group, method)
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
	return routes, nil
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
	out.WriteString("> 本文档由 `go run ./cmd/campusos-contracts --write` 根据 `internal/server/server.go` 生成，请勿手工编辑。\n\n")
	out.WriteString("当前接口均标记为 `experimental`；进入 stable 前不得承诺无弃用期的兼容性。`handler-enforced` 表示资源归属和字段过滤由对应 handler/service 负责。\n\n")
	out.WriteString("| Method | Path | Handler | Auth | Permission | Ownership | Scope | Audit |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, route := range routes {
		permission := route.Permission
		if permission == "" {
			permission = "-"
		}
		fmt.Fprintf(&out, "| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` |\n",
			route.Method, route.Path, route.Handler, route.Auth, permission, route.Ownership, route.Scope, route.Audit)
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
	out.WriteString("openapi: 3.0.3\ninfo:\n  title: CampusOS Current HTTP API\n  version: 0.6.0-experimental\n  description: Generated route-level contract. Handler-specific schemas remain experimental.\nservers:\n  - url: http://localhost:8080\npaths:\n")
	for _, path := range paths {
		fmt.Fprintf(&out, "  %s:\n", path)
		sort.Slice(grouped[path], func(i, j int) bool { return grouped[path][i].Method < grouped[path][j].Method })
		for _, route := range grouped[path] {
			fmt.Fprintf(&out, "    %s:\n", strings.ToLower(route.Method))
			fmt.Fprintf(&out, "      operationId: %s\n", strings.ReplaceAll(route.Handler, ".", "_"))
			fmt.Fprintf(&out, "      summary: %s %s\n", route.Method, route.Path)
			fmt.Fprintf(&out, "      tags: [%s]\n", route.Audience)
			fmt.Fprintf(&out, "      x-campusos-stability: %s\n", route.Stability)
			fmt.Fprintf(&out, "      x-campusos-ownership: %s\n", route.Ownership)
			fmt.Fprintf(&out, "      x-campusos-scope: %s\n", route.Scope)
			if route.Permission != "" {
				fmt.Fprintf(&out, "      x-campusos-permission: %s\n", route.Permission)
			}
			if route.Auth != "none" {
				out.WriteString("      security:\n        - bearerAuth: []\n")
			}
			parameters := openAPIPathParameters(path)
			if len(parameters) > 0 {
				out.WriteString("      parameters:\n")
				for _, parameter := range parameters {
					fmt.Fprintf(&out, "        - name: %s\n          in: path\n          required: true\n          schema:\n            type: string\n", parameter)
				}
			}
			out.WriteString("      responses:\n        '200':\n          description: Success\n          content:\n            application/json:\n              schema:\n                $ref: '#/components/schemas/Envelope'\n")
			if route.Auth != "none" {
				out.WriteString("        '401':\n          $ref: '#/components/responses/Unauthorized'\n")
			}
			if route.Permission != "" {
				out.WriteString("        '403':\n          $ref: '#/components/responses/Forbidden'\n")
			}
		}
	}
	out.WriteString("components:\n  securitySchemes:\n    bearerAuth:\n      type: http\n      scheme: bearer\n      bearerFormat: JWT\n  schemas:\n    Envelope:\n      type: object\n      required: [code, msg]\n      properties:\n        code:\n          type: integer\n        msg:\n          type: string\n        data: {}\n    Error:\n      type: object\n      required: [code, msg]\n      properties:\n        code:\n          type: integer\n        msg:\n          type: string\n  responses:\n    Unauthorized:\n      description: Missing or invalid authentication\n      content:\n        application/json:\n          schema:\n            $ref: '#/components/schemas/Error'\n    Forbidden:\n      description: Authenticated subject lacks the required permission or scope\n      content:\n        application/json:\n          schema:\n            $ref: '#/components/schemas/Error'\n")
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
