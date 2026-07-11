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

v0.6 当前路由统一标记为 `experimental`。进入 stable 前，客户端应允许新增可选字段，并同时依据 HTTP 状态、兼容顶层 `{ code, msg, data }` 和结构化 `error.code` 处理失败。删除路径、修改字段类型或扩大写入语义必须经过弃用期或新 API 版本，不能静默变化。

OpenAPI 当前保证 141 条路由的方法、认证、显式权限和请求体声明与代码一致。核心身份、社区、空间、课表、富文本、角色/版主和风格包选择请求已有字段级 schema；标记 `generic-experimental` 的动态配置和上传模型仍只提供通用对象，不能视为 stable。

生成器测试会解析 YAML、检查路径参数与分页参数只能出现一个 `parameters` 块，并要求核心 request schema、结构化错误和请求体数量达到门槛。`make contracts-check` 负责阻止生成产物漂移。
