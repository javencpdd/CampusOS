# 生命周期与数据

## 用户级插件

`scope: user` 支持管理员执行：

- 加载。
- 停止。
- 重载。
- 覆盖更新。
- 卸载。

这些操作不要求重启 CampusOS API。覆盖更新时应先停止旧 Runtime，再加载新包。

## 系统级插件

`scope: system` 随 CampusOS 后端部署。管理端启用或停用只保存目标状态：

```text
current status != desired status
  -> pending restart
  -> restart API
  -> apply desired status
```

系统级插件不能通过 Admin 卸载。更新实现通常需要部署新的 CampusOS 后端版本。

特定 Built-in 插件可以让业务配置热更新，例如 `category-moderation` 的动作开关；这不改变插件整体启停需要重启的规则。

## 状态判断

常见状态：

| 状态 | 含义 |
| --- | --- |
| `installed` | 已识别但未运行。 |
| `running` | Runtime 当前运行。 |
| `stopped` | 当前已停止。 |
| `error` | 启动或运行发生错误。 |

客户端应同时展示当前状态、目标状态和 `pending_restart`，避免把“已请求停用”误显示成“当前已经停用”。

## 数据保留

插件停止或禁用不应自动删除：

- `data/plugin_data/<plugin>/`。
- PostgreSQL 插件配置和日志。
- 用户业务数据。
- 审计记录。

卸载前必须由插件 README 明确说明数据是否保留、如何清理以及能否重新安装恢复。

## 日志

插件日志记录启动、停止、事件处理、Host API 调用和错误。Admin 可通过：

```http
GET /api/v1/plugins/:name/logs
```

排查顺序：

1. 检查当前状态和目标状态。
2. 查看插件日志。
3. 查看 `.campusos/logs/api.log`。
4. 检查 manifest、模块文件和权限。
5. 检查插件运行数据和外部依赖。

## 卸载检查

1. 确认没有用户功能依赖插件。
2. 备份插件包、配置和运行数据。
3. 停止插件。
4. 执行卸载。
5. 检查残留路由、事件订阅和数据。

系统级插件不走此流程，应通过 CampusOS 版本升级或回退管理。
