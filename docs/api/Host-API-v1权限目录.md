# CampusOS Host API v1 权限目录

> 由 `go run ./cmd/campusos-contracts --write` 从代码生成。Manifest 默认无权限；调用 Host API 时同时校验插件身份和声明权限。

## Host API 方法

| 方法 | Manifest 权限 |
| --- | --- |
| `CheckPermission` | `permission/check` |
| `GetConfig` | `config/read` |
| `GetReply` | `reply/read` |
| `GetThread` | `thread/read` |
| `GetUser` | `user/read` |
| `Log` | `log/write` |
| `PublishEvent` | `event/publish` |
| `QueryThreads` | `thread/read` |
| `SendNotification` | `notification/send` |
| `SetConfig` | `config/write` |
| `StorageDelete` | `storage/delete` |
| `StorageGet` | `storage/read` |
| `StorageSet` | `storage/write` |

## 权限说明

| Resource | Action | Risk | Description |
| --- | --- | --- | --- |
| `audit` | `write` | `high` | Write security or governance audit records. |
| `config` | `read` | `low` | Read the calling plugin's configuration. |
| `config` | `write` | `medium` | Update the calling plugin's configuration. |
| `event` | `publish` | `medium` | Publish an event through the CampusOS event bus. |
| `homepage` | `read` | `low` | Read homepage presentation configuration. |
| `homepage` | `write` | `high` | Change the system homepage presentation. |
| `log` | `write` | `low` | Write namespaced plugin logs. |
| `notification` | `send` | `high` | Send a user-facing notification. |
| `permission` | `check` | `medium` | Evaluate a user's CampusOS permission. |
| `post` | `delete` | `high` | Delete a reply within an authorized governance scope. |
| `reply` | `read` | `low` | Read reply data exposed by Host API. |
| `richtext_article` | `read` | `low` | Read rich-text article data. |
| `richtext_article` | `write` | `high` | Create or change rich-text article data. |
| `richtext_asset` | `read` | `low` | Read rich-text asset metadata. |
| `richtext_asset` | `write` | `high` | Upload or change rich-text assets. |
| `schedule` | `read` | `medium` | Read the authorized user's schedule. |
| `schedule` | `write` | `high` | Change the authorized user's schedule. |
| `space` | `read` | `medium` | Read a personal space allowed by its visibility policy. |
| `space` | `write` | `high` | Change the authorized user's personal space. |
| `space_file` | `read` | `medium` | Read files in the authorized personal-space namespace. |
| `space_file` | `write` | `high` | Create or change files in the authorized personal-space namespace. |
| `storage` | `delete` | `medium` | Delete a key in the calling plugin's isolated storage. |
| `storage` | `read` | `low` | Read a key in the calling plugin's isolated storage. |
| `storage` | `write` | `medium` | Write a key in the calling plugin's isolated storage. |
| `style` | `read` | `low` | Read style-pack metadata allowed for the current target. |
| `thread` | `lock` | `high` | Lock or unlock a thread within an authorized category. |
| `thread` | `pin` | `high` | Pin or unpin a thread within an authorized category. |
| `thread` | `read` | `low` | Read thread data exposed by Host API. |
| `thread` | `write` | `high` | Create or change thread data for an authorized subject. |
| `user` | `read` | `medium` | Read the public Host API user projection. |
| `web_theme` | `configure` | `high` | Configure system-provided Web style packs. |
| `web_theme` | `read` | `low` | Read available Web style packs and current selection. |

系统级插件仍需在重启后应用生命周期变更；用户级插件可以受控热加载。权限不会因为 Runtime 或热加载而自动扩大。
