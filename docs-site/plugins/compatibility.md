# 插件兼容矩阵

| CampusOS | Manifest API | Host API | Go SDK 标签 | Runtime 模板 |
| --- | --- | --- | --- | --- |
| `0.10.x` | `campusos.plugin/v1`、`campusos.plugin/v2` | `v1`、`v2` | `v0.10` | Wasm、受管进程（`grpc` 兼容名）与 legacy builtin 映射 |

Manifest 应显式声明：

```yaml
api_version: campusos.plugin/v1
host_api_version: v1
compatibility:
  campusos: ">=0.6.0 <0.11.0"
  host_api: "v1"
  sdk_go: "v0.10"
```

旧包未写版本字段时按 v1 读取，便于 v0.3-v0.5 包迁移；写入未知版本会被拒绝。v2 必须配合 Host API v2，并用于受管数据、用户授权、文件和发布治理。新增权限会提高预检风险并要求管理员重新确认。破坏 Host API 请求/响应或权限语义必须发布新版本，不能在 v1/v2 中静默替换。

当前 `runtime: grpc` 是进程 Runtime 的历史名称，Extension/Event 使用显式 loopback HTTP 合同。标准 protobuf gRPC 插件协议属于未来独立协议版本。

TypeScript SDK 暂不复制 Go Host API。浏览器插件不能持有 Host token，前端调用继续使用 Public HTTP API；待字段级 OpenAPI schema 稳定后，从 OpenAPI 生成只覆盖 Public API 的 TypeScript client。
