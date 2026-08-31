# API 端点清单

> **版本**：v0.2.2-dev
> **基础路径**：`/api/v1`
> **总端点数**：33

---

## 一、公开接口（无需认证）

| # | 方法 | 端点 | 说明 | Handler |
|:---|:---|:---|:---|:---|
| 1 | `GET` | `/health` | 健康检查 | UserHandler.HealthCheck |
| 2 | `POST` | `/auth/register` | 用户注册（bcrypt 密码哈希） | UserHandler.Register |
| 3 | `POST` | `/auth/login` | 用户登录（返回 JWT Token） | UserHandler.Login |
| 4 | `GET` | `/threads` | 帖子列表（支持分页/搜索/过滤） | ThreadHandler.ListThreads |
| 5 | `GET` | `/threads/:id` | 帖子详情（浏览计数+1） | ThreadHandler.GetThread |
| 6 | `GET` | `/users` | 用户列表（分页） | UserHandler.ListUsers |
| 7 | `GET` | `/users/:id` | 用户详情 | UserHandler.GetUser |
| 8 | `GET` | `/categories` | 版块列表 | CategoryHandler.List |
| 9 | `GET` | `/categories/:id` | 版块详情 | CategoryHandler.Get |
| 10 | `GET` | `/threads/:id/posts` | 帖子回复列表（分页） | PostHandler.ListPosts |
| 11 | `GET` | `/events` | 事件历史（最近 N 条） | EventHandler.ListEvents |

---

## 二、认证接口（需要 JWT Token）

请求头：`Authorization: Bearer <access_token>`

| # | 方法 | 端点 | 说明 | Handler |
|:---|:---|:---|:---|:---|
| 12 | `GET` | `/auth/me` | 获取当前登录用户信息 | UserHandler.GetMe |
| 13 | `PUT` | `/users/:id` | 更新用户信息（昵称/头像/简介） | UserHandler.UpdateUser |
| 14 | `POST` | `/threads` | 创建帖子 | ThreadHandler.CreateThread |
| 15 | `PUT` | `/threads/:id` | 更新帖子（仅作者） | ThreadHandler.UpdateThread |
| 16 | `DELETE` | `/threads/:id` | 删除帖子（仅作者，软删除） | ThreadHandler.DeleteThread |
| 17 | `POST` | `/threads/:id/posts` | 创建回复 | PostHandler.CreatePost |
| 18 | `PUT` | `/threads/:thread_id/posts/:post_id` | 更新回复（仅作者） | PostHandler.UpdatePost |
| 19 | `DELETE` | `/threads/:thread_id/posts/:post_id` | 删除回复（仅作者） | PostHandler.DeletePost |
| 20 | `POST` | `/categories` | 创建版块 | CategoryHandler.Create |
| 21 | `PUT` | `/categories/:id` | 更新版块 | CategoryHandler.Update |

---

## 三、管理员接口（需要 JWT + RBAC 权限）

| # | 方法 | 端点 | 说明 | 权限要求 | Handler |
|:---|:---|:---|:---|:---|:---|
| 22 | `POST` | `/users/:id/suspend` | 封禁用户 | `user:suspend` | UserHandler.SuspendUser |
| 23 | `POST` | `/users/:id/activate` | 解封用户 | `user:suspend` | UserHandler.ActivateUser |
| 24 | `POST` | `/threads/:id/pin` | 置顶帖子 | `thread:pin` | ThreadHandler.PinThread |
| 25 | `POST` | `/threads/:id/unpin` | 取消置顶 | `thread:pin` | ThreadHandler.UnpinThread |
| 26 | `POST` | `/threads/:id/lock` | 锁定帖子 | `thread:pin` | ThreadHandler.LockThread |
| 27 | `POST` | `/threads/:id/unlock` | 解锁帖子 | `thread:pin` | ThreadHandler.UnlockThread |
| 28 | `DELETE` | `/categories/:id` | 删除版块 | `category:delete` | CategoryHandler.Delete |
| 29 | `GET` | `/plugins` | 插件列表 | `role:manage` | PluginHandler.ListPlugins |
| 30 | `GET` | `/plugins/:name` | 插件详情 | `role:manage` | PluginHandler.GetPlugin |
| 31 | `POST` | `/plugins/:name/enable` | 启用插件 | `role:manage` | PluginHandler.EnablePlugin |
| 32 | `POST` | `/plugins/:name/disable` | 禁用插件 | `role:manage` | PluginHandler.DisablePlugin |
| 33 | `DELETE` | `/plugins/:name` | 卸载插件 | `role:manage` | PluginHandler.UninstallPlugin |

---

## 四、统一响应格式

### 成功响应
```json
{
  "code": 0,
  "msg": "success",
  "data": { ... }
}
```

### 列表响应
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [ ... ],
    "pagination": {
      "page": 1,
      "page_size": 20,
      "total": 100,
      "total_pages": 5
    }
  }
}
```

### 错误响应
```json
{
  "code": 40001,
  "msg": "invalid request parameters"
}
```

---

## 五、错误码体系

| 范围 | 模块 | 示例 |
|:---|:---|:---|
| 10xxx | 通用错误 | 10001 参数错误，10006 服务器内部错误 |
| 20xxx | 认证/授权 | 20001 未认证，20004 权限不足，20005 API Key 无效 |
| 30xxx | 用户模块 | 30004 用户不存在 |
| 40xxx | 帖子模块 | 40003 帖子不存在 |
| 50xxx | 版块模块 | 50002 版块不存在 |
| 60xxx | 插件模块 | 60001 事件不可用，60003 插件不存在 |

---

## 六、RBAC 角色权限矩阵

| 操作 | guest | member | moderator | admin |
|:---|:---|:---|:---|:---|
| 浏览帖子/版块 | ✅ | ✅ | ✅ | ✅ |
| 注册/登录 | ✅ | ✅ | ✅ | ✅ |
| 发帖/回帖 | ❌ | ✅ | ✅ | ✅ |
| 编辑/删除自己的帖子 | ❌ | ✅ | ✅ | ✅ |
| 编辑/删除任意帖子 | ❌ | ❌ | ✅ | ✅ |
| 置顶/锁定帖子 | ❌ | ❌ | ✅ | ✅ |
| 封禁/解封用户 | ❌ | ❌ | ✅ | ✅ |
| 管理版块 | ❌ | ❌ | ❌ | ✅ |
| 管理插件 | ❌ | ❌ | ❌ | ✅ |
| 角色分配 | ❌ | ❌ | ❌ | ✅ |