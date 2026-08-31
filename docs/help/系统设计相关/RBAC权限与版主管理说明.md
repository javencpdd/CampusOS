# CampusOS RBAC 权限与版主管理说明

> 文档状态：v0.10 前后的权限模型快照；当前操作入口见
> [权限配置入门](../../../docs-site/guide/permission-configuration.md) 和
> [v0.11 权限管理与可靠审计](v11权限管理与可靠审计设计入门.md)。
> 适用范围：角色管理、版块版主授权、Moderation Core 和用户端治理

## 1. 先看结论

- 版主不是全站角色。每个版主必须分配至少一个版块，只能管理被分配版块中的内容。
- 一个用户可以同时负责多个版块；管理员可以随时增减其版块范围。
- 当前版主可执行主题置顶/取消置顶、主题锁定/解锁、删除回复。
- 版主不能分配角色、管理用户状态、修改版块、删除主题、管理插件或查看系统日志。
- 管理员在后台“版主管理”页面统一设置系统级动作上限和每个用户的版块范围。
- `core.moderation` 是始终启用的 Core Module，不能停用或卸载；管理员只能热更新 `allow_pin`、`allow_lock`、`allow_delete_post` 来缩小版主动作范围。

前端按钮只用于提示。每次治理请求仍由后端重新读取主题或回复所属版块，并检查用户是否拥有该版块的作用域。

## 2. 授权模型

一次受保护请求依次经过：

```text
登录身份
  -> resource:action 动作权限
  -> global/category 数据范围
  -> 目标资源实际所属版块
  -> Moderation Core 动作上限
  -> 执行业务操作并写入审计
```

| 角色 | 授权方式 | 主要职责 |
| --- | --- | --- |
| `member` | 有效用户隐式拥有，不写入 `user_roles` | 发帖、回复和管理本人内容。 |
| `moderator` | 一个或多个 `category` 作用域 | 治理被分配版块内的主题和回复。 |
| `admin` | `global` 作用域 | 用户、角色、版块、内容、插件和系统管理。 |
| `guest` | 未登录语义，不写入 `user_roles` | 访问公开内容。 |

管理员和版主都保留隐式 `member` 能力。`member`、`guest` 不能通过角色接口分配或撤销。

## 3. 版主权限边界

### 3.1 当前可以做什么

| 权限 | 用户端操作 | 必要条件 |
| --- | --- | --- |
| `thread:pin` | 置顶、取消置顶 | Core 配置允许置顶，且主题属于已分配版块。 |
| `thread:lock` | 锁定、解锁 | Core 配置允许锁定，且主题属于已分配版块。 |
| `post:delete` | 删除回复 | Core 配置允许删除回复，且回复所属主题属于已分配版块。 |

按钮显示在帖子详情页。页面通过 `/moderation/me?thread_id=...` 获取后端确认后的 `actions`，不根据前端保存的角色名称自行推断。

### 3.2 当前不能做什么

版主不能：

- 管理未分配版块中的任何主题或回复。
- 给自己或他人分配角色、扩大版块范围或修改 Moderation Core 配置。
- 封禁用户、删除用户、修改版块、删除主题或管理全站内容。
- 进入管理员的插件、集成、平台日志、系统配置和密钥功能。
- 通过历史全局治理路由绕过作用域检查。

普通的 `HasPermission` 只接受全局角色关联。版主的版块关联必须使用 `CheckScoped` 并匹配目标版块，因此不会意外获得历史管理接口的全局权限。

## 4. 管理员如何分配版主

### 4.1 从用户管理页分配

1. 进入管理后台 `/users`。
2. 找到目标用户，点击“分配角色”。
3. 选择“版主”。
4. 在随即出现的版块多选框中选择一个或多个版块。
5. 保存。没有选择版块时不能提交。

对于已经是版主的用户，再次选择“版主”可以修改其版块范围。撤销版主角色会清除该用户的全部版块授权。

### 4.2 从版主管理页配置

进入管理后台 `/moderators`，可以：

- 查看 Moderation Core 的健康状态和当前动作配置。
- 设置所有版主动作的系统级最大范围；配置保存后立即用于下一次授权判断，审计是强制安全能力，不能关闭。
- 查看每名版主已经负责的版块。
- 为用户增加或移除多个版块。
- 将版块列表清空以撤销版主身份。

权限分为两层：

| 层级 | 管理内容 | 示例 |
| --- | --- | --- |
| Core 动作上限 | 所有版主最多能做哪些动作 | 全站关闭“删除回复”。 |
| 用户级版块范围 | 一个版主能在哪些版块执行已开启动作 | user1 只负责“校园生活”和“技术交流”。 |

两层必须同时满足。给 user1 分配“校园生活”不会自动开放其他版块；打开“允许置顶”也不会让未分配版块可管理。

## 5. 后端如何防止跨版块操作

数据库中的版主授权形态为：

```text
user_roles
  user_id = 版主用户
  role_id = moderator
  scope_type = category
  scope_id = categories.id
```

一个版块对应一条有效关联。主题治理时，后端读取 `threads.category_id`；回复治理时，先读取 `posts.thread_id`，再读取主题的 `category_id`。请求体或查询参数中的版块值不作为授权证据。

```text
目标实际 category_id
  == 用户某条 moderator category scope_id
  && moderator 角色拥有所需 action
  && Core 动作上限已开启
  -> 允许
否则
  -> 403 或 503
```

版块授权首版不自动继承到子版块。需要管理子版块时，管理员必须明确勾选该子版块。

## 6. Moderation Core 生命周期与配置

描述符位于 `modules/core/moderation/module.yaml`，实现位于
`internal/modules/core/moderation`。它使用 `kind: core` 和
`activation_mode: always-on`：

- 版块作用域、权限策略、审计和数据完整性校验始终运行。
- Admin `/features` 对其停用请求会返回错误，不能通过重启绕过。
- `category-moderation` 只保留为旧 API 兼容别名，不能作为 External Plugin 导入。
- `allow_pin`、`allow_lock`、`allow_delete_post` 保存后热更新，下一次后端授权检查立即使用。
- 关闭某个动作不会删除 `user_roles` 版块授权或历史审计，也不会授予其他动作。
- 新进入或刷新帖子详情后按钮会同步；旧页面即使仍显示按钮，后端也会立即拒绝已关闭动作。

## 7. 接口

### 7.1 全局角色接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/roles` | `role:read` | 返回内置角色。 |
| `GET` | `/api/v1/users/:id/roles` | `role:read` | 返回显式角色和隐式 `member`。 |
| `POST` | `/api/v1/users/:id/roles` | `role:assign` | 只用于全局角色；分配 `moderator` 返回错误并提示改用版主管理。 |
| `DELETE` | `/api/v1/users/:id/roles` | `role:revoke` | 撤销目标角色的全部显式关联；禁止撤销自己的角色。 |

### 7.2 版主管理接口

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/moderation/admin/moderators` | `role:read` | 列出所有版主及其版块。 |
| `GET` | `/api/v1/moderation/admin/moderators/:user_id` | `role:read` | 读取一个用户的版块范围。 |
| `PUT` | `/api/v1/moderation/admin/moderators/:user_id` | `role:assign` | 用 `category_ids` 完整替换范围；空数组表示撤销。 |
| `GET` | `/api/v1/moderation/status` | 已登录 | 返回 Moderation Core 状态和动作配置。 |
| `GET` | `/api/v1/moderation/me?thread_id=...` | 已登录 | 返回当前用户对指定主题可执行的动作。 |
| `POST` | `/api/v1/moderation/threads/:id/pin`、`unpin` | 匹配版块 scope | 置顶或取消置顶。 |
| `POST` | `/api/v1/moderation/threads/:id/lock`、`unlock` | 匹配版块 scope | 锁定或解锁。 |
| `DELETE` | `/api/v1/moderation/threads/:thread_id/posts/:post_id` | 匹配版块 scope | 删除回复。 |

## 8. 数据迁移与审计

`000014_role_assignment_permissions` 修复角色关联 ID 和全局唯一性，并增加 `role:read`、`role:assign`、`role:revoke`。

`000015_category_moderation_scope` 进一步：

1. 软删除历史有效的全局版主关联，不把旧版主自动升级为全站版主。
2. 移除版主的用户封禁、主题写入和主题删除权限。
3. 为管理员和版主补充 `thread:lock`。
4. 约束有效关联只能是 `global + NULL` 或 `category + 正数 ID`，并禁止版主使用 global 作用域。
5. 为用户、角色和作用域查询增加索引。

版主范围变更、置顶、锁定和删除回复强制写入既有 `audit_logs`，记录操作者、动作、资源和目标。审计是不可关闭的 Core 安全能力，调整动作上限不会清理这些记录。

## 9. 验收规则

| 场景 | 预期 |
| --- | --- |
| 分配版主但未选择版块 | 管理端阻止提交；通用角色接口也拒绝建立全局版主。 |
| 分配一个版块 | 只在该版块帖子详情显示允许的治理动作。 |
| 跨版块置顶、锁定或删除回复 | 后端返回 `403`。 |
| 使用历史全局管理路由 | 版主返回 `403`。 |
| 管理员清空版块范围 | 用户不再拥有版主角色。 |
| 请求停用 `core.moderation` | Feature API 拒绝；Core 继续运行。 |
| 关闭一个动作上限 | 下一次授权检查立即拒绝该动作，其他动作不受影响。 |
| 重新打开动作上限 | 已有版块授权继续生效，无需重启。 |

每次扩展版主动作时，都必须增加同版块成功、跨版块拒绝、动作上限关闭拒绝、普通用户拒绝和审计写入测试。
