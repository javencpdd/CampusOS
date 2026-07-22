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
| `internal/modules/core/userstorage` | 用户目录、安全路径、配额和资产存储 Port。 |
| `internal/modules/core/community` | 版块、帖子、回复、标签、内容治理状态机和统一 `ContentQuery`。 |
| `internal/modules/core/moderation` | 版块版主作用域、治理动作和审计。 |
| `internal/platform/observability` | 有界指标、Admin 摘要、运行时与数据库池快照，以及默认关闭的 loopback Prometheus 导出。 |
| `internal/plugin` | External Plugin Catalog、Runtime、Lifecycle、配置、UI/Event 与审计 Facade。 |
| `internal/plugin/hostapi` | 插件访问主系统能力的受控边界。 |
| `internal/modules/features/personalspace` | 个人主页、头像、主页所有者风格和 Community 内容查询适配。 |
| `internal/modules/features/appearance/stylepack`、`internal/modules/features/appearance/webtheme` | 目标作用域筛查、沙箱特效声明和管理员系统主题目录。 |
| `internal/modules/features/richtext` | 富文本草稿、清洗、发布和图片资产。 |
| `internal/modules/features/schedule` | 学期课表、日历计算和表格导入。 |
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
| 用户文件 | `data/personal-space/<user_id>/`。 |
| 开发日志 | `.campusos/logs/`。 |

数据库既包含已验证的核心 PostgreSQL 外键，也保留部分由逻辑归属、索引和服务层约束表达的插件关系。修改 schema 前必须运行 `make database-check` 和 migration 验证；当前顺序 migration 为 `000001` 至 `000040`。`000039` 记录管理员准入状态变更，`000040` 增加服务端 MFA Session 强度、TOTP 加密信封、Ticket/恢复码摘要和管理员策略。

## 页面扩展安全

个人主页、首页和完整用户前台风格包必须通过后端筛查。HTML 事件处理器、不安全 URL、路径逃逸和危险 CSS 会被拒绝；可选 JavaScript 仅在无同源、无 DOM、无存储和无任意网络权限的 `sandbox-worker.v1` 中执行。CampusStyleSDK 只代理 manifest 声明的只读能力，并继续执行主页所有者绑定和用户私有能力授权。

下一步可阅读 [数据目录](/reference/data-layout) 或 [插件体系](/plugins/overview)。
