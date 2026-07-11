# 版主管理 API

## 授权模型

版主不是全站角色。每个版主必须绑定一个或多个 `category` scope，只能治理被分配版块中的内容。

当前治理动作：

| 权限 | 动作 |
| --- | --- |
| `thread:pin` | 置顶和取消置顶。 |
| `thread:lock` | 锁定和解锁。 |
| `post:delete` | 删除回复。 |

主题操作从 `threads.category_id` 读取版块；回复操作先读取所属主题。请求体中的版块值不会作为授权依据。

## 管理员配置版主

列出所有版主：

```http
GET /api/v1/moderation/admin/moderators
Authorization: Bearer <admin_token>
```

读取一个用户：

```http
GET /api/v1/moderation/admin/moderators/:user_id
```

完整替换其版块范围：

```http
PUT /api/v1/moderation/admin/moderators/:user_id
Content-Type: application/json
```

```json
{
  "category_ids": [
    "1000000000000000004",
    "1783048438434995264"
  ]
}
```

空数组会撤销该用户全部版主 scope。通用 `/users/:id/roles` 接口拒绝创建没有版块范围的全局版主。

## 当前用户可执行动作

```http
GET /api/v1/moderation/me?thread_id=<thread_id>
Authorization: Bearer <access_token>
```

响应中的 `actions` 由后端根据插件状态、动作开关、角色权限和版块 scope 计算。前端只应显示返回为 `true` 的按钮。

## 治理接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/moderation/threads/:id/pin` | 置顶主题。 |
| `POST` | `/moderation/threads/:id/unpin` | 取消置顶。 |
| `POST` | `/moderation/threads/:id/lock` | 锁定主题。 |
| `POST` | `/moderation/threads/:id/unlock` | 解锁主题。 |
| `DELETE` | `/moderation/threads/:thread_id/posts/:post_id` | 删除回复。 |

跨版块请求返回 `403`。插件整体停用并重启后返回 `503/71001`。

## 插件配置热更新

`category-moderation` 是系统级插件，整体启用或停用需要重启 API。但以下动作开关保存后立即作用于下一次授权检查：

```json
{
  "allow_pin": true,
  "allow_lock": true,
  "allow_delete_post": false
}
```

配置通过：

```http
PUT /api/v1/plugins/category-moderation/config
```

旧页面尚未刷新时可能仍显示按钮，但后端会立即拒绝已经关闭的动作。刷新帖子详情后按钮会同步。

## 审计

版主范围变更、置顶、锁定和删除回复写入 `audit_logs`。插件停用不会删除 scope 或历史审计。
