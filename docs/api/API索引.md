# CampusOS API 索引

> 基础地址：`http://localhost:8080/api/v1`
> 当前实现基线：`v0.5.33-dev`

## 1. 契约状态

CampusOS 目前通过 Gin 暴露带版本前缀的 HTTP 路由。历史文件 [openapi-v0.3-pre.yaml](openapi-v0.3-pre.yaml) 只是 v0.3 前期基线，不是当前系统的完整规范。

在 v6 API 契约任务完成前，当前权威实现来源是 `internal/server/server.go` 的路由注册、各 `internal/<module>/` 包中的请求/响应模型，以及 `web/src/api/index.ts` 和 `admin/src/api/index.ts` 中的类型化前端调用。

## 2. 路由分组

| 分组 | 常见路径 | 说明 |
| --- | --- | --- |
| 认证与身份 | `/auth/*`、`/users/*` | 注册、登录、当前用户、资料和 RBAC 保护的管理操作。 |
| 社区 | `/categories`、`/threads`、`/posts` | 版块、默认标签、帖子/回复流程、可见性和内容治理。 |
| 个人空间 | `/spaces/me/*`、`/u/:username` | 主页设置、内容同步、头像、风格包和公开主页。 |
| 个人课表 | `/schedule/me/*` | 当前或指定学期课表、已保存课表列表、选择/新建、保存和导入。 |
| 富文本文章 | `/richtext/articles/*` | 草稿、上传、预览、发布、详情、作者操作和管理端治理。 |
| 插件 | `/plugins/*`、`/plugin-packages/*` | 列表、配置、生命周期、插件包预检/导入/导出和审计。 |
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

## 4. 兼容性规则

1. 新公开 API 应使用 `/api/v1` 前缀，并在所属模块中记录请求/响应变化。
2. 不应静默改变已有字段或路由语义；行为变化时新增明确字段或版本化路由。
3. 临时实验接口必须在对应帮助文档或进度记录中标记。
4. 内部 `/mcp/*` 路由不等同于标准 MCP；标准服务需要独立传输、认证、能力协商和契约测试。

## 5. 关联文档

- [接口协议适配器标准说明](../help/系统设计相关/接口协议适配器标准说明.md)
- [v6 API 契约计划](../项目计划v6/01-v6版本计划书.md)
- [v0.5 第二版计划书](../项目计划v5/01-v5版本计划书第二版.md)
