# 接口约定

## 基础地址

本地开发：

```text
http://localhost:8080/api/v1
```

所有当前 HTTP 接口都注册在 `/api/v1` 下。旧文件 `docs/api/openapi-v0.3-pre.yaml` 只是历史基线，不是当前完整 OpenAPI。

## 请求格式

- 普通读写使用 `application/json`。
- 文件上传使用 `multipart/form-data`。
- 下载和日志流会使用文件响应或 SSE。
- 需要登录的接口使用 Bearer JWT。

```http
Authorization: Bearer <access_token>
Content-Type: application/json
```

## 响应包络

成功响应通常使用：

```json
{
  "code": 0,
  "msg": "success",
  "data": {},
  "request_id": "8b73..."
}
```

列表响应的 `data` 中通常包含 `items` 和 `pagination`：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [],
    "pagination": {
      "page": 1,
      "page_size": 20,
      "total": 0,
      "total_pages": 0
    }
  }
}
```

错误响应示例：

```json
{
  "code": 20004,
  "msg": "permission denied",
  "error": {
    "code": "permission.denied",
    "message": "permission denied",
    "details": {},
    "request_id": "8b73...",
    "retryable": false
  },
  "request_id": "8b73..."
}
```

顶层 `code` 和 `msg` 为旧客户端保留。新客户端应同时判断 HTTP status 和 `error.code`，不要依赖 `msg` 文本做程序分支；`request_id` 用于关联服务端日志，`retryable` 表示同一请求是否适合稍后重试。

`page` 从 1 开始，`page_size` 的允许范围为 1 到 100。无法解析、零、负数或超上限都返回 `400 request.invalid`，客户端应修正参数，不要依赖服务端静默归一化。

## 常见状态码

| HTTP | 含义 |
| --- | --- |
| `200` | 查询或更新成功。 |
| `201` | 创建成功。 |
| `204` | 删除成功且没有响应体。 |
| `400` | 参数、请求体或业务输入无效。 |
| `401` | 未登录、token 无效或已过期。 |
| `403` | 已登录，但权限、所有权或作用域不满足。 |
| `404` | 目标资源不存在或当前主体不可见。 |
| `409` | 唯一性冲突或资源状态冲突。 |
| `503` | Built-in Feature 被 Gate 停用、External Plugin 不可用或依赖服务临时失败。 |

## 接口分组

| 分组 | 常见路径 | 认证 |
| --- | --- | --- |
| 认证 | `/auth/registration/*`、`/auth/register`、`/auth/login`、`/auth/me` | 注册 Challenge/校验/创建公开；`me` 需要 JWT。 |
| 用户 | `/users/*` | 公开读取与受保护管理接口并存。 |
| 社区 | `/categories`、`/threads`、`/posts` | 公开读取，写入需要 JWT。 |
| 版主 | `/moderation/*` | JWT + 动作权限 + category scope。 |
| 个人空间 | `/spaces/me/*`、`/u/:username` | 本人写入，公开主页按可见性读取。 |
| 课表 | `/schedule/me/*` | 当前用户。 |
| 富文本 | `/richtext/*` | 草稿和写入属于作者，治理属于管理员。 |
| 插件 | `/plugins/*`、`/plugin-packages/*` | 管理员权限。 |
| 集成 | `/webhooks/*`、`/mcp/*`、`/messages/*`、`/ai/*` | 管理员权限。 |

## 当前契约来源

当前 OpenAPI 已由实际路由生成。接口真相按以下顺序确认：

1. `internal/transport/httpapi/router.go` 的实际路由与 owned route registry。
2. `docs/api/openapi-v0.6-current.yaml` 的路由、认证、权限和字段级 schema。
3. 对应 handler 和请求/响应结构；标记为 `generic-experimental` 的动态对象以这里为准。
4. `web/src/api/index.ts`、`admin/src/api/index.ts` 和本文。

新增或修改公开接口时，应同时更新代码、调用方、文档和负向权限测试。
