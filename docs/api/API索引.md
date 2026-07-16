# CampusOS API 索引

> 基础地址：`http://localhost:8080/api/v1`
> 当前实现基线：`v0.11.0`

## 1. 契约状态

CampusOS 通过 Gin 暴露带版本前缀的 HTTP 路由。当前路由级权威契约是 [openapi-v0.6-current.yaml](openapi-v0.6-current.yaml)，机器清单是 [http-routes-v0.6.json](http-routes-v0.6.json)，完整授权矩阵见 [HTTP 路由与授权矩阵](HTTP路由与授权矩阵-v0.6.md)。这些兼容文件名不再表示实现版本；内容的 `version` 为 v11，并由真实路由代码生成和检查漂移。

当前 OpenAPI 已覆盖 method/path、认证、显式权限、请求体和通用响应包络。注册登录、用户资料、帖子、回复、版块、个人空间、课表、富文本、角色/版主和风格包选择等核心请求使用字段级 schema；动态插件配置、集成配置和部分上传接口使用 `GenericObject` 或 `MultipartRequest`，继续标记 `generic-experimental`。历史 [openapi-v0.3-pre.yaml](openapi-v0.3-pre.yaml) 只作旧版本参考。

错误响应在保留旧 `{ code, msg, data }` 字段的同时增加 `error.code`、`error.message`、`error.details`、`error.request_id` 和 `error.retryable`。成功响应也可携带顶层 `request_id`。列表接口的 `page` 必须大于等于 1，`page_size` 必须在 1 到 100 之间，非法值返回 `400 request.invalid`，不会静默改成默认值。

公开 `/users` 与 `/users/:id` 只返回公开资料，不返回邮箱；`/auth/me` 和登录响应仍向本人返回账号资料。`PUT /users/:id` 只允许本人修改 `nickname`、`bio`、`avatar`，未知字段和跨用户写入分别返回 400 与 403。

## 2. 路由分组

| 分组 | 常见路径 | 说明 |
| --- | --- | --- |
| 认证与身份 | `/auth/*`、`/users/*` | 注册、登录、当前用户、资料和 RBAC 保护的管理操作。 |
| 社区 | `/categories`、`/threads`、`/posts` | 版块、默认标签、帖子/回复流程、可见性和内容治理。 |
| 版主管理 | `/moderation/*` | 版块作用域分配、当前用户可执行动作、置顶、锁定和删除回复。 |
| 个人空间 | `/spaces/me/*`、`/u/:username` | 主页设置、内容同步、头像、风格包和公开主页。 |
| 个人课表 | `/schedule/me/*` | 当前或指定学期课表、已保存课表列表、选择/新建、保存和导入。 |
| 富文本文章 | `/richtext/articles/*` | 草稿、上传、预览、发布、详情、作者操作和管理端治理。 |
| 插件 | `/plugins/*`、`/plugin-packages/*` | 列表、配置、生命周期、插件包预检/导入/导出和审计。 |
| 插件中心 | `/plugin-market/*` | 用户目录、明确授权、受管记录/文件、导出删除、管理员目录、请求和发布记录。 |
| 插件前端运行时 | `/ui/runtime-manifest`、`/ui/events` | 当前主体可见 UI 合同、单调 Revision 与 SSE 变更通知。 |
| 插件业务 Gateway | `/extensions/:plugin/*path` | JWT、权限、状态、健康、大小、超时、Trace、审计和 Runtime 分发。 |
| 用户前台系统主题 | `/web-themes/*` | 公开列出管理员提供且筛查通过的 `target:web` 风格包、读取运行包和声明图片资源。 |
| 集成 | `/webhooks/*`、`/mcp/*`、`/message/*`、`/ai/*` | Webhook、内部 MCP-like 工具、Message local adapter 和 AI Gateway。 |
| 可靠任务 | `/platform/reliability/*` | 持久事件、消费尝试、Worker、可恢复操作、命令关联审计、兼容遥测、dry-run 与受控重放。 |
| 运维 | `/health`、`/metrics`、`/platform-logs/*` | 健康、最小指标和管理员日志流。 |

不同接口的认证和权限要求不同。客户端不能根据 UI 推断授权，应处理 HTTP 失败和 CampusOS 响应包络。

## 3. 个人课表 API

每个学期互相独立。当前用户接口如下：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/schedule/me` | 返回活跃学期课表。可同时提供 `term_year` 和 `semester` 查询参数，读取一个已有学期但不改变活跃选择。 |
| `GET` | `/schedule/me/terms` | 列出已保存的学期 JSON，并标记活跃课表。 |
| `POST` | `/schedule/me/terms/activate` | 使用 `term_year` 和 `semester` 选择已有课表或创建空课表。 |
| `PUT` | `/schedule/me` | 保存到其 `term_year`、`semester` 对应 JSON，并将其设为活跃课表。 |
| `POST` | `/schedule/me/import` | multipart 导入。必须提供 `file`、`term_year`、`semester`；`replace` 控制当前学期内替换或合并。 |

API 请求应使用 `spring` 或 `fall` 作为 `semester`。服务为兼容旧数据可能接受别名，但客户端应发送标准值。

## 4. 角色与权限管理 API

v10 使用稳定 Permission Code。旧的 `resource:action` 仍作为兼容映射保留，但客户端和新代码应使用以下 Code：

| 方法 | 路径 | Permission Code | 说明 |
| --- | --- | --- | --- |
| `GET` | `/roles`、`/permissions`、`/roles/:id/permissions` | `identity.role.read` | 读取角色、权限目录和角色权限矩阵。 |
| `GET` | `/authorization-audits` | `identity.role.read_audit` | 读取结构化授权记录。 |
| `POST` | `/roles` | `identity.role.create` | 创建自定义角色；系统角色不能由此改写。 |
| `PUT` | `/roles/:id/permissions` | `identity.role.update_permissions` | 更新自定义角色权限；操作者不能授予自己不拥有的 Code。 |
| `GET` | `/users/:id/roles` | `identity.role.read` | 读取目标用户有效角色，包含隐式基础 `member`。 |
| `POST` | `/users/:id/roles` | `identity.role.assign` | 分配额外全局角色；`moderator` 必须通过版主管理指定板块。 |
| `DELETE` | `/users/:id/roles` | `identity.role.revoke` | 撤销额外角色；拒绝自我操作并以原子门禁保留至少一个全局管理员。 |

HTTP Handler 缺少已认证操作者上下文时直接返回 `401`，不会回退到受信任内部调用。`member`、`guest` 是保护角色；系统角色矩阵只读，版主只能使用 category 作用域。完整设计见 [v10 权限管理设计与使用入门](../help/系统设计相关/v10权限管理设计与使用入门.md)。

## 5. 版主管理 API

Moderation Core 始终保留权限策略、板块作用域、审计和数据完整性。历史 `category-moderation` 兼容开关只控制用户侧置顶、锁定和删除回复入口；整体启停在重启 API 后生效，`allow_pin`、`allow_lock`、`allow_delete_post` 动作配置保存后立即生效。功能停用不会删除版主范围或审计数据。

| 方法 | 路径 | 所需权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/moderation/status` | 已登录 | 返回插件当前运行状态和动作开关。 |
| `GET` | `/moderation/me?thread_id=<id>` | 已登录 | 后端读取主题版块，返回当前用户对该主题可执行的治理动作。 |
| `GET` | `/moderation/admin/moderators` | `role:read` | 列出所有版主及其已分配版块。 |
| `GET` | `/moderation/admin/moderators/:user_id` | `role:read` | 读取一个用户的版主版块。 |
| `PUT` | `/moderation/admin/moderators/:user_id` | `role:assign` | 请求体为 `{ "category_ids": ["..."] }`；空数组撤销该用户全部版主作用域。 |
| `POST` | `/moderation/threads/:id/pin`、`/unpin` | 已登录 + 匹配 scope | 在已分配版块内置顶或取消置顶。 |
| `POST` | `/moderation/threads/:id/lock`、`/unlock` | 已登录 + 匹配 scope | 在已分配版块内锁定或解锁主题。 |
| `DELETE` | `/moderation/threads/:thread_id/posts/:post_id` | 已登录 + 匹配 scope | 删除已分配版块主题中的回复。 |

授权时不能信任客户端提供的版块。主题操作从 `threads.category_id` 取作用域，回复操作先读取回复所属主题再取其版块；跨版块请求返回 `403`。插件当前停用时，用户侧治理接口返回 `503`，已保存的版主分配和审计数据不会删除。

## 6. 可靠任务 API

| 方法 | 路径 | Permission Code | 说明 |
| --- | --- | --- |
| `GET` | `/platform/reliability/summary`、`/events`、`/attempts`、`/workers`、`/operations`、`/command-audits`、`/compatibility` | `platform.reliability.read` | 读取队列、Worker、操作、命令关联和兼容使用；不返回 payload、Secret 或审计 details。 |
| `POST` | `/platform/reliability/events/:id/replay` | `platform.reliability.replay` | 仅重放 dead-letter；要求认证 actor 与 `Idempotency-Key`。 |
| `GET`/`POST` | `/platform/reliability/retention-preview`、`/retention-runs`、`/retention-runs/preview` | `platform.retention.preview` | 仅计算和保存 dry-run，不执行物理删除。 |

详细请求、错误与 Webhook 补充见 [v11 可靠任务与 Webhook 合同](v11可靠任务与Webhook合同.md)。

## 7. 兼容性规则

1. 新公开 API 应使用 `/api/v1` 前缀，并在所属模块中记录请求/响应变化。
2. 不应静默改变已有字段或路由语义；行为变化时新增明确字段或版本化路由。
3. 临时实验接口必须在对应帮助文档或进度记录中标记。
4. 内部 `/mcp/*` 路由不等同于标准 MCP；标准服务需要独立传输、认证、能力协商和契约测试。
5. `make contracts-check` 必须通过；新增管理路由没有显式 `RequirePermission` 时生成器直接失败。

## 8. 插件 UI Runtime 与 Gateway

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/ui/runtime-manifest` | 可选 JWT | 返回 `campusos.ui/v1`、revision、可见插件、三轴状态和 UI 贡献；权限 Action 由后端过滤。 |
| `GET` | `/ui/events` | 无敏感载荷 | SSE 只发送 revision 和 keepalive；客户端收到更高 revision 后重新获取完整 manifest。 |
| `ANY` | `/extensions/:plugin/*path` | 必须 JWT | 仅允许调用插件已声明 Action；Core 注入可信 caller 并执行权限、大小、5 秒超时和审计。 |

Runtime Manifest 不是授权替代。页面隐藏和 Action 过滤只是减少误操作，最终业务授权仍在 Gateway、插件服务和 Core 领域服务完成。详细合同见 [插件前端运行时与 Extension Gateway](../help/插件相关/插件前端运行时与Extension%20Gateway.md)。

## 9. 关联文档

- [接口协议适配器标准说明](../help/系统设计相关/接口协议适配器标准说明.md)
- [v10 权限管理设计与使用入门](../help/系统设计相关/v10权限管理设计与使用入门.md)
- [v11 权限管理与可靠审计设计入门](../help/系统设计相关/v11权限管理与可靠审计设计入门.md)
- [v11 可靠任务与 Webhook 合同](v11可靠任务与Webhook合同.md)
- [RBAC 权限与版主管理说明（历史兼容）](../help/系统设计相关/RBAC权限与版主管理说明.md)
- [v6 API 契约计划](../项目计划v6/01-v6版本计划书.md)
- [v6 第三版计划](../项目计划v6/02-v6版本计划书第三版.md)
- [v0.5 第二版计划书](../项目计划v5/01-v5版本计划书第二版.md)
- [Host API v2 受管数据合同](Host-API-v2受管数据合同.md)
- [Plugin Manifest v2 JSON Schema](plugin-manifest-v2.schema.json)
- [插件市场与受管数据](../help/插件相关/插件市场与受管数据-v0.9.md)
- [v10 最终全方位审计与后续路线](../项目计划v10/03-v10最终全方位审计与后续路线.md)
