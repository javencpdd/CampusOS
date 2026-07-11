# 插件前端运行时与 Extension Gateway

> 适用基线：v0.6 插件 UI Runtime（`campusos.ui/v1`）

## 1. 一句话理解

插件提供业务、默认界面声明和 Action；CampusOS Core 负责登录、权限、路由、导航、渲染、API 转发、审计和卸载清理。插件不直接修改主 Router，也不拿 JWT。

```text
plugin.yaml ui
  -> GET /api/v1/ui/runtime-manifest
  -> Web Runtime Registry
  -> Route / Navigation / Slot / Surface / Action
  -> POST|GET /api/v1/extensions/<plugin>/<path>
  -> Core trusted context
  -> builtin | gRPC | Wasm Runtime
```

## 2. 生命周期不等于 scope

`scope` 只表示谁能安装、配置、卸载以及系统重要性。实际生效方式写在 `lifecycle`：

```yaml
lifecycle:
  backend:
    activation_mode: plugin-restart
  frontend:
    activation_mode: hot
```

旧 manifest 不必修改。默认值为：builtin+system 使用 `restart`，gRPC 使用 `plugin-restart`，Wasm 使用 `hot`，所有前端使用 `hot`。系统级 gRPC 因此可以只重启插件；系统级 Wasm 也可以热替换。只有 Core 进程内 builtin 默认需要整站重启。

Admin 会分别显示 BackendState、FrontendState 和 Health。后端短暂 restarting/degraded 时，前端贡献不会自动消失；用户会看到统一健康提示。管理员明确停用或卸载插件时，Runtime Registry 才清理其路由、菜单、插槽、Surface、Action 和缓存。

## 3. 最小默认 UI

第三方插件默认使用声明式 schema：

```yaml
ui:
  contract_version: campusos.ui/v1
  actions:
    - id: plugin.demo.action.refresh
      label: 刷新
      method: POST
      path: /refresh
      permission: demo:read
      audit: true
  surfaces:
    - id: plugin.demo.page.main
      version: v1
      type: page
      layout_role: main
      renderer: schema
      action_ids: [plugin.demo.action.refresh]
      schema:
        component: stack
        children:
          - component: heading
            text: Demo
          - component: button
            text: 刷新
            action_id: plugin.demo.action.refresh
  routes:
    - id: plugin.demo.route.main
      path: /plugins/demo
      surface_id: plugin.demo.page.main
      requires_auth: true
```

允许的 schema 组件只有 `stack`、`grid`、`card`、`heading`、`text`、`badge`、`alert`、`button` 和 `list`。未知组件、未知 Action、重复 ID、危险路径和可变 HTTP Method 会在安装前失败。

复杂编辑器、地图和图表可以声明 `renderer: trusted-module`，但 `module_id` 必须存在于 Core 编译期白名单。当前没有远程 Vue 模块加载，也不允许插件读取全局 Router、Store、JWT 或其他插件 DOM。

## 4. Gateway 与可信上下文

稳定入口是：

```http
ANY /api/v1/extensions/:plugin/*path
Authorization: Bearer <user token>
```

Gateway 只接受 manifest 中声明的 Action，限制请求体为 1 MiB、处理时间为 5 秒，并检查插件后端状态、健康、用户权限、Trace ID 和审计。客户端请求体里的 `user_id`、角色或管理员字段没有可信意义。

Runtime 收到的 `caller.user_id`、`caller.username` 和 `caller.trace_id` 由 Core 从已验证 JWT 和请求上下文生成。builtin 使用 Core 注册的 handler；gRPC 外部进程必须配置 loopback `extension_url`；Wasm 通过受限 `extension.request` ABI 接收 JSON。

## 5. 权限差异

- 系统级插件并不会因为 `scope: system` 自动获得管理员权限。Gateway 仍按当前用户和 Action 的 `permission` 检查。
- 用户级插件也不是“用户自行安装”。安装、配置和卸载仍受 Admin 的插件权限保护。
- 风格包使用的是只读 CampusStyleSDK capability，不是 Extension Gateway，不能执行插件 Action。
- 插件前端只能调用自己 manifest 声明的 Action；不能替其他插件拼接 Gateway URL。

## 6. 可运行示例

`data/plugins/campus-welcome/` 展示完整链路。启动后访问 `/extensions/campus-welcome`，按钮会经 Gateway 调用 builtin Runtime，并在插件日志中留下带 Trace ID 的审计记录。

## 7. 更新与清理检查

1. 先让新 manifest 通过校验。
2. 启用后检查 Runtime Manifest revision 是否增加。
3. 直接访问动态路由并执行 Action。
4. 让后端进入 degraded，确认页面仍存在并显示健康提示。
5. 停用插件，确认路由、导航、Surface 和 Action 都被清理。
6. 检查业务数据和 `data/plugin_data/<plugin>/` 未被误删。
