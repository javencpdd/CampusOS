# CampusOS API 索引

> 基础地址：`http://localhost:8080/api/v1`
> 当前实现基线：`v0.6.8-dev`

## 1. 契约状态

CampusOS 通过 Gin 暴露带版本前缀的 HTTP 路由。当前路由级权威契约是 [openapi-v0.6-current.yaml](openapi-v0.6-current.yaml)，机器清单是 [http-routes-v0.6.json](http-routes-v0.6.json)，完整授权矩阵见 [HTTP 路由与授权矩阵](HTTP路由与授权矩阵-v0.6.md)。这些产物由真实路由代码生成并在 CI 检查漂移。

当前 OpenAPI 已覆盖 method/path、认证、显式权限和通用响应包络。尚未补齐业务字段 schema 的接口仍标记 `experimental`，具体模型以各 `internal/<module>/` 请求/响应类型为准。历史 [openapi-v0.3-pre.yaml](openapi-v0.3-pre.yaml) 只作旧版本参考。

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
| 用户前台系统主题 | `/web-themes/*` | 公开列出管理员提供且筛查通过的 `target:web` 风格包、读取运行包和声明图片资源。 |
| 集成 | `/webhooks/*`、`/mcp/*`、`/message/*`、`/ai/*` | Webhook、内部 MCP-like 工具、Message local adapter 和 AI Gateway。 |
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

## 4. 角色管理 API

角色定义、读取、分配和撤销由后台 API 管理。它们不再共用 `role:manage`：

| 方法 | 路径 | 所需权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/roles` | `role:read` | 列出内置角色；`member`、`guest` 不可通过角色管理接口赋予或撤销。 |
| `GET` | `/users/:id/roles` | `role:read` | 读取目标用户的有效角色，始终包含隐式基础 `member`。 |
| `POST` | `/users/:id/roles` | `role:assign` | 分配额外全局角色。`moderator` 不允许经此接口分配，必须使用版主管理 API 指定版块。 |
| `DELETE` | `/users/:id/roles` | `role:revoke` | 请求体为 `{ "role_id": 2 }`；不能撤销当前操作人的角色。 |

默认管理员拥有上述三项权限；版主、普通用户和访客不拥有角色管理权限。`member` 是每个有效登录用户自动具备的基础角色，`guest` 只用于匿名访问语义；二者均不是可分配角色。管理员等系统角色使用全局关联，版主只能使用版块作用域关联。

## 5. 版主管理 API

`category-moderation` 是系统级内置插件。插件整体启用/停用仍在重启 API 后生效，但 `allow_pin`、`allow_lock`、`allow_delete_post` 动作开关保存后立即生效。管理接口可在插件待重启或当前停用时维护授权范围；用户侧治理接口只有在当前 API 进程已加载插件时可用。

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

## 6. 兼容性规则

1. 新公开 API 应使用 `/api/v1` 前缀，并在所属模块中记录请求/响应变化。
2. 不应静默改变已有字段或路由语义；行为变化时新增明确字段或版本化路由。
3. 临时实验接口必须在对应帮助文档或进度记录中标记。
4. 内部 `/mcp/*` 路由不等同于标准 MCP；标准服务需要独立传输、认证、能力协商和契约测试。
5. `make contracts-check` 必须通过；新增管理路由没有显式 `RequirePermission` 时生成器直接失败。

## 7. 关联文档

- [接口协议适配器标准说明](../help/系统设计相关/接口协议适配器标准说明.md)
- [RBAC 权限与版主管理说明](../help/系统设计相关/RBAC权限与版主管理说明.md)
- [v6 API 契约计划](../项目计划v6/01-v6版本计划书.md)
- [v0.5 第二版计划书](../项目计划v5/01-v5版本计划书第二版.md)
