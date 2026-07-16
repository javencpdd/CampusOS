# 生命周期与数据

CampusOS 不再用 `scope` 直接决定启停方式。`scope: system | user` 只描述管理级别、安装权限和系统重要性；后端和前端生效方式由 `lifecycle` 声明。

```yaml
lifecycle:
  backend:
    activation_mode: plugin-restart
  frontend:
    activation_mode: hot
```

## 默认值

| Runtime | 后端 | 前端 |
| --- | --- | --- |
| builtin + system | `restart` | `hot` |
| 受管进程（`grpc` 兼容名） | `plugin-restart` | `hot` |
| Wasm | `hot` | `hot` |

旧 manifest 会自动得到这些默认值。受管进程可以只重启插件；Wasm 使用候选实例验证成功后原子替换；Core 始终启用，Built-in Feature 则按自身 `restart` 或 `hot-gated` 策略生效。

## 三轴状态

- BackendState：`installed / starting / running / restarting / stopping / stopped / pending_restart / error`。
- FrontendState：`unloaded / loading / loaded / incompatible / error`。
- Health：`healthy / degraded / unavailable / unknown`。

后端短暂 restarting 或 degraded 时，前端页面不会消失。管理员明确停用或卸载插件后，Web 才清理其动态路由、导航、插槽、Surface 和 Action。旧 `status` 与 `pending_restart` 字段继续兼容。

## 数据保留

停止、禁用或热卸载前端不会自动删除 `data/plugin_data/<plugin>/`、PostgreSQL 配置和日志、用户业务数据或审计。系统级插件不能从 Admin 卸载；用户级插件卸载前应备份数据并阅读插件 README。

下一步阅读 [前端运行时与 Gateway](./frontend-runtime.md)。
