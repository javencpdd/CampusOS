# 插件兼容矩阵

| CampusOS | Manifest API | Host API | Go SDK | Runtime 模板 |
| --- | --- | --- | --- | --- |
| `0.6.x` | `campusos.plugin/v1` | `v1` | `v0.6` | Built-in/gRPC/Wasm `0.1.x` |

Manifest 应显式声明：

```yaml
api_version: campusos.plugin/v1
host_api_version: v1
compatibility:
  campusos: ">=0.6.0 <0.7.0"
  host_api: "v1"
  sdk_go: "v0.6"
```

旧包未写版本字段时按 v1 读取，便于 v0.3-v0.5 包迁移；写入未知版本会被拒绝。新增权限会提高预检风险并要求管理员重新确认。破坏 Host API 请求/响应或权限语义必须发布新版本，不在 v1 中静默替换。

TypeScript SDK 暂不复制 Go Host API。浏览器插件不能持有 Host token，前端调用继续使用 Public HTTP API；待字段级 OpenAPI schema 稳定后，从 OpenAPI 生成只覆盖 Public API 的 TypeScript client。
