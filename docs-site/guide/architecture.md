# 系统架构

## 请求链路

浏览器不直接访问数据库或插件数据。用户前台和管理后台统一调用 `/api/v1`，由后端完成认证、权限、对象归属和数据范围检查。

```text
browser
  -> Gin middleware
  -> identity and permission check
  -> domain service
  -> PostgreSQL / cache / local file storage
  -> event bus and plugin runtime
```

前端隐藏按钮只用于改善体验，不是安全边界。即使客户端手工构造请求，后端仍必须重新授权。

## 后端模块

| 模块 | 职责 |
| --- | --- |
| `internal/modules/core/identity` | 用户、账号、JWT、角色、稳定权限目录、作用域、授权审计、TOTP MFA 和受控恢复。 |
| `internal/modules/core/userstorage` | 用户目录、安全路径、配额、私有 Object Port、持久 Reservation 账本以及按批次/可恢复 checkpoint 的本地对象对账。 |
| `internal/modules/core/academicterm` | 管理员治理 spring/fall 学期、第一周、open/closed/default 生命周期和乐观锁。 |
| `internal/modules/core/contenteditor` | 文本、Markdown 和 CampusDoc v1 的校验、清洗与安全渲染；不依赖帖子或文件系统。 |
| `internal/modules/core/community` | 版块、帖子、回复、标签、内容治理状态机和统一 `ContentQuery`。 |
| `internal/modules/core/moderation` | 版块版主作用域、治理动作和审计。 |
| `internal/platform/observability` | 有界指标、Admin 摘要、运行时与数据库池快照，以及默认关闭的 loopback Prometheus 导出。 |
| `internal/plugin` | External Plugin Catalog、Runtime、Lifecycle、配置、UI/Event 与审计 Facade。 |
| `internal/plugin/hostapi` | 插件访问主系统能力的受控边界。 |
| `internal/modules/features/personalspace` | 个人主页、头像、主页所有者风格和 Community 内容查询适配。 |
| `internal/modules/features/appearance/stylepack`、`internal/modules/features/appearance/webtheme` | 目标作用域筛查、沙箱特效声明和管理员系统主题目录。 |
| `internal/modules/features/richtext` | 富文本草稿、清洗、发布和图片资产。 |
| `internal/modules/features/schedule` | 受管学期课表、日历计算和表格导入；新版本以私有 Object 和乐观锁引用保存，JSON 仅保留为兼容镜像。 |
| `internal/modules/features/personaldocuments` | 私有文档、不可变版本、回收站、认证下载与安全预览；版本提交同时写入最小化预览 Outbox 事件。未启用受审查 Converter Runner 时，内置安全降级消费者只校验允许字段并写 receipt，不读取内容或调用外部转换器。 |
| `internal/modules/features/webhook`、`internal/modules/features/mcp`、`internal/modules/features/message` | 低风险外部集成。 |

## 权限模型

敏感操作按以下顺序判断：

```text
authentication
  -> stable permission code and route operation
  -> ownership or server-derived scope
  -> operation constraints
  -> audit
```

`member` 是有效登录用户的隐式基础角色；管理员使用全局授权；版主必须绑定一个或多个版块。版主操作帖子或回复时，后端从目标资源读取真实 `category_id`，不信任客户端提交的版块值。

## 插件边界

CampusOS 区分 Core Module、Built-in Feature、External Plugin 和无 Runtime 的 Resource Package。External Plugin 当前有以下运行方式：

| Runtime | 运行方式 | 适用场景 |
| --- | --- | --- |
| `wasm` | 由 wazero 在受控 Runtime 中加载模块。 | 小型事件逻辑和隔离执行。 |
| `grpc`（兼容名称） | CampusOS 启停外部进程；Extension/Event 仅访问显式声明并校验的 loopback HTTP 端点。 | 当前受管进程扩展；不是标准 protobuf gRPC。 |

历史 `runtime: builtin` 只作为旧 Manifest 到 Built-in Feature/Core 的兼容映射，不是第三方动态安装进程内 Go 代码的入口。

插件不能直接获得主系统全部能力。Host API 根据插件 manifest 中声明的权限进行默认拒绝检查。

## 数据持久化

| 数据 | 保存位置 |
| --- | --- |
| 用户、帖子、权限、插件元数据和审计 | PostgreSQL。 |
| 列表缓存和临时状态 | Redis。 |
| 领域事件 | NATS 与进程内事件总线。 |
| 插件代码 | `data/plugins/<plugin>/`。 |
| 插件运行数据 | `data/plugin_data/<plugin>/`。 |
| Built-in Feature 本地数据 | `data/module_data/<feature>/`。 |
| 主题、首页和个人主页风格资源 | `data/resources/<type>/<package>/`。 |
| 用户文件 | `data/personal-space/<user_id>/`；新文档/受管课表的元数据与 Object ID 位于 PostgreSQL，文件仍是私有 Local Provider payload。 |
| 开发日志 | `.campusos/logs/`。 |

数据库既包含已验证的核心 PostgreSQL 外键，也保留部分由逻辑归属、索引和服务层约束表达的当前插件关系。
当前 clean baseline 为 `000001-000003`：76 张现行业务表、8 张 v1 插件身份/版本/三层授权基础表，以及不含用户凭据的稳定参考数据；
执行器另管理 checksum 和互斥锁。修改 Schema 前必须运行 `make v1-database-baseline-check`、`make database-check`
和架构同步检查。详见 [数据库迁移与 Schema 冗余治理](/operations/database-migration-hygiene)。

## 页面扩展安全

个人主页、首页和完整用户前台风格包必须通过后端筛查。HTML 事件处理器、不安全 URL、路径逃逸和危险 CSS 会被拒绝；可选 JavaScript 仅在无同源、无 DOM、无存储和无任意网络权限的 `sandbox-worker.v1` 中执行。CampusStyleSDK 只代理 manifest 声明的只读能力，并继续执行主页所有者绑定和用户私有能力授权。

下一步可阅读 [数据目录](/reference/data-layout) 或 [插件体系](/plugins/overview)。
