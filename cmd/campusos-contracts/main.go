package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/internal/plugin/hostapi"
	"github.com/campusos/CampusOS/internal/projectaudit"
)

type artifact struct {
	path string
	data []byte
}

func main() {
	write := flag.Bool("write", false, "write generated contracts")
	check := flag.Bool("check", false, "check committed contracts for drift")
	rootFlag := flag.String("root", ".", "repository root or a directory inside it")
	flag.Parse()
	if *write == *check {
		fmt.Fprintln(os.Stderr, "exactly one of --write or --check is required")
		os.Exit(2)
	}
	root, err := projectaudit.FindRepositoryRoot(*rootFlag)
	if err != nil {
		fatal(err)
	}
	routes, err := projectaudit.ParseServerRoutes(filepath.Join(root, "internal/server/application.go"))
	if err != nil {
		fatal(err)
	}
	routesJSON, err := projectaudit.RoutesJSON(routes)
	if err != nil {
		fatal(err)
	}
	permissionJSON, err := json.MarshalIndent(map[string]interface{}{
		"manifest_api_version": plugin.CurrentManifestAPIVersion,
		"host_api_version":     plugin.CurrentHostAPIVersion,
		"permissions":          plugin.PermissionCatalog(),
		"host_api_methods":     hostapi.PermissionCatalog(),
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	permissionJSON = append(permissionJSON, '\n')
	artifacts := []artifact{
		{filepath.Join(root, "docs/api/http-routes-v0.6.json"), routesJSON},
		{filepath.Join(root, "docs/api/HTTP路由与授权矩阵-v0.6.md"), projectaudit.RoutesMarkdown(routes)},
		{filepath.Join(root, "docs/api/openapi-v0.6-current.yaml"), projectaudit.OpenAPI(routes)},
		{filepath.Join(root, "docs/api/plugin-permissions-v1.json"), permissionJSON},
		{filepath.Join(root, "docs/api/Host-API-v1权限目录.md"), permissionMarkdown()},
	}
	for _, item := range artifacts {
		if *write {
			if err := os.MkdirAll(filepath.Dir(item.path), 0o755); err != nil {
				fatal(err)
			}
			if err := os.WriteFile(item.path, item.data, 0o644); err != nil {
				fatal(err)
			}
			fmt.Printf("wrote %s\n", item.path)
			continue
		}
		current, err := os.ReadFile(item.path)
		if err != nil {
			fatal(fmt.Errorf("contract missing: %s: %w", item.path, err))
		}
		if !bytes.Equal(current, item.data) {
			fatal(fmt.Errorf("contract drift: %s (run go run ./cmd/campusos-contracts --write)", item.path))
		}
		fmt.Printf("ok %s\n", item.path)
	}
}

func permissionMarkdown() []byte {
	var out strings.Builder
	out.WriteString("# CampusOS Host API v1 权限目录\n\n")
	out.WriteString("> 由 `go run ./cmd/campusos-contracts --write` 从代码生成。Manifest 默认无权限；调用 Host API 时同时校验插件身份和声明权限。\n\n")
	out.WriteString("## Host API 方法\n\n| 方法 | Manifest 权限 |\n| --- | --- |\n")
	for _, item := range hostapi.PermissionCatalog() {
		fmt.Fprintf(&out, "| `%s` | `%s/%s` |\n", item.Method, item.Resource, item.Action)
	}
	out.WriteString("\n## 权限说明\n\n| Resource | Action | Risk | Description |\n| --- | --- | --- | --- |\n")
	for _, item := range plugin.PermissionCatalog() {
		fmt.Fprintf(&out, "| `%s` | `%s` | `%s` | %s |\n", item.Resource, item.Action, item.Risk, item.Description)
	}
	out.WriteString("\n系统级插件仍需在重启后应用生命周期变更；用户级插件可以受控热加载。权限不会因为 Runtime 或热加载而自动扩大。\n")
	return []byte(out.String())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
