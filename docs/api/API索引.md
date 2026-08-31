# CampusOS API 索引

> 基础地址：`http://localhost:8080/api/v1`
> 当前发布基线：`v0.13.0`（统一错误合同、可观测性、可靠任务运营、双端风格包、管理员准入、TOTP MFA 与容量门禁已验收）；`v0.14-dev` 的学期治理、对象存储和个人文档接口已进入当前工作树，但不代表 `v0.14.0 Final` 已发布。

第一次调用 API 时先读 [官方接口约定](../../docs-site/api/overview.md)。本页适合开发者按业务域查找当前
路由；机器集成应使用生成的 OpenAPI 和路由授权矩阵，不能从本文示例推断未声明字段。

## 1. 契约状态

CampusOS 通过 Gin 暴露带版本前缀的 HTTP 路由。当前路由级权威契约是 [openapi-v0.6-current.yaml](openapi-v0.6-current.yaml)，机器清单是 [http-routes-v0.6.json](http-routes-v0.6.json)，完整授权矩阵见 [HTTP 路由与授权矩阵](HTTP路由与授权矩阵-v0.6.md)。这些兼容文件名不再表示实现版本；内容的 `version` 为 v0.13，并由真实路由代码生成和检查漂移。

当前 OpenAPI 已覆盖 method/path、认证、显式权限、请求体和通用响应包络。验证式注册、登录、用户资料、帖子、回复、版块、个人空间、受管课表、个人文档、富文本、角色/版主和风格包选择等核心请求使用字段级 schema；动态插件配置、集成配置和部分上传接口使用 `GenericObject` 或 `MultipartRequest`，继续标记 `generic-experimental`。历史 [openapi-v0.3-pre.yaml](openapi-v0.3-pre.yaml) 只作旧版本参考。

错误响应在保留旧 `{ code, msg, data }` 字段的同时增加 `error.code`、`error.message`、`error.details`、`error.request_id` 和 `error.retryable`。成功响应也可携带顶层 `request_id`。列表接口的 `page` 必须大于等于 1，`page_size` 必须在 1 到 100 之间，非法值返回 `400 request.invalid`，不会静默改成默认值。

公开 `/users` 与 `/users/:id` 只返回公开资料，不返回邮箱；`/auth/me` 和登录响应仍向本人返回账号资料。
后台用户目录使用独立的 `GET /admin/users`，只有通过管理员准入且拥有 `identity.user.read` 的主体才能读取
包含邮箱的管理投影；未绑定邮箱及历史共享占位邮箱返回空值。`PUT /users/:id` 只允许本人修改 `nickname`、
`bio`、`avatar`，未知字段和跨用户写入分别返回 400 与 403。

## 2. 路由分组

| 分组 | 常见路径 | 说明 |
| --- | --- | --- |
| 认证与身份 | `/auth/*`、`/users/*` | 验证式注册、Session 轮换、设备管理、当前用户、资料和 RBAC 保护的管理操作。 |
| 社区 | `/categories`、`/admin/categories`、`/threads`、`/posts` | 两级版块、默认标签、帖子/回复流程、可见性和内容治理。 |
| 版主管理 | `/moderation/*` | 版块作用域分配、当前用户可执行动作、置顶、锁定和删除回复。 |
| 个人空间 | `/spaces/me/*`、`/u/:username` | 主页设置、内容同步、头像、风格包和公开主页。 |
| 个人课表 | `/schedule/terms`、`/schedule/me/*` | 开放学期目录、本人（含关闭历史）课表、选择/创建、保存和导入。 |
| 学期管理 | `/admin/academic-terms/*` | 管理员创建、修改、开放/关闭、设默认和删除受管春/秋学期。 |
| 我的文档 | `/documents/*` | 本人私有文档的上传、列表、不可变版本、预览、下载与回收站。 |
| 富文本文章 | `/richtext/articles/*` | 草稿、上传、预览、发布、详情、作者操作和管理端治理。 |
| 通用图文内容 | `/content/preview`、`/content/assets/images/*` | Community 安全预览与 User Storage 正文图片；不受单个 Feature 开关联动。 |
| 校园互助 | `/mutual-aid/*` | 结构化求助、提供、志愿协助与资源分享；独立业务状态不覆盖 Community 治理。 |
| 校园二手 | `/secondhand/*` | 结构化闲置物品信息；CNY 分价与交易状态不覆盖 Community 治理。 |
| 插件 | `/plugins/*`、`/plugin-packages/*` | 列表、配置、生命周期、插件包预检/导入/导出和审计。 |
| 插件中心 | `/plugin-market/*` | 用户目录、明确授权、受管记录/文件、导出删除、管理员目录、请求和发布记录。 |
| 插件前端运行时 | `/ui/runtime-manifest`、`/ui/events` | 当前主体可见 UI 合同、单调 Revision 与 SSE 变更通知。 |
| 插件业务 Gateway | `/extensions/:plugin/*path` | JWT、权限、状态、健康、大小、超时、Trace、审计和 Runtime 分发。 |
| 用户前台系统主题 | `/web-themes/*` | 公开列出管理员提供且筛查通过的 `target:web` 风格包、读取运行包和声明图片资源。 |
| 集成 | `/webhooks/*`、`/mcp/*`、`/message/*`、`/ai/*` | Webhook、内部 MCP-like 工具、Message local adapter 和 AI Gateway。 |
| 可靠任务 | `/platform/reliability/*` | 持久事件、消费尝试、Worker、可恢复操作、命令关联审计、兼容遥测、dry-run 与受控重放。 |
| 运维 | `/health`、`/metrics`、`/platform-logs/*` | 健康、最小指标和管理员日志流。 |

不同接口的认证和权限要求不同。客户端不能根据 UI 推断授权，应处理 HTTP 失败和 CampusOS 响应包络。

## 3. 两级版块 API

`categories` 仍是 Community Core 的唯一板块事实表。`group` 只能是根级导航容器；`board` 可以位于根级
或一个活动 group 下，并且只有未关闭的活动 board 能创建主题或回复。公开读取不会返回 archived 节点；
管理端读取使用独立的 `/admin/categories` 命名空间，避免已归档数据落入公开接口。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/categories`、`/categories/tree`、`/categories/:id` | 无 | 读取公开活动节点；flat 列表保持兼容，tree 提供两级导航。 |
| `GET` | `/admin/categories`、`/admin/categories/tree`、`/admin/categories/:id` | `category:read` | 读取包括 archived 节点的管理视图。 |
| `POST` | `/categories` | `community.category.create` | 创建根级 group 或 board，或创建某 active group 下的 board。 |
| `PUT` | `/categories/:id` | `community.category.update` | 更新名称、标签、颜色、关闭状态等；请求体必须携带 `version`。 |
| `PUT` | `/categories/:id/parent` | `community.category.move` | 移动 board；`parent_id: null` 表示移到根级，且必须携带 `version`。 |
| `GET` | `/categories/:id/archive-impact` | `community.category.archive` | 读取活动子 board 与关联主题数量。 |
| `POST` | `/categories/:id/archive?version=<n>` | `community.category.archive` | 归档节点；有活动子 board 的 group 会被拒绝。 |
| `POST` | `/categories/:id/restore?version=<n>` | `community.category.restore` | 恢复已归档节点；父 group 仍须活动。 |
| `DELETE` | `/categories/:id?version=<n>` | `community.category.archive` | 兼容入口，只执行 archive，不物理删除，并写兼容遥测。 |

`color` 只接受 `#RRGGBB` 或 `#RRGGBBAA`。并发修改返回 `409`，客户端应重新读取管理视图后再提交。
板块移动不会授予任何父级权限，版主仍只能治理显式分配的 board。详细状态、错误和迁移规则见
[v12 两级板块与可靠管理流程](v12两级板块与可靠管理流程.md)。

## 4. 结构化帖子类型与板块策略 API

`thread_type` 是稳定业务类型，和正文渲染用的 `content_format` 分开。当前固定类型为
`discussion`、`article`、`mutual_aid` 与 `secondhand`。每个 board 有独立的
创建策略；历史 board 默认只允许前两项。策略影响新建请求，不会删除或隐藏已经存在的内容。

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/categories/:id/thread-types` | 无 | 读取活动 board 允许的新建类型。 |
| `GET` | `/admin/categories/:id/thread-types` | `category:read` | 读取管理视图，包含归档 board。 |
| `PUT` | `/categories/:id/thread-types` | `community.category.configure_thread_types` | 以 `allowed_types` 和当前 `version` 整体替换 board 策略。 |

请求体示例：

```json
{
  "allowed_types": ["discussion", "article", "mutual_aid"],
  "version": 4
}
```

创建会同时校验 board 可发帖状态、固定类型、Feature 启用状态和板块策略。通用
`POST /threads` 始终创建 `discussion`；图文文章由既有 RichText API 创建 `article`；校园互助使用
类型化 Feature API 创建 `mutual_aid`；二手信息使用其独立 Feature API 创建 `secondhand`。所有这些入口均不接受通用
Detail JSON。结构化 Participant 是编译内 Built-in Feature 合同，External Plugin、SDK、MCP 和 Agent Runner
不会获得事务参与能力。详细说明见
[v12 结构化帖子类型与板块策略](../help/系统设计相关/v12结构化帖子类型与板块策略.md)。

互助与二手正文可以使用兼容 `markdown` 纯文本或 `safe_html` 图文格式。`safe_html` 保存前由 Community
Core 清洗，正文图片经 `POST /content/assets/images` 保存到当前用户的 User Storage；
`POST /content/preview` 使用相同清洗规则。完整图文文章仍使用 `richtext_article` 和独立 Article Detail。
详见 [v12 分组导航与结构化图文发布](../help/系统设计相关/v12分组导航与结构化图文发布.md)。

## 5. 校园互助 API

`feature.mutual-aid` 仅管理互助 Detail 和业务状态；基础 Thread、回复、可见性与治理仍由 Community Core
管理。管理员需要同时启用 Feature 并在目标 board 的类型策略中允许 `mutual_aid`。

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/mutual-aid/status` | 无 | 查询互助 Feature 是否启用；停用时仍返回 `enabled=false`。 |
| `GET` | `/mutual-aid/threads` | 无 | 分页读取公开互助内容；支持 `category_id`、`keyword`、`tag`。 |
| `GET` | `/mutual-aid/threads/:id` | 无 | 读取公开 Thread 和 Detail。 |
| `POST` | `/mutual-aid/threads` | JWT | 创建本人互助内容。 |
| `GET` | `/mutual-aid/threads/:id/me` | JWT | 作者编辑读取。 |
| `PUT` | `/mutual-aid/threads/:id` | JWT + 作者 | 以完整投影更新正文和 Detail，需 `version`。 |
| `POST` | `/mutual-aid/threads/:id/status` | JWT + 作者 | 按状态机更新 `aid_status`，需 `version`。 |

状态只能按 `open -> in_progress/resolved/closed`、`in_progress -> open/resolved/closed`、
`resolved -> closed` 转换。发生 `409` 时客户端必须重新读取后再提交，不能用旧版本覆盖新修改。详情字段和
安全边界见 [v12 校园互助发布与状态管理](../help/系统设计相关/v12校园互助发布与状态管理.md)。
编辑或状态更新要求基础 Thread 仍处于 active（未回收）状态；作者已回收的内容返回 `409 / 40009`，不会产生新的
互助状态、审计或 Outbox。未分类服务端错误统一返回 `500 / 10006` 与通用错误文案。创建和编辑请求可附带
`content_format: "safe_html"`；省略时继续按历史纯文本处理。

## 6. 校园二手 API

`feature.secondhand` 只拥有价格、成色、交付方式、交易状态和地点范围；基础 Thread、回复、可见性与
治理仍由 Community Core 拥有。管理员必须启用 Feature，并在目标 board 的策略中允许 `secondhand`。

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/secondhand/status` | 无 | 查询二手 Feature 是否启用；停用时仍返回 `enabled=false`。 |
| `GET` | `/secondhand/threads` | 无 | 分页读取公开二手内容；支持 `category_id`、`keyword`、`tag`。 |
| `GET` | `/secondhand/threads/:id` | 无 | 读取公开 Thread 和 Detail。 |
| `POST` | `/secondhand/threads` | JWT | 创建本人二手信息。 |
| `GET` | `/secondhand/threads/:id/me` | JWT | 作者编辑读取。 |
| `PUT` | `/secondhand/threads/:id` | JWT + 作者 | 以完整投影更新正文与 Detail，需 `version`。 |
| `POST` | `/secondhand/threads/:id/status` | JWT + 作者 | 更新 `trade_status`，需 `version`。 |

`price_minor` 是分，不是浮点元；首版只接受 `CNY`。停用时只有只读 `/secondhand/status`
会继续返回 `enabled=false`，其余二手路径由 Feature Gate 拒绝。状态只能按
`available -> reserved/sold/closed`、`reserved -> available/sold/closed` 转换，`sold` 和
`closed` 是终态。详情和安全边界见
[v12 校园二手发布与交易状态管理](../help/系统设计相关/v12校园二手发布与交易状态管理.md)。
编辑或状态更新要求基础 Thread 仍处于 active（未回收）状态；作者已回收的内容返回 `409 / 40009`，不会产生新的
交易状态、审计或 Outbox。未分类服务端错误统一返回 `500 / 10006` 与通用错误文案。创建和编辑请求可附带
`content_format: "safe_html"`；省略时继续按历史纯文本处理。

## 7. Session 与设备 API

v0.12 登录以短期 Access JWT 配合服务端可撤销 Session 实现。浏览器 Refresh Token 位于
`HttpOnly` Cookie，响应中的 Access Token 只应放在进程内存。详细流程见
[v12 会话与 Token 安全流程](v12会话与Token安全流程.md)。

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `POST` | `/auth/login` | 无 | 创建 Session，返回短期 Access Token，并设置 Refresh/CSRF Cookie。 |
| `POST` | `/auth/refresh` | Refresh Cookie + CSRF | 原子轮换一次性 Refresh Token；JSON body 只在受控兼容配置下接受。 |
| `POST` | `/auth/logout` | Access JWT + CSRF | 撤销当前 Session。 |
| `POST` | `/auth/logout-all` | Access JWT + CSRF | 撤销全部 Session 并立即失效旧 Access JWT。 |
| `GET` | `/auth/sessions` | Access JWT | 查看当前账号的安全设备投影。 |
| `DELETE` | `/auth/sessions/:id` | Access JWT + CSRF | 撤销当前账号拥有的指定设备。 |

刷新失败、过期、重放或已撤销均返回统一未授权语义；客户端不得尝试通过错误文案判断 Session
是否存在。旧 v11 JWT 缺少 `typ/session_id/auth_version`，升级后必须重新登录。

## 8. v14-dev 受管课表、学期与我的文档 API

学期是管理员维护的服务端事实，客户端不能用年份参数创建未配置学期。关闭学期只能由已有课表用户读取，所有保存和导入仍由服务端检查开放状态与版本。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/schedule/terms` | 返回开放学期目录与默认项。 |
| `GET` | `/schedule/me` | 返回当前查看课表；使用 `academic_term_id` 读取本人已有的关闭历史，或在兼容期同时提供 `term_year` 与 `semester` 读取已存在学期。两种查询不能混用。 |
| `GET` | `/schedule/me/terms` | 列出本人已有课表，含 `academic_term_id`、`term_status`、`version` 和查看偏好。 |
| `POST` | `/schedule/me/terms/activate` | 使用 `academic_term_id` 选择开放学期；关闭学期只允许选择本人已有的历史项。兼容的 `term_year` + `semester` 只能解析现存开放学期。 |
| `PUT` | `/schedule/me` | 保存开放学期课表。已有受管课表必须携带返回的 `expected_version`，服务端先保存私有 Object 再切换引用。 |
| `POST` | `/schedule/me/import` | multipart 导入，提供 `file`、`term_year`、`semester`，可选 `replace` 和 `expected_version`；超限会返回中文的 `max_bytes`、`actual_bytes` 与下一步。 |
| `GET/POST` | `/admin/academic-terms` | 管理员列出或创建学期；列表含课表引用计数。 |
| `PUT` | `/admin/academic-terms/:id` | 使用 `expected_version` 修改第一周。 |
| `POST` | `/admin/academic-terms/:id/open`、`/close`、`/default` | 使用版本和原因执行状态命令。 |
| `DELETE` | `/admin/academic-terms/:id` | 使用版本和原因删除无引用学期；有引用时返回稳定冲突。 |
| `GET/POST` | `/documents`、`/documents/upload` | 列出本人文档，或创建文本文档/上传文件。 |
| `GET/PUT/DELETE` | `/documents/:id` | 读取元数据、追加版本或兼容删除入口；推荐回收站命令见下一行。 |
| `GET` | `/documents/:id/content`、`/download`、`/preview`、`/versions` | 读取文本版本、认证下载、受控预览或版本历史；可选 `version_id` 指向历史版本。 |
| `POST` | `/documents/:id/trash`、`/restore`、`/versions/:version_id/restore` | 移入/恢复回收站，或从历史版本创建新版本，均需要 `expected_version`。 |

`semester` 的标准值为 `spring` 或 `fall`。详细请求字段和所有错误代码以生成的 OpenAPI 为准；学期、配额和文档上传失败使用中文安全文案和机器可读 details，不能依据底层错误文本编写客户端逻辑。

## 9. 角色与权限管理 API

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
| `GET` | `/identity/challenge-policy` | `identity.challenge_policy.read` | 读取始终启用的验证码窗口与次数策略。 |
| `PUT` | `/identity/challenge-policy` | `identity.challenge_policy.update` | 使用 `expected_version` 热更新策略并写 required audit。 |

HTTP Handler 缺少已认证操作者上下文时直接返回 `401`，不会回退到受信任内部调用。`member`、`guest` 是保护角色；系统角色矩阵只读，版主只能使用 category 作用域。完整设计见 [v10 权限管理设计与使用入门](../help/系统设计相关/v10权限管理设计与使用入门.md)。

## 10. 版主管理 API

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

## 11. 可靠任务 API

| 方法 | 路径 | Permission Code | 说明 |
| --- | --- | --- |
| `GET` | `/platform/reliability/summary`、`/events`、`/attempts`、`/workers`、`/operations`、`/command-audits`、`/compatibility` | `platform.reliability.read` | 读取队列、Worker、操作、命令关联和兼容使用；不返回 payload、Secret 或审计 details。 |
| `POST` | `/platform/reliability/events/:id/replay` | `platform.reliability.replay` | 仅重放 dead-letter；要求认证 actor 与 `Idempotency-Key`。 |
| `GET`/`POST` | `/platform/reliability/retention-preview`、`/retention-runs`、`/retention-runs/preview` | `platform.retention.preview` | 仅计算和保存 dry-run，不执行物理删除。 |

详细请求、错误与 Webhook 补充见 [v11 可靠任务与 Webhook 合同](v11可靠任务与Webhook合同.md)。

## 12. 兼容性规则

1. 新公开 API 应使用 `/api/v1` 前缀，并在所属模块中记录请求/响应变化。
2. 不应静默改变已有字段或路由语义；行为变化时新增明确字段或版本化路由。
3. 临时实验接口必须在对应帮助文档或进度记录中标记。
4. 内部 `/mcp/*` 路由不等同于标准 MCP；标准服务需要独立传输、认证、能力协商和契约测试。
5. `make contracts-check` 必须通过；新增管理路由没有显式 `RequirePermission` 时生成器直接失败。

## 13. 插件 UI Runtime 与 Gateway

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/ui/runtime-manifest` | 可选 JWT | 返回 `campusos.ui/v1`、revision、可见插件、三轴状态和 UI 贡献；权限 Action 由后端过滤。 |
| `GET` | `/ui/events` | 无敏感载荷 | SSE 只发送 revision 和 keepalive；客户端收到更高 revision 后重新获取完整 manifest。 |
| `ANY` | `/extensions/:plugin/*path` | 必须 JWT | 仅允许调用插件已声明 Action；Core 注入可信 caller 并执行权限、大小、5 秒超时和审计。 |

Runtime Manifest 不是授权替代。页面隐藏和 Action 过滤只是减少误操作，最终业务授权仍在 Gateway、插件服务和 Core 领域服务完成。详细合同见 [插件前端运行时与 Extension Gateway](../help/插件相关/插件前端运行时与Extension%20Gateway.md)。

## 14. 关联文档

- [接口协议适配器标准说明](../help/系统设计相关/接口协议适配器标准说明.md)
- [v10 权限管理设计与使用入门](../help/系统设计相关/v10权限管理设计与使用入门.md)
- [v11 权限管理与可靠审计设计入门](../help/系统设计相关/v11权限管理与可靠审计设计入门.md)
- [v11 可靠任务与 Webhook 合同](v11可靠任务与Webhook合同.md)
- [v12 会话与 Token 安全流程](v12会话与Token安全流程.md)
- [v12 结构化帖子类型与板块策略](../help/系统设计相关/v12结构化帖子类型与板块策略.md)
- [v12 校园互助发布与状态管理](../help/系统设计相关/v12校园互助发布与状态管理.md)
- [v12 校园二手发布与交易状态管理](../help/系统设计相关/v12校园二手发布与交易状态管理.md)
- [RBAC 权限与版主管理说明（历史兼容）](../help/系统设计相关/RBAC权限与版主管理说明.md)
- [v6 API 契约计划](../项目计划v6/01-v6版本计划书.md)
- [v6 第三版计划](../项目计划v6/02-v6版本计划书第三版.md)
- [v0.5 第二版计划书](../项目计划v5/01-v5版本计划书第二版.md)
- [Host API v2 受管数据合同](Host-API-v2受管数据合同.md)
- [Plugin Manifest v2 JSON Schema](plugin-manifest-v2.schema.json)
- [插件市场与受管数据](../help/插件相关/插件市场与受管数据-v0.9.md)
- [v10 最终全方位审计与后续路线](../项目计划v10/03-v10最终全方位审计与后续路线.md)
