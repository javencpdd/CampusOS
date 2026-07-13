# Host API 与权限

Host API 只监听配置的内部地址，默认 `127.0.0.1:18080`。每个调用同时检查运行中插件身份、启动时签发的短期随机令牌和 Manifest 权限。令牌会在重载时轮换、停用时撤销，并通过进程环境变量交给受管插件；插件不应记录它。

## 请求身份

SDK 自动读取：

```text
CAMPUSOS_PLUGIN_NAME
CAMPUSOS_PLUGIN_TOKEN
CAMPUSOS_HOST_API_URL
```

直接调用需要 `X-CampusOS-Plugin` 和 `X-CampusOS-Plugin-Token`。只有插件名称没有令牌不能通过生产 Host API。

## 权限模型

Manifest 默认没有权限。方法与权限对应关系由 `docs/api/plugin-permissions-v1.json` 生成，例如：

| 方法 | 权限 |
| --- | --- |
| `GetUser` | `user/read` |
| `QueryThreads` | `thread/read` |
| `PublishEvent` | `event/publish` |
| `StorageSet` | `storage/write` |
| `SendNotification` | `notification/send` |

## Host API v2 受管数据

`RecordCreate`、`RecordGet`、`RecordList`、`RecordUpdate` 和 `RecordDelete` 只提供给 `api_version: campusos.plugin/v2`、`host_api_version: v2` 的 External Plugin，并且只能操作其 Manifest 中 `owner: system` 的集合。

用户归属记录和文件必须经已登录的 `/api/v1/plugin-market/...` 接口处理。扩展进程不能提交用户 ID，也不能直接访问数据库、JWT 或个人空间绝对路径。完整合同见仓库的 `docs/api/Host-API-v2受管数据合同.md`。

插件权限和系统用户 RBAC 是两个不同层次。插件声明只表示它可请求某类 Host 能力；涉及具体用户、个人空间、课表或版主管理时，Host 仍需检查用户归属、scope 和可见性。

系统级与用户级插件使用同一最小权限原则。`restart`、`plugin-restart` 或 `hot` 都不会自动扩大权限，也不会绕过权限复核。
