# Host API v1 与权限

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

插件权限和系统用户 RBAC 是两个不同层次。插件声明只表示它可请求某类 Host 能力；涉及具体用户、个人空间、课表或版主管理时，Host 仍需检查用户归属、scope 和可见性。

系统级与用户级插件使用同一最小权限原则。系统级插件不会因为需要重启而自动获得更多权限，用户级热加载也不会绕过权限复核。
