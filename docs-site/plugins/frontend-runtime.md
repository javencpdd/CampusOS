# 前端运行时与 Extension Gateway

插件在 `plugin.yaml` 的 `ui` 中声明 Route、Navigation、Slot、Surface 和 Action。Core 返回当前用户可见的 Runtime Manifest，Web 收到 SSE revision 后原子重建注册项。

```http
GET /api/v1/ui/runtime-manifest
GET /api/v1/ui/events
ANY /api/v1/extensions/:plugin/*path
```

## 默认 UI

第三方插件应使用声明式 schema，只能组合 Campus UI 白名单组件。复杂编辑器或图表可以使用 `trusted-module`，但 module ID 必须由 Core 编译进白名单；不支持远程 Vue 模块或任意同源脚本。

每个 Surface 必须提供 ID、版本、类型、layout role、默认 renderer/schema、data contract、Action IDs、公开 token 和可调整 region。Action 的 method、path、权限、确认、审计和固定 body 属于插件合同，风格包不能修改。

## Gateway 安全

Gateway 要求 JWT，并执行插件状态与健康检查、权限、1 MiB 请求上限、5 秒超时、Trace ID、审计和标准错误。插件只信任 Core 注入的 caller，不能信任请求体中的 user ID、角色或管理员状态。

系统级插件和用户级插件都不会自动取得当前用户没有的权限。风格包使用只读 CampusStyleSDK，与可执行业务 Action 的 Extension Gateway 是两条不同边界。

可运行示例位于 `data/plugins/campus-welcome/`。
