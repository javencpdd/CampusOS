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
| `internal/core/identity` | 用户、账号、JWT、角色和权限。 |
| `internal/community` | 版块、帖子、回复、标签和社区事件。 |
| `internal/moderation` | 版块版主作用域、治理动作和审计。 |
| `internal/plugin` | Manager、manifest、Runtime、插件包、配置和日志。 |
| `internal/plugin/hostapi` | 插件访问主系统能力的受控边界。 |
| `internal/space` | 个人主页、头像、用户文件和主页所有者风格包。 |
| `internal/stylepack`、`internal/webtheme` | 目标作用域筛查、沙箱特效声明和管理员系统主题目录。 |
| `internal/richtext` | 富文本草稿、清洗、发布和图片资产。 |
| `internal/schedule` | 学期课表、日历计算和表格导入。 |
| `internal/webhook`、`internal/mcp`、`internal/message` | 低风险外部集成。 |

## 权限模型

敏感操作按以下顺序判断：

```text
authentication
  -> resource:action
  -> ownership or scope
  -> operation constraints
  -> audit
```

`member` 是有效登录用户的隐式基础角色；管理员使用全局授权；版主必须绑定一个或多个版块。版主操作帖子或回复时，后端从目标资源读取真实 `category_id`，不信任客户端提交的版块值。

## 插件边界

CampusOS 当前支持三种 Runtime：

| Runtime | 运行方式 | 适用场景 |
| --- | --- | --- |
| `builtin` | 业务实现编译在 CampusOS 后端中，插件目录保存 manifest 和配置。 | 随系统部署的默认能力。 |
| `wasm` | 由 wazero 在受控 Runtime 中加载模块。 | 小型事件逻辑和隔离执行。 |
| `grpc` | 外部进程通过 gRPC 与主系统通信。 | 复杂依赖和独立服务。 |

插件不能直接获得主系统全部能力。Host API 根据插件 manifest 中声明的权限进行默认拒绝检查。

## 数据持久化

| 数据 | 保存位置 |
| --- | --- |
| 用户、帖子、权限、插件元数据和审计 | PostgreSQL。 |
| 列表缓存和临时状态 | Redis。 |
| 领域事件 | NATS 与进程内事件总线。 |
| 插件代码 | `data/plugins/<plugin>/`。 |
| 插件运行数据 | `data/plugin_data/<plugin>/`。 |
| 用户文件 | `data/personal-space/<user_id>/`。 |
| 开发日志 | `.campusos/logs/`。 |

数据库 migration 当前没有统一声明 PostgreSQL 外键，部分关系仍由索引和服务层约束表达。修改 schema 前应先运行数据质量检查和 migration 验证。

## 页面扩展安全

个人主页、首页和完整用户前台风格包必须通过后端筛查。HTML 事件处理器、不安全 URL、路径逃逸和危险 CSS 会被拒绝；可选 JavaScript 仅在无同源、无 DOM、无存储和无任意网络权限的 `sandbox-worker.v1` 中执行。CampusStyleSDK 只代理 manifest 声明的只读能力，并继续执行主页所有者绑定和用户私有能力授权。

下一步可阅读 [数据目录](/reference/data-layout) 或 [插件体系](/plugins/overview)。
