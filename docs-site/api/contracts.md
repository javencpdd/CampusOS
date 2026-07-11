# 当前 API 契约与兼容规则

CampusOS v0.6 的当前 HTTP 路由由 `internal/server/server.go` 注册，并通过 `campusos-contracts` 生成 OpenAPI、JSON 清单和授权矩阵。合约漂移检查已进入 CI。

## 契约产物

| 产物 | 仓库路径 |
| --- | --- |
| Current OpenAPI | `docs/api/openapi-v0.6-current.yaml` |
| 路由 JSON | `docs/api/http-routes-v0.6.json` |
| 路由与授权矩阵 | `docs/api/HTTP路由与授权矩阵-v0.6.md` |
| 插件权限目录 | `docs/api/plugin-permissions-v1.json` |

```bash
make contracts
make contracts-check
```

## 稳定性

v0.6 当前路由统一标记为 `experimental`。进入 stable 前，客户端应允许新增可选字段，并同时依据 HTTP 状态和 `{ code, msg, data }` 包络处理失败。删除路径、修改字段类型或扩大写入语义必须经过弃用期或新 API 版本，不能静默变化。

OpenAPI 当前保证路由、方法、认证和显式权限与代码一致。部分业务请求/响应仍只提供通用 Envelope，不能把缺少字段级 schema 的接口视为 stable。
