# v0.10 原始计划讨论（非正式计划）

> 文档生命周期：原始输入，已被 [v0.10 正式计划](00-v0.10版本计划书.md) 和
> [最终审计](03-v0.10最终全方位审计与后续路线.md) 替代。

message time: 2026-07-15 17:46:42

刚才  https://chatgpt.com/share/6a547e07-be70-83ea-84f1-f5a30116adf2中布置的v0.9计划已完成，github上已经同步了项目代码了，我刚给你v0.9计划的验收审查报告，但经过用户实测，通过ui界面使用测试发现了以下几个问题，需要优先解决
1、用户的插件中心目前怎么什么都没有，
2、用户端的插件示例是什么作用，可以在页面上通俗点说明吗（普通用户看不懂）
3、个人主页中的同步板块和同步标签是什么作用，自己是自己发的帖子都应该同步到自己空间，应该说是和全部帖子是一个接口，但是多一个查询条件？不需要硬性复制、类似于软链接？
4、关于pc端和移动端的前端页面适配问题，感觉不是单纯的将页面改成grid之类的布局，而是从逻辑上可以识别到当前屏幕的长宽比，然后使用不同的布局适配，（例如课表的在手机端上的布局应该将宽度降低，然后同比缩放，注意不是像素缩放，而是从代码上改变字体和单元边框等，因为课表内容不会影响它所占屏幕的空间，故可以舍弃滚动的布局）
5、管理后台 -- 列子管理 中对图文下架和删除有什么区别，删除对应数据库中是将其不可见吗，我将user1的图文帖子下架后，发现在全部帖子中看不到了，但是在用户个人主页中可以看到，点击编辑再发布后，又出来了，这部分的逻辑是不是有问题，图文如果下架了在发布，应该要经过审核吧，而且个人主页应该也要能发现被下架了
6、管理后台 -- 外部插件和插件中心是不是一个是已经安装的和插件市场，这个是不是可以整合到一个标签页中，可以使用多级标签页，例如在插件这个一级标签下有 已安装插件（包括启动和未启动的）、插件市场、mcp工具、skills等），从逻辑上内置功能是否需要也放入插件中，一个系统插件一个外部插件？
7、管理后台 -- 集成中心 中的webhook、mcp只读工具、message Local能用吗，该如何使用？可不可以在类似需要配置的页面上增加一个说明文档的跳转链接（图表+按钮），跳转到 campusos文档 对应的目录或文档中
8、不仅是插件的标签页，像用户管理、版主管理、再加上一个权限设置（定义角色与其对应的权限、目前可以暂时不做太多的开放，但在数据库应该有个权限的对应表，给每个接口注册一个id，权限表中可以通过角色与对应id集合来完成对权限的控制），可以合并成一个用户的一级标签；帖子管理和帖子审核可以合并成帖子的一级标签
9、http://localhost:8080/api/v1 可以直接访问吗，显示的404 page ，如果不能直接访问，是否可以在访问时先是一个提醒页面
10、还有，目前需要对项目的架构进行整理，规划好各个文件目录的功能与边界，整理其中文件，确定是否都在正确的位置，都不是垃圾文件（例如已经不用了的插件），完整项目架构的整理后，还要对当前数据库的规划进行整理，要求也是规划好各个表的功能与边界，确保没有闲置的数据表来干扰开发|
请结合我给你的v0.9计划的验收审查报告、用户实测出的问题和项目仓库代码 开始深度思考与规划，给出v0.10计划的思路

---

# chatgpt response

我会先核对 v0.9 验收报告中的既有边界，再检查 GitHub 当前仓库的前后端、插件、帖子状态、权限与数据库结构，最后按优先级整理成可直接执行的 v0.10 分阶段计划。

v0.9 报告明确：插件底层能力已经具备，但“用户看到什么、如何理解、状态如何闭环”仍未完成；v0.10 应把重点从继续堆插件能力，转向产品语义、状态机和架构治理。

已确认插件中心为空不是单纯前端渲染问题：用户端只展示 `published` 的 v2 外部插件，而仓库中的 v2 示例仅是开发样例、没有可执行 Runtime，也不会自动安装或发布，因此默认环境很容易显示空列表。

下面给出基于 v0.9 验收报告、用户实测问题和当前 `main` 分支代码形成的 v0.10 规划思路。

**Document Title: **

# CampusOS v0.10 计划思路

## 一、v0.10 的总体定位

### 1. 版本主题

**CampusOS v0.10：产品语义闭环、统一治理与自适应多端**

v0.9 已经完成插件 Manifest v2、受管数据、用户授权、本地插件目录、运行时门禁和多端基础回归等底层能力，但最终数据库动态检查和修改后的五视口回归尚需在完整环境中补跑。

因此，v0.10 不应继续以“增加更多插件底层接口”为主，而应集中解决以下问题：

1. 用户能否理解和实际使用现有能力。
2. 帖子、个人主页、审核、下架和删除是否遵循同一套状态规则。
3. PC、平板、手机端是否真正采用不同的信息布局。
4. 管理后台是否按照业务领域组织，而不是不断增加平级菜单。
5. 文件目录、模块边界、API 权限和数据库表是否具有明确归属。
6. 系统是否能够持续发现无主文件、无主接口、闲置表和失效插件。

### 2. v0.10 的五条主线

| 主线 | 对应问题 |
|---|---|
| 内容状态与个人主页统一 | 3、5 |
| 插件产品化与扩展中心 | 1、2、6 |
| 自适应多端布局 | 4 |
| 后台信息架构、权限和集成治理 | 7、8、9 |
| 项目与数据库架构治理 | 10 |

---

# 二、对十个实测问题的判断

## 1. 用户插件中心为什么是空的

### 当前原因

用户插件中心并不是展示所有内置功能，也不是展示仓库中的示例目录，而是只展示：

- 已经安装；
- 使用 Manifest v2；
- 不属于 Built-in；
- 已被管理员设置为 `published`；

的外部插件。

当前 Catalog 同步代码会跳过 Built-in 和非 v2 插件，新发现的插件默认进入 `draft`，用户端只查询 `published`。

仓库中的 `v2-managed-example` 只是 Manifest 示例，README 明确说明其不包含可执行 gRPC 二进制，因此不能作为实际可运行插件自动出现在用户插件中心。

### v0.10 处理方案

建立三种环境策略：

#### 开发和演示环境

提供至少一个完整可运行的官方示例插件，例如：

- 校园个人便签；
- 校园公告订阅；
- 帖子收藏夹；
- 每日课程提醒。

该插件必须包含：

- 可执行 Runtime；
- 中文 Manifest；
- 普通用户说明；
- 测试数据；
- 前端入口；
- 数据授权示例；
- 卸载和数据保留说明。

开发环境可以通过初始化命令执行：

```bash
make seed-demo-plugins
```

完成安装和发布，但不能在生产环境中静默安装代码。

#### 生产环境

插件市场为空时，不只显示“暂无插件”，而应显示：

- 当前没有管理员发布的插件；
- 插件需要由管理员审核安装；
- 如何向管理员申请；
- 查看插件说明文档；
- 系统内置功能不在此页面显示。

#### 管理后台

增加清晰的发布流程提示：

```text
导入插件包
→ 安全预检
→ 安装
→ 启动 Runtime
→ 发布到本地插件目录
→ 用户可见并申请授权
```

---

## 2. 插件示例普通用户看不懂

当前插件页面主要展示 `managed_data`、`plugin_file`、`plugin_search`、resource/action 等开发者概念。

v0.10 应把插件信息拆成两层。

### 普通用户默认看到

- **这个插件能做什么**
- **适合什么场景**
- **需要读取哪些数据**
- **会保存哪些数据**
- **关闭后数据是否保留**
- **是否会在其他用户面前显示内容**
- **是否由 CampusOS 官方提供**

例如将当前示例说明改为：

> 该插件用于演示如何在 CampusOS 中保存个人笔记、添加文本附件，并允许用户选择是否将笔记加入站内搜索。它不会读取私信、密码或其他用户的数据。

### 高级信息折叠展示

点击“查看技术权限”后再显示：

```text
managed_data: read/write/delete
plugin_file: read/write/delete
plugin_search: read
```

### Manifest v0.10 建议增加字段

```yaml
user_experience:
  summary: 保存个人笔记并添加文本附件
  use_cases:
    - 记录校园事务
    - 保存课程备忘
  data_usage:
    - 保存用户主动创建的笔记
    - 保存用户主动上传的 txt 文件
  does_not_access:
    - 私信
    - 密码
    - 其他用户的私人数据
  audience: ordinary-user
  official: true
  docs_url: /plugins/official-notes
  screenshots: []
```

---

## 3. 个人主页的同步版块、同步标签

用户提出的理解基本正确：

> 用户自己发布的帖子本来就属于该用户，个人主页应当通过同一帖子数据源增加作者查询条件，而不是再复制一份帖子。

当前 `user_space_contents` 被定义为个人主页同步内容的缓存或投影，保存 `user_id`、`thread_id`、`category_id` 和同步时间。

这不是完整复制正文，但仍会产生以下问题：

- 原帖子状态变化后缓存可能未及时更新；
- 下架内容可能仍留在个人主页；
- 删除、私密、审核状态需要额外同步；
- “同步”容易让用户误解成数据复制。

### v0.10 推荐模型

以 `threads` 为唯一内容事实源：

```text
全部帖子：
ListThreads(status、category、tag、keyword)

个人主页：
ListThreads(
  author_id = 当前主页用户,
  publication_status,
  moderation_status,
  deletion_status,
  category_ids,
  tags
)
```

个人主页只保存：

- 主页标题和简介；
- 可见性；
- 布局；
- 是否展示发帖；
- 版块筛选条件；
- 标签筛选条件；
- 风格配置。

### UI 文案调整

将：

- 内容同步
- 同步版块
- 同步标签

改成：

- **在个人主页展示我的帖子**
- **展示哪些版块的帖子**
- **展示哪些标签的帖子**

增加说明：

> 个人主页不会复制帖子，而是从你的全部发帖中按条件展示。留空表示展示全部符合公开条件的帖子。

### `user_space_contents` 的处理

v0.10 不应立即直接删除该表，应先完成：

1. 确认所有读写调用。
2. 将个人主页查询切换到 `threads`。
3. 停止生成新的投影记录。
4. 运行一致性比较。
5. 确认无依赖后，通过新 migration 废弃或删除。

若未来因为性能必须保留，则应明确命名为投影表，例如：

```text
user_space_content_projection
```

并增加：

- `source_version`
- `visibility_state`
- `moderation_state`
- `last_projected_at`

以及自动修复任务。

---

## 4. PC 和移动端页面适配

当前课表的移动端策略仍主要依赖横向滚动。周课表列宽设置为每列至少 `116px`，移动端仍至少为 `112px`，并保留 `overflow: auto`。 

用户提出的问题成立：真正的多端适配不能只把页面从两列变成一列。

### v0.10 自适应模型

不能只判断“是不是手机”，应综合使用：

- 容器可用宽度；
- 容器高度；
- 屏幕纵横比；
- 横屏或竖屏；
- 触控还是鼠标；
- 字体缩放；
- 安全区域；
- 用户是否开启减少动画。

建议定义统一布局模式：

```ts
type LayoutMode =
  | 'compact-portrait'
  | 'compact-landscape'
  | 'regular'
  | 'wide'
```

优先使用：

- CSS Container Queries；
- `ResizeObserver`；
- `matchMedia`；
- CSS 变量；
- 组件级布局策略。

纵横比只作为辅助条件，不能替代宽度判断。

### 手机端课表方案

周课表应尽量在屏幕宽度内完整展示，而不是对整个页面使用 `transform: scale()`。

根据容器宽度动态调整：

- 节次列宽；
- 每日列宽；
- 字体大小；
- 单元格内边距；
- 课程卡片圆角；
- 边框宽度；
- 信息显示层级。

手机端课程卡只显示：

- 课程简称；
- 上课节次；
- 教室缩写。

教师、完整地点、周次和备注放入点击后的抽屉。

对于七天课表，可提供：

- 工作日；
- 周末；

两个分页视图，或者在横屏模式下展示完整七天。

### 明确禁止

v0.10 不应采用：

- 整页像素缩放；
- CSS `transform: scale()` 模拟响应式；
- 单纯依赖 User-Agent；
- 所有复杂页面统一改成 Grid；
- 手机端默认保留大范围横向滚动。

---

## 5. 图文下架、删除和重新发布逻辑

### 当前下架逻辑

管理员下架图文时：

- 图文正文状态变为 `offline`；
- Thread 状态变为 `archived`。

但是用户编辑文章后，文章会转为草稿；用户再次点击发布时，Publish 会直接把文章和 Thread 都设为 `published`，没有检查它是否曾被管理员下架。

这正是实测中“下架后编辑再发布即可重新出现”的根本原因。

### 当前删除逻辑

从富文本服务来看，删除至少会：

- 将文章状态设置为 `deleted`；
- 再调用 Community 的 `DeleteThread`。

v0.10 需要进一步核查 `DeleteThread` 最终是物理删除还是逻辑删除。当前管理端提示“不可恢复”，但在完成数据库行为审计前，不应继续使用含糊文案。

### v0.10 状态模型

不建议继续使用单个 `status` 字段承载所有含义，应拆成三个维度：

```text
publication_status:
- draft
- published
- private

moderation_status:
- clear
- pending
- rejected
- taken_down

deletion_status:
- active
- trashed
- purged
```

公开可见条件统一为：

```text
publication_status = published
AND moderation_status = clear
AND deletion_status = active
```

### 下架后的正确流程

```text
已发布
→ 管理员下架
→ taken_down
→ 用户可看到下架原因
→ 用户编辑整改
→ pending
→ 管理员审核
→ published + clear
```

用户不能通过普通 Publish 接口绕过审核。

### 管理员恢复

管理员可以执行明确的“直接恢复”，但需要：

- 单独权限；
- 填写恢复原因；
- 写入审计日志；
- 记录操作人；
- 记录恢复到哪个内容版本。

### 个人主页显示

对普通访客：

- 下架内容完全不可见。

对作者本人：

- 保留卡片；
- 显示“已被下架”；
- 显示下架原因；
- 提供“编辑并重新提交审核”。

### 删除建议

将普通删除改为：

- **移入回收站**：逻辑删除，可恢复；
- **永久清除**：高权限操作，经过二次确认和审计。

### 建议新增数据表

```text
content_moderation_cases
content_moderation_actions
content_revisions
```

至少记录：

- 内容 ID；
- 内容版本；
- 操作类型；
- 操作原因；
- 操作人；
- 操作前状态；
- 操作后状态；
- 创建时间。

---

## 6. 外部插件、插件中心和内置功能是否合并

当前管理端将“外部插件”“插件中心”“内置功能”作为三个平级菜单。

从产品信息架构上，应当合并。

从后端生命周期和安全模型上，不能完全合并。

当前项目已经明确区分：

- Core Module；
- Built-in Feature；
- External Plugin；
- Resource Package。

### v0.10 推荐后台结构

一级菜单：

```text
扩展与集成
```

二级标签：

```text
扩展总览
├── 已安装插件
├── 插件市场
├── 内置功能
├── MCP 工具
├── Skills
├── 资源包
└── 安装申请与审计
```

### 后端仍然保持不同生命周期

| 类型 | 是否可卸载 | 是否有独立 Runtime | 是否进入插件市场 |
|---|---:|---:|---:|
| Core Module | 否 | 否 | 否 |
| Built-in Feature | 否 | 通常否 | 否 |
| External Plugin | 是 | 是 | 是 |
| Resource Package | 是 | 否 | 可进入资源目录 |
| Skill | 取决于实现 | 可能有 | 独立目录 |
| MCP Tool | 取决于来源 | 可能有 | 不等同于插件 |

可以建立统一的只读 `Extension Inventory`，供前端聚合展示，但不能把所有对象强行放入同一张插件生命周期表。

---

## 7. Webhook、MCP 只读工具和 Message Local 是否可用

从当前管理端代码看，这三部分已经有实际接口和操作入口：

- Webhook 支持创建、启停、测试和投递记录；
- MCP 工具支持列出、启停和调用；
- Message Local 支持模拟本地入站消息和查看日志。

MCP 当前实际提供：

- `health.check`
- `categories.list`
- `threads.list`
- `threads.get`

并且只返回公开帖子。 

但当前 MCP 是 **CampusOS 内部的 MCP-like 只读工具接口，不是标准 MCP Server**。

Message Local 是本地测试适配器，不是 QQ、微信、Discord 或 OneBot 的正式连接器。

### v0.10 页面需要增加能力成熟度

每个集成卡片显示：

- 可用状态；
- 是否已配置；
- 适用场景；
- 是否仅供开发测试；
- 所需权限；
- 调用方式；
- 最近一次成功时间；
- 最近一次错误；
- 查看文档；
- 执行测试。

示例：

```text
Message Local
状态：可用
成熟度：开发测试
作用：模拟外部聊天平台向 CampusOS 发送消息
不适用于：生产环境真实消息平台连接
```

### 文档跳转机制

不要在每个 Vue 页面中散落硬编码 URL。

建立统一文档注册表：

```ts
docsRegistry = {
  webhook: '/integrations/webhook',
  mcpTools: '/integrations/mcp-tools',
  messageLocal: '/integrations/message-local',
  pluginInstall: '/plugins/package-import'
}
```

每张配置卡统一包含：

- 文档图标；
- “使用说明”按钮；
- “配置示例”按钮；
- “常见错误”按钮。

---

## 8. 用户、版主、权限以及帖子管理的后台合并

### 推荐信息架构

```text
用户与权限
├── 用户管理
├── 版主管理
├── 角色管理
├── 权限矩阵
└── 授权记录

内容治理
├── 帖子列表
├── 待审核
├── 下架内容
├── 已拒绝内容
├── 回收站
├── 版块与标签
└── 操作记录
```

使用嵌套路由实现，不应把所有功能塞进一个超大 Vue 文件。

例如：

```text
/admin/users/list
/admin/users/moderators
/admin/users/roles
/admin/users/permissions

/admin/content/threads
/admin/content/reviews
/admin/content/taken-down
/admin/content/trash
```

### 权限设计判断

用户提出“给每个接口注册一个 ID”的方向是合理的，而且项目已经部分具备。

当前路由审计器会为接口生成稳定描述符 ID：

```go
httpapi.<method>.<route>
```

并关联模块、Audience 和 `resource:action` 权限。

当前后台路由也已经显式声明了诸如：

```text
thread:delete
richtext:moderate
plugin:install
mcp:call
role:assign
```

等权限。

### v0.10 不应只使用数字 ID

数据库可以有数字主键，但业务代码和审计必须使用稳定权限编码：

```text
community.thread.read
community.thread.take_down
community.thread.restore
community.thread.delete
identity.user.suspend
plugin.package.install
integration.webhook.execute
```

建议结构：

```text
permission_definitions
- id
- code
- resource
- action
- description
- risk_level
- scope_type
- deprecated_at

role_permissions
- role_id
- permission_id

route_permission_bindings
- route_id
- permission_id
```

其中：

- `id` 用于数据库关联；
- `code` 用于代码、日志和文档；
- `route_id` 使用稳定接口 ID；
- 一个权限可以保护多个接口；
- 一个接口原则上对应一个明确的权限要求。

v0.10 初期可以先开放：

- 系统角色查看；
- 权限矩阵查看；
- 角色权限调整；
- 用户角色分配；
- 版主作用域分配。

不必立即开放任意复杂条件权限表达式。

---

## 9. `/api/v1` 访问后显示 404

当前 Router 只创建了 `/api/v1` Group，但没有为 Group 根路径注册处理器，所以访问 `/api/v1` 返回 404 是当前代码的自然结果。

### v0.10 推荐行为

#### API 客户端请求

请求：

```http
Accept: application/json
```

返回：

```json
{
  "service": "CampusOS API",
  "api_version": "v1",
  "status": "available",
  "health": "/api/v1/health",
  "documentation": "/docs/api",
  "openapi": "/docs/openapi.json"
}
```

#### 浏览器直接访问

请求接受 `text/html` 时，返回一个简单提示页：

```text
CampusOS API v1

这是 CampusOS 后端接口入口，不是普通用户页面。

[查看 API 文档]
[检查服务状态]
[进入 CampusOS]
```

#### API 404

对 `/api/*` 的不存在路由，统一返回 JSON：

```json
{
  "code": "API_ROUTE_NOT_FOUND",
  "message": "请求的 API 路径不存在",
  "path": "/api/v1/example",
  "documentation": "/docs/api"
}
```

前台路由和 API 路由的 404 处理必须分开。

---

## 10. 项目目录和数据库架构整理

这是 v0.10 中风险最高、但必须开展的工作。

### 首要原则

不能边看边删，也不能因为静态搜索没有结果就直接删除文件或数据表。

必须依次执行：

```text
清点
→ 分类
→ 确认所有者
→ 确认运行依赖
→ 标记弃用
→ 迁移调用
→ 回归验证
→ 删除
```

### 文件分类

每个文件或目录必须归入以下类别之一：

- 源代码；
- 测试；
- 合同；
- Migration；
- 文档；
- 示例；
- 构建产物；
- 运行时数据；
- 兼容代码；
- 已弃用；
- 未确认。

### 应重点审查的目录

```text
admin/dist/
web/dist/
docs-site/.vitepress/.temp/
data/plugins/
data/plugin_data/
examples/plugins/
internal/homepage/
internal/webtheme/
internal/plugin/
docs/Todo/
```

当前 `.gitignore` 已忽略 `web/dist` 和 docs-site 的 `cache/dist`，但没有覆盖 `admin/dist/` 和 `docs-site/.vitepress/.temp/`，应在 v0.10 明确构建产物管理政策。

### 架构清单

为每个后端模块记录：

```text
模块 ID
源码目录
业务职责
依赖模块
提供的 Port
使用的 Adapter
拥有的 API
拥有的数据表
拥有的数据目录
启停方式
测试位置
文档位置
状态：active/compatibility/deprecated
```

### 数据表清单

为每张表记录：

```text
表名
所属业务域
创建 Migration
主要写入模块
主要读取模块
主键
外键或逻辑关联
索引
数据保留策略
是否包含敏感数据
是否仍有运行时访问
状态：active/projection/compatibility/deprecated
```

### v0.10 应重点审查的数据库问题

#### `user_space_contents`

可能被统一作者查询替代，是优先投影废弃候选。

#### `tags` 与 `threads.tags`

当前既有标签字典表，又在 Thread 中保存标签数组，需要确定唯一权威模型，避免长期双轨。当前架构说明已经明确两者并存。

#### 插件相关表

检查：

- `plugins`
- `plugin_permissions`
- `plugin_records`
- `plugin_file_metadata`
- `plugin_user_grants`
- `plugin_catalog_entries`
- `plugin_install_requests`
- `plugin_releases`
- `plugin_market_audits`

确认哪些是运行状态，哪些是目录治理，哪些是审计，不应因为名称相似而合并。

#### 实验性集成表

检查：

- `mcp_audit_logs`
- `message_bindings`
- `message_logs`

如果功能保留，应明确标记成熟度；如果未来废弃，应先停止写入并提供数据迁移或清理策略。

### Migration 原则

- 不修改已经发布和执行的历史 Migration；
- 不删除旧 Migration 文件；
- 表重命名、字段调整和删除都通过新增 Migration；
- 删除前至少经历一个弃用版本；
- Down Migration 必须说明数据能否恢复；
- 数据库清理和大规模目录移动不能放在同一个提交中。

---

# 三、v0.10 目标架构

## 1. 内容事实源

```text
threads
  ├── 普通帖子正文
  ├── richtext_article_contents
  ├── content_revisions
  └── content_moderation_cases

所有帖子页 ───────┐
个人主页 ─────────┼── Unified Content Query
审核后台 ─────────┤
MCP 公开帖子查询 ─┘
```

所有入口必须复用统一可见性策略。

## 2. 扩展能力模型

```text
Extension Inventory
├── Built-in Feature
├── External Plugin
├── Resource Package
├── MCP Tool
└── Skill
```

Inventory 只负责统一展示，不统一篡改各自生命周期。

## 3. 权限模型

```text
HTTP Route
→ Stable Route ID
→ Permission Code
→ Role Permission
→ User Role + Scope
→ Authorization Decision
→ Audit Log
```

## 4. 响应式模型

```text
Viewport + Container + Orientation + Pointer
→ Layout Capability
→ Component Layout Mode
→ Content Density
```

不是：

```text
设备名称
→ 固定 CSS
```

---

# 四、v0.10 分阶段执行计划

## A0：关闭 v0.9 最终验收并冻结基线

### 工作

补跑：

```bash
make migrate-status
make database-check
STOP_EXISTING=true make dev-all
RUN_RESTORE_DRILL=true RUN_BROWSER_SMOKE=true make release-check
```

### 输出

- v0.9 最终验收记录；
- v0.10 基线 Commit；
- 数据库备份；
- `data/` 备份；
- 当前路由、表和插件快照。

### 验收

v0.10 开发开始前，必须能够明确区分：

- v0.9 原有问题；
- v0.10 新增修改引起的问题。

---

## A1：建立十个实测问题的自动化复现

### 工作

将用户实测场景写成 E2E 或集成测试。

重点场景：

```text
user1 发布图文
→ 管理员下架
→ 全部帖子不可见
→ 作者主页显示下架状态
→ 作者编辑
→ 再次发布
→ 必须进入待审核
→ 管理员审核后恢复公开
```

### 验收

每一个实测问题都有：

- 复现步骤；
- 预期行为；
- 自动化测试；
- 对应责任模块。

---

## A2：完成项目、API 和数据库架构清点

### 工作

生成：

- 文件归属清单；
- Go 模块依赖图；
- 前端模块和路由清单；
- API 所有权清单；
- 权限清单；
- 数据表读写清单；
- 插件和兼容模块清单；
- 构建产物清单。

### 输出

```text
docs/architecture/v10-current-inventory.md
docs/architecture/v10-target-architecture.md
docs/database/v10-schema-catalog.md
docs/adr/
```

### 验收

不存在“无法说明用途”的活跃目录、接口和数据表。无法确认的对象进入待调查清单，而不是直接删除。

---

## A3：重构内容状态机与审核流程

### 工作

- 拆分发布、审核和删除状态；
- 引入内容版本；
- 引入下架原因；
- 下架内容重新发布必须审核；
- 统一管理员恢复和用户重新提交；
- 引入回收站；
- 所有操作写入审计。

### 验收

- 用户无法绕过管理员下架；
- 管理员操作可追踪；
- 内容状态转换都有后端校验；
- 前端不能通过直接构造状态值绕过流程。

---

## A4：统一个人主页与帖子查询

### 工作

- 引入统一 Content Query；
- 个人主页使用 `author_id` 查询；
- 版块和标签变为展示筛选条件；
- 公共访客和作者本人使用不同可见性策略；
- 停止依赖旧同步投影；
- 制定 `user_space_contents` 废弃计划。

### 验收

帖子状态变化后：

- 全部帖子；
- 个人主页；
- 搜索；
- MCP 查询；
- 管理后台；

结果保持一致。

---

## A5：提供可运行的官方示例插件

### 工作

制作一个完整插件，而不是只有 Manifest：

- Go 或 Wasm Runtime；
- 用户页面；
- 中文说明；
- 数据授权；
- 文件示例；
- 测试；
- 安装包；
- 签名；
- 卸载验证。

### 验收

开发环境执行一条初始化命令后：

- 管理后台能看到插件；
- 能发布至插件目录；
- 用户插件中心能看到；
- 用户能理解用途；
- 用户能授权、撤销、导出和删除数据。

---

## A6：插件中心普通用户体验改造

### 工作

- 用户说明与技术权限分层；
- 空状态引导；
- 官方、第三方、实验性标签；
- 使用场景；
- 数据使用说明；
- 风险等级解释；
- 关闭和卸载后的数据说明；
- 文档链接；
- 安装申请状态。

### 验收

不懂 Plugin、Manifest 和 Host API 的普通用户，也能判断：

- 插件有什么用；
- 是否需要启用；
- 会读取什么；
- 如何关闭；
- 数据会不会消失。

---

## A7：建立统一响应式能力层

### 工作

增加：

```text
useLayoutCapability()
Container Query tokens
Responsive density tokens
Orientation-aware layout
Pointer-aware controls
```

定义统一的：

- 间距；
- 字号；
- 触控目标；
- 抽屉宽度；
- 表格切换策略；
- 卡片密度。

### 验收

前端页面不再各自随意定义一套手机断点。

---

## A8：完成课表和重点页面移动端重构

### 第一批页面

- 课表；
- 个人主页设置；
- 插件中心；
- 帖子详情；
- 图文编辑器；
- 管理后台帖子表格。

### 固定回归视口

至少包括：

```text
320 × 568
360 × 800
393 × 852
768 × 1024
1024 × 768
1366 × 768
852 × 393 横屏
```

### 课表验收

- 手机周课表默认无横向滚动；
- 字体、边框、间距由代码适配；
- 课程信息不会重叠；
- 点击课程可查看完整信息；
- 横屏能使用更高密度布局；
- 不使用整体像素缩放。

---

## A9：重构管理后台信息架构

### 工作

建立一级业务域：

```text
用户与权限
内容治理
扩展与集成
系统与运维
```

采用嵌套路由和二级标签。

### 验收

- 外部插件、市场和内置功能不再占据三个平级菜单；
- 帖子管理与审核位于同一业务域；
- 用户、版主和权限位于同一业务域；
- 刷新子标签页后能够保持当前路由；
- 每个标签仍可独立懒加载。

---

## A10：完善权限定义和接口注册体系

### 工作

- 引入稳定权限 Code；
- 权限定义入库；
- 路由与权限绑定；
- 生成权限矩阵；
- 增加风险等级；
- 增加作用域类型；
- 提供只读和基础编辑后台。

### 验收

每个管理接口都能回答：

- 接口 ID 是什么；
- 属于哪个模块；
- 需要什么权限；
- 哪些角色拥有；
- 是否支持版块作用域；
- 操作是否需要审计。

---

## A11：集成中心成熟度和文档体系

### 工作

- Webhook、MCP、Message 增加成熟度；
- 增加配置状态；
- 增加测试状态；
- 增加文档按钮；
- 增加调用示例；
- 增加最近错误；
- 明确 MCP-like 与标准 MCP 的区别；
- 明确 Message Local 仅为测试适配器。

### 验收

管理员不阅读源码也能完成 Webhook 测试，并能理解 MCP 和 Message Local 的用途及边界。

---

## A12：API 入口和错误响应规范化

### 工作

- 注册 `GET /api/v1`；
- 增加 JSON API Index；
- 增加浏览器提示页；
- 增加 API NoRoute；
- 增加统一错误码；
- 增加 Docs/OpenAPI 跳转。

### 验收

浏览器访问 `/api/v1` 不再看到无解释的 `404 page not found`。

---

## A13：执行项目目录清理

### 工作

- 移除或忽略构建临时文件；
- 统一前端构建产物政策；
- 整理废弃插件；
- 整理 Legacy Compatibility；
- 移动文件前增加 Import Boundary 检查；
- 为每个主要目录添加 README 或 Ownership 定义；
- 删除前先完成弃用和回归。

### 验收

CI 增加：

```bash
make generated-files-check
make architecture-check
make frontend-boundary-check
make dead-code-check
make plugin-inventory-check
```

---

## A14：执行数据库整理和迁移

### 工作

- 处理个人主页投影表；
- 决定标签唯一事实源；
- 完善审核表和版本表；
- 引入权限定义表；
- 清理确认无用的数据表；
- 补充索引和约束；
- 执行数据迁移和恢复演练。

### 验收

每张数据表都有：

- 所属模块；
- 明确用途；
- 读写路径；
- 数据保留策略；
- Migration 来源；
- 测试；
- 备份和恢复说明。

---

## A15：v0.10 全量验收

执行：

```bash
go test ./... -count=1
pnpm test
pnpm build
make contracts-check
make architecture-check
make database-check
make docs-links
make frontend-budget
make release-check
```

并完成：

- 七视口浏览器回归；
- 用户、版主、管理员三角色回归；
- 插件安装、发布、授权、停用、卸载回归；
- 图文下架、整改、审核、恢复、删除回归；
- 数据库升级和回退演练；
- `data/` 与 PostgreSQL 联合恢复演练。

---

# 五、优先级建议

## P0：必须先解决

1. 图文下架后可绕过审核重新发布。
2. 个人主页与公共帖子状态不一致。
3. 插件中心默认空白且没有用户引导。
4. `/api/v1` 无解释 404。
5. 建立架构和数据库清单，防止后续继续制造重复结构。

## P1：v0.10 主体能力

1. 课表移动端真正自适应。
2. 插件示例和权限说明普通用户化。
3. 管理后台业务域整合。
4. 权限定义和接口绑定。
5. 集成中心文档与成熟度。

## P2：在事实清点后执行

1. 大规模目录移动。
2. 删除旧插件。
3. 删除或合并数据表。
4. 清理 Legacy Compatibility。
5. 清理构建产物和历史临时文件。

---

# 六、v0.10 明确不应采用的方案

1. 不在生产环境自动安装或发布示例插件。
2. 不把 Built-in Feature 和 External Plugin 强行改成同一种生命周期。
3. 不继续把个人主页理解为帖子数据复制。
4. 不依赖前端隐藏按钮保证下架状态。
5. 不允许用户通过 Publish 接口跳过审核。
6. 不使用整页缩放解决手机课表。
7. 不只用数字权限 ID，必须保留稳定权限 Code。
8. 不直接修改或删除历史 Migration。
9. 不在未确认读写路径前删除数据表。
10. 不在同一个 PR 中同时进行大规模目录重构和数据库清理。

---

# 七、v0.10 最终成功标准

v0.10 完成后，CampusOS 应达到以下状态：

- 普通用户打开插件中心时知道这里是什么、为什么可能为空、插件有什么作用。
- 个人主页只是用户内容的筛选视图，不再与帖子事实源产生状态分裂。
- 被管理员下架的内容不能自行恢复公开。
- 作者能够看到下架原因并重新提交审核。
- 手机课表无需横向滚动即可完成主要查看操作。
- 管理后台按照用户、内容、扩展、系统四个业务域组织。
- 每个管理接口都具有稳定接口 ID 和权限 Code。
- Webhook、MCP 和 Message Local 都有清晰用途、成熟度和文档。
- `/api/v1` 提供明确 API 入口说明。
- 每个目录、模块、接口和数据表都有明确所有者。
- 无法确认用途的文件和数据表不会继续静默存在。
- 架构、数据库、权限和文档之间能够通过 CI 自动校验。

v0.10 的核心不是继续增加功能数量，而是让 CampusOS 已经拥有的能力形成一致、可解释、可维护、可验收的完整产品。

下一步建议将该思路固化为 `00-v0.10版本计划书.md`，再按 A0–A15 拆分独立验收标准和 AI 执行提示词。
