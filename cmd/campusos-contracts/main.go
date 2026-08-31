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
	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/observability"
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
	routes, err := projectaudit.ParseServerRoutes(filepath.Join(root, "internal/transport/httpapi/router.go"))
	if err != nil {
		fatal(err)
	}
	routesJSON, err := projectaudit.RoutesJSON(routes)
	if err != nil {
		fatal(err)
	}
	permissionJSON, err := json.MarshalIndent(map[string]interface{}{
		"manifest_api_version":  plugin.CurrentManifestAPIVersion,
		"host_api_version":      plugin.CurrentHostAPIVersion,
		"manifest_api_versions": []string{plugin.ManifestAPIVersionV1, plugin.ManifestAPIVersionV2},
		"host_api_versions":     []string{plugin.HostAPIVersionV1, plugin.HostAPIVersionV2},
		"permissions":           plugin.PermissionCatalog(),
		"host_api_methods":      hostapi.PermissionCatalog(),
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	permissionJSON = append(permissionJSON, '\n')
	errorCatalog := apperror.Catalog()
	if err := apperror.ValidateCatalog(errorCatalog); err != nil {
		fatal(err)
	}
	errorJSON, err := json.MarshalIndent(map[string]interface{}{
		"contract_version": apperror.CatalogVersion,
		"errors":           errorCatalog,
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	errorJSON = append(errorJSON, '\n')
	metricCatalog := observability.MetricCatalog()
	if err := observability.ValidateMetricCatalog(metricCatalog); err != nil {
		fatal(err)
	}
	metricsJSON, err := json.MarshalIndent(map[string]interface{}{
		"contract_version": "campusos.metrics/v1",
		"metrics":          metricCatalog,
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	metricsJSON = append(metricsJSON, '\n')
	artifacts := []artifact{
		{filepath.Join(root, "docs/api/http-routes-v0.6.json"), routesJSON},
		{filepath.Join(root, "docs/api/HTTP路由与授权矩阵-v0.6.md"), projectaudit.RoutesMarkdown(routes)},
		{filepath.Join(root, "docs/api/openapi-v0.6-current.yaml"), projectaudit.OpenAPI(routes)},
		{filepath.Join(root, "docs/api/plugin-permissions-v1.json"), permissionJSON},
		{filepath.Join(root, "docs/api/plugin-permissions-v2.json"), permissionJSON},
		{filepath.Join(root, "docs/api/Host-API-v1权限目录.md"), permissionMarkdown()},
		{filepath.Join(root, "docs/api/error-catalog-v0.13.json"), errorJSON},
		{filepath.Join(root, "docs/api/v0.13统一错误合同.md"), errorCatalogMarkdown(errorCatalog)},
		{filepath.Join(root, "docs/api/metrics-catalog-v0.13.json"), metricsJSON},
		{filepath.Join(root, "docs/api/v0.13可观测性指标目录.md"), metricsCatalogMarkdown(metricCatalog)},
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

func metricsCatalogMarkdown(items []observability.MetricDescriptor) []byte {
	var out strings.Builder
	out.WriteString("# CampusOS v0.13 可观测性指标目录\n\n")
	out.WriteString("> 由 `go run ./cmd/campusos-contracts --write` 从 `pkg/observability` 生成。指标名、类型、单位、标签和 series 上限属于稳定合同。\n\n")
	out.WriteString("| 指标 | 类型 | 单位 | 标签白名单 | Series 上限 | 说明 |\n| --- | --- | --- | --- | ---: | --- |\n")
	for _, item := range items {
		labels := "无"
		if len(item.LabelNames) > 0 {
			labels = "`" + strings.Join(item.LabelNames, "`, `") + "`"
		}
		fmt.Fprintf(&out, "| `%s` | `%s` | `%s` | %s | %d | %s |\n", item.Name, item.Type, item.Unit, labels, item.MaximumSeries, item.Help)
	}
	out.WriteString("\n禁止把用户 ID、邮箱、IP、帖子 ID、完整 URL、错误文本、Token、Secret 或任意配置值写入标签。External Plugin 不能直接取得 Meter。\n")
	return []byte(out.String())
}

func errorCatalogMarkdown(items []apperror.Descriptor) []byte {
	var out strings.Builder
	out.WriteString("# CampusOS v0.13 统一错误合同\n\n")
	out.WriteString("> 由 `go run ./cmd/campusos-contracts --write` 从 `pkg/apperror` 生成。旧顶层 `code`、`msg`、`data`、`request_id` 保持兼容；新客户端应优先读取 `error.code`。\n\n")
	out.WriteString("## 响应包络\n\n```json\n{\n  \"code\": 10001,\n  \"msg\": \"invalid request\",\n  \"error\": {\n    \"code\": \"request.invalid\",\n    \"message\": \"invalid request\",\n    \"details\": {},\n    \"request_id\": \"...\",\n    \"retryable\": false\n  },\n  \"request_id\": \"...\"\n}\n```\n\n")
	out.WriteString("未知错误与 panic 只能返回 `internal.error`，原始 cause 只进入服务端日志。客户端不得依赖内部错误文本。\n\n")
	out.WriteString("## 错误目录\n\n| Machine code | Owner | HTTP | 旧数字码 | Retryable | 安全文案 |\n| --- | --- | ---: | ---: | --- | --- |\n")
	for _, item := range items {
		fmt.Fprintf(&out, "| `%s` | `%s` | %d | %d | %t | %s |\n", item.MachineCode, item.Owner, item.HTTPStatus, item.LegacyCode, item.Retryable, item.Message)
	}
	out.WriteString("\n`internal.error` 在兼容期允许由明确的不可用路径以 HTTP 503 返回；目录中的 500 是规范状态。该覆盖必须通过 AppError 显式声明，不能由 Handler 任意改写。\n")
	return []byte(out.String())
}

func permissionMarkdown() []byte {
	var out strings.Builder
	out.WriteString("# CampusOS Host API 权限目录（v1 / v2）\n\n")
	out.WriteString("> 由 `go run ./cmd/campusos-contracts --write` 从代码生成。Manifest 默认无权限；调用 Host API 时同时校验插件身份和声明权限。`Record*` 方法只接受 `campusos.plugin/v2`、`host_api_version: v2` 的系统归属集合。\n\n")
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
