# 当前 API 契约与兼容规则

CampusOS 的当前 HTTP 路由由模块化 Bootstrap 注册，并通过 `campusos-contracts` 生成 OpenAPI、JSON 清单和授权矩阵。合约漂移检查已进入 CI。

## 契约产物

| 产物 | 仓库路径 |
| --- | --- |
| Current OpenAPI | `docs/api/openapi-v0.6-current.yaml` |
| 路由 JSON | `docs/api/http-routes-v0.6.json` |
| 路由与授权矩阵 | `docs/api/HTTP路由与授权矩阵-v0.6.md` |
| 插件权限目录 | `docs/api/plugin-permissions-v1.json` |
| v2 插件权限目录 | `docs/api/plugin-permissions-v2.json` |

```bash
make contracts
make contracts-check
```

## 稳定性

当前 v0.11 路由仍统一标记为 `experimental`。`openapi-v0.6-current.yaml` 等文件名只为兼容既有链接保留，文件内容的版本和权限元数据由当前路由代码生成。进入 stable 前，客户端应允许新增可选字段，并同时依据 HTTP 状态、兼容顶层 `{ code, msg, data }` 和结构化 `error.code` 处理失败。删除路径、修改字段类型或扩大写入语义必须经过弃用期或新 API 版本，不能静默变化。

OpenAPI 当前保证路由的方法、认证、稳定权限 Code、Operation、模块所有者和请求体声明与代码一致。Manifest v2 的受管数据、用户授权、用户文件、目录和发布记录端点继续兼容；`Record*` Host API v2 方法仅可操作声明的系统归属集合。标记 `generic-experimental` 的动态配置、声明式 UI 和上传模型仍只提供通用对象，不能视为 stable。

生成器测试会解析 YAML、检查路径参数与分页参数只能出现一个 `parameters` 块，并要求核心 request schema、结构化错误和请求体数量达到门槛。`make contracts-check` 负责阻止生成产物漂移。

v11 新增的可靠任务接口位于 `/platform/reliability/*`，使用独立的查看、重放和保留预演 Permission Code；使用说明见 [可靠任务与 Webhook](/operations/reliable-tasks)。
