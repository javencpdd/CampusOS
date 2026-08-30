# 认证与社区 API

## 注册

注册需要先完成邮箱验证：

1. `POST /api/v1/auth/registration/challenge` 请求验证码。
2. `POST /api/v1/auth/registration/verify` 用六位验证码换取一次性 Ticket。
3. 下面的 `/auth/register` 请求同时携带 Challenge ID 和 Ticket。

```http
POST /api/v1/auth/register
Content-Type: application/json
```

```json
{
  "username": "alice",
  "nickname": "Alice",
  "email": "alice@example.edu",
  "password": "a-strong-password",
  "challenge_id": "opaque-public-id",
  "ticket": "opaque-one-time-value"
}
```

用户名、昵称、邮箱和密码都有长度或格式校验。缺少 Ticket 返回
`400 identity.verification_required`；失效或已使用 Ticket 返回
`400 identity.registration_verification_invalid`。邮箱和用户名冲突通常返回 `409`。完整流程见
[注册邮箱验证](./registration.md)。

## 登录

```http
POST /api/v1/auth/login
Content-Type: application/json
```

```json
{
  "email": "alice@example.edu",
  "password": "a-strong-password"
}
```

成功响应包含用户、有效角色、`access_token` 和 `refresh_token`。后续请求使用：

```http
Authorization: Bearer <access_token>
```

## 当前用户

```http
GET /api/v1/auth/me
Authorization: Bearer <access_token>
```

## 两级版块

CampusOS 只有两级导航：`group` 是根级分组，不能发帖；`board` 是实际版块，可以位于根级或一个
活动 group 下。一个 board 是否可发帖由三项服务端事实共同决定：它必须是 `board`、状态为
`active`、且没有关闭。客户端的下拉框和导航只用于减少误操作，后端仍会重新校验。

| 方法 | 路径 | 认证和权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/categories`、`/categories/tree`、`/categories/:id` | 无 | 读取公开的活动节点；flat 列表保持旧客户端兼容，tree 用于导航。 |
| `GET` | `/admin/categories`、`/admin/categories/tree`、`/admin/categories/:id` | 管理员 + `category:read` | 读取包括归档节点的管理视图。 |
| `POST` | `/categories` | `community.category.create` | 创建根级 group/board 或 group 下的 board。 |
| `PUT` | `/categories/:id` | `community.category.update` | 更新展示、标签和关闭状态，必须提交当前 `version`。 |
| `PUT` | `/categories/:id/parent` | `community.category.move` | 移动 board；`parent_id: null` 明确表示移到根级。 |
| `GET` | `/categories/:id/archive-impact` | `community.category.archive` | 在归档前读取活动子 board 和关联主题数量。 |
| `POST` | `/categories/:id/archive?version=<n>` | `community.category.archive` | 归档；有活动子 board 的 group 被拒绝。 |
| `POST` | `/categories/:id/restore?version=<n>` | `community.category.restore` | 恢复；父 group 必须仍是活动状态。 |
| `DELETE` | `/categories/:id?version=<n>` | `community.category.archive` | 历史兼容入口，实际执行 archive，不物理删除。 |

创建或移动 board 时，父节点必须是活动的 group。移动不会继承或扩大版主权限；版主权限始终只和
管理员显式分配的 board 绑定。`color` 只能为 `#RRGGBB` 或 `#RRGGBBAA`。更新、移动、归档和恢复
使用乐观版本控制，返回 `409` 时先重新读取后再处理冲突。

版块可以配置默认标签。用户发帖时，服务会将默认标签和用户标签合并并去重。

公开帖子列表的 `category_id` 也接受 group ID。服务端会把活动 group 展开为其全部活动子 board，再执行
统一的公开状态和分页筛选；空 group 返回空列表，不会误命中“全部帖子”缓存。Web 导航中的“分组 · 全部帖子”
使用该语义，单个 board 仍只返回本板块帖子。PostgreSQL 实现把展开结果校验并转换为 `bigint[]`，与
`threads.category_id BIGINT` 同型比较；互助和二手的基础 Thread 因而都包含在所属 group 的聚合列表中。

## 结构化帖子类型与板块策略

`thread_type` 表示内容的业务类型，`content_format` 只表示正文如何渲染。当前固定类型是：

| 类型 | 入口 | 说明 |
| --- | --- | --- |
| `discussion` | `POST /threads` | 标准讨论、提问和普通文本主题。 |
| `article` | RichText API | 受控富文本图文文章。 |
| `mutual_aid` | Campus Mutual Aid API | 校园求助、提供、志愿协助与独立业务状态。 |
| `secondhand` | Campus Secondhand API | CNY 分价、成色、交付方式和交易状态受约束的闲置信息。 |

每个 board 有独立策略。历史 board 默认允许 `discussion`、`article`；管理员可使用下列
接口启用其他类型：

| 方法 | 路径 | 认证和权限 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/categories/:id/thread-types` | 无 | 读取活动 board 的可创建类型。 |
| `GET` | `/admin/categories/:id/thread-types` | 管理员 + `category:read` | 读取管理视图，包含归档 board。 |
| `PUT` | `/categories/:id/thread-types` | `community.category.configure_thread_types` | 使用 `allowed_types` 与 `version` 整体更新。 |

```json
{
  "allowed_types": ["discussion", "article", "mutual_aid"],
  "version": 4
}
```

服务端在创建时同时校验 board 是否可发帖、类型是否固定、对应 Built-in Feature 是否启用以及该 board
是否允许该类型。关闭类型只阻止新的创建，不删除已有内容，也不削弱 Community Core 的下架、恢复和审计能力。
类型化 Detail 只能由编译内 Built-in Feature 在同一可靠事务内写入；External Plugin、MCP、Agent Runner
和浏览器客户端不能参与宿主数据库事务。

## 校园互助

校园互助是 `feature.mutual-aid` 提供的 Built-in Feature。它只拥有一对一 Detail 中的互助类型、状态、
截止时间、粗粒度地点和联系渠道类别；Thread、回复、发布可见性和治理仍由 Community Core 拥有。

管理员需先启用 Feature，并在目标 board 的类型策略里允许 `mutual_aid`。两项条件缺一不可。Feature
停用时，系统拒绝其业务路径而不删除历史 Thread 或 Detail，Community 仍可治理基础内容。只读
`GET /mutual-aid/status` 会继续返回 `enabled=false`，供客户端显示准确状态。

| 方法 | 路径 | 认证 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/mutual-aid/status` | 无 | 查询功能是否启用；停用时仍返回 `enabled=false`。 |
| `GET` | `/mutual-aid/threads` | 无 | 分页查看公开互助内容。 |
| `GET` | `/mutual-aid/threads/:id` | 无 | 读取公开互助详情。 |
| `POST` | `/mutual-aid/threads` | JWT | 创建互助内容。 |
| `GET` | `/mutual-aid/threads/:id/me` | JWT + 作者 | 读取编辑投影。 |
| `PUT` | `/mutual-aid/threads/:id` | JWT + 作者 | 编辑正文和 Detail，需版本号。 |
| `POST` | `/mutual-aid/threads/:id/status` | JWT + 作者 | 修改互助业务状态，需版本号。 |

状态机为 `open -> in_progress/resolved/closed`、`in_progress -> open/resolved/closed`、`resolved -> closed`。
它不等同于帖子发布或审核状态。状态冲突返回 `409`；客户端应重新读取后再让作者确认。
编辑和状态更新只接受仍处于 active（未回收）状态的基础 Thread；作者已回收的内容返回 `409 / 40009`，且不会
新增互助状态、审计或 Outbox 事件。未分类内部故障统一返回 `500 / 10006` 与通用错误文案，不会暴露数据库、
查询或用户内部信息。

## 校园二手

校园二手是 `feature.secondhand` 提供的 Built-in Feature。它只拥有一对一 Detail 中的人民币分价、物品成色、
交付方式、交易状态和地点范围；Thread、回复、发布可见性和治理仍由 Community Core 拥有。

管理员需先启用 Feature，并在目标 board 的类型策略里允许 `secondhand`。Feature 停用时，Path Gate
拒绝其业务路径，不删除历史 Thread 或 Detail；Community 仍可治理基础内容。只读
`GET /secondhand/status` 会继续返回 `enabled=false`，供客户端显示准确状态。

| 方法 | 路径 | 认证 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/secondhand/status` | 无 | 查询功能是否启用；停用时仍返回 `enabled=false`。 |
| `GET` | `/secondhand/threads` | 无 | 分页查看公开二手信息。 |
| `GET` | `/secondhand/threads/:id` | 无 | 读取公开二手详情。 |
| `POST` | `/secondhand/threads` | JWT | 创建二手信息。 |
| `GET` | `/secondhand/threads/:id/me` | JWT + 作者 | 读取编辑投影。 |
| `PUT` | `/secondhand/threads/:id` | JWT + 作者 | 编辑正文和 Detail，需版本号。 |
| `POST` | `/secondhand/threads/:id/status` | JWT + 作者 | 修改交易业务状态，需版本号。 |

`price_minor` 以分保存，首版只允许 `CNY`。Web 表单允许两位小数并用 `Math.round(元 × 100)` 转换，因此
`12.34` 元以整数 `1234` 提交，不使用浮点数作为持久化金额。状态机为
`available -> reserved/sold/closed`、`reserved -> available/sold/closed`；`sold` 和
`closed` 是终态。它不等同于帖子发布或审核状态，状态冲突返回 `409`。
编辑和状态更新只接受仍处于 active（未回收）状态的基础 Thread；作者已回收的内容返回 `409 / 40009`，且不会
新增交易状态、审计或 Outbox 事件。未分类内部故障统一返回 `500 / 10006` 与通用错误文案，不会暴露数据库、
查询或用户内部信息。

## 创建普通帖子

```http
POST /api/v1/threads
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "title": "社团招新安排",
  "content": "本周五在活动中心进行招新。",
  "category_id": "1000000000000000004",
  "tags": ["社团", "招新"],
  "is_private": false
}
```

`category_id` 使用字符串承载 BIGINT ID，避免 JavaScript 数值精度损失。

## 帖子读取与作者操作

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/threads?page=1&page_size=20` | 公开帖子列表。 |
| `GET` | `/threads/:id` | 公开帖子详情。 |
| `GET` | `/threads/:id/me` | 按当前用户可见性读取详情。 |
| `PUT` | `/threads/:id` | 作者更新普通帖子。 |
| `DELETE` | `/threads/:id` | 作者删除普通帖子。 |

私密帖子只能由作者和明确允许的治理主体访问。客户端不要仅根据列表中是否出现来判断权限。

## 站内通知

管理员或其他治理主体把帖子移入回收站或下架时，Community Core 会在同一可靠事务中给作者写入站内通知。
其他用户直接回复主题时通知主题作者；回复某条评论时通知评论作者，并在接收者不同的情况下同时通知主题作者。
版主或管理员删除评论时通知评论作者，评论删除与通知在同一可靠命令中提交；管理员调整用户的板块版主范围时，
按差额发送 `identity.moderator.granted` / `identity.moderator.revoked` 通知。
自我回复、同一接收者、版主删除自己的评论和管理员修改自己的授权不会产生重复或自我通知。除版主授权变更外，
通知保存失败会使对应可靠命令回滚；授权变更的通知是授权提交后的尽力而为投递。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/notifications?page=1&page_size=20` | 获取当前用户通知、分页和未读总数。 |
| `POST` | `/notifications/:id/read` | 标记当前用户拥有的一条通知为已读。 |
| `POST` | `/notifications/read-all` | 标记当前用户的全部通知为已读。 |

所有接口都要求 JWT，用户 ID 只取自当前认证上下文。用户端铃铛会在页面可见时周期刷新；这属于站内通知，
不是邮件、短信或浏览器 Push。

## 管理端批量治理

帖子治理表格支持表头选择当前页，然后批量置顶、锁定、审核、下架、恢复、回收或永久清除。批量入口不会绕过
单项治理 API：每条内容仍分别执行 Permission Code、板块作用域、状态机、可靠事务和审计检查。操作可能部分
成功，界面会汇总成功与失败数量；切换筛选、分页或列表时会清除选择。

## 回复

创建回复：

```http
POST /api/v1/threads/:thread_id/posts
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "content": "收到，我会参加。",
  "parent_id": null
}
```

回复另一层时把 `parent_id` 设置为目标回复 ID。服务会为回复分配楼层号，并在创建时把父回复楼层号快照到
`parent_floor_number`；父回复之后被删除或不在当前分页时，引用处的"回复：第 N 楼"仍显示原楼层。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/threads/:id/posts` | 公开回复列表。 |
| `GET` | `/threads/:id/posts/me` | 按当前用户可见性读取。 |
| `PUT` | `/threads/:id/posts/:post_id` | 回复作者更新。 |
| `DELETE` | `/threads/:id/posts/:post_id` | 回复作者删除。 |

## 富文本文章

富文本文章与普通帖子共用 `threads` 作为列表入口，业务类型固定为 `article`，正文和图片元数据使用独立表。主要流程为：

1. `POST /richtext/articles` 创建草稿。
2. `POST /richtext/assets` 上传图片。
3. `GET /richtext/assets/me` 读取当前上传者的只读图片库存。
4. `PUT /richtext/articles/:id` 保存正文。
5. `POST /richtext/preview` 预览清洗后的 HTML。
6. `POST /richtext/articles/:id/publish` 发布。

创建、保存、发布、下线和删除图文文章时，基础 Thread、Revision、Article Detail、命令审计和 Outbox
会在同一个可靠事务中提交。详情保存失败时不会留下回收站 Thread；文件上传仍独立于数据库事务，并受
User Storage 的路径和配额规则约束。

`GET /richtext/assets/me` 仅返回当前认证用户上传的资源元数据和 URL；它不会列出其他用户文件，也不提供删除，
因为一张图片可能仍被已发布的文章引用。普通帖子正文图片的对应只读入口是
`GET /content/assets/images/me`。

富文本 HTML 会在后端清洗。客户端不能通过自定义标签、脚本或事件处理器绕过清洗规则。
