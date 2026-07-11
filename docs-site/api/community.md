# 认证与社区 API

## 注册

```http
POST /api/v1/auth/register
Content-Type: application/json
```

```json
{
  "username": "alice",
  "nickname": "Alice",
  "email": "alice@example.edu",
  "password": "a-strong-password"
}
```

用户名、昵称、邮箱和密码都有长度或格式校验。邮箱和用户名冲突通常返回 `409`。

## 登录

```http
POST /api/v1/auth/login
Content-Type: application/json
```

```json
{
  "email": "alice@example.edu",
  "password": "a-strong-password"
}
```

成功响应包含用户、有效角色、`access_token` 和 `refresh_token`。后续请求使用：

```http
Authorization: Bearer <access_token>
```

## 当前用户

```http
GET /api/v1/auth/me
Authorization: Bearer <access_token>
```

## 版块

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/categories` | 列出版块。 |
| `GET` | `/categories/:id` | 获取一个版块。 |
| `POST` | `/categories` | 管理员创建版块，需要 `category:write`。 |
| `PUT` | `/categories/:id` | 管理员更新版块，需要 `category:write`。 |
| `DELETE` | `/categories/:id` | 管理员删除版块，需要 `category:delete`。 |

版块可以配置默认标签。用户发帖时，服务会将默认标签和用户标签合并并去重。

## 创建普通帖子

```http
POST /api/v1/threads
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "title": "社团招新安排",
  "content": "本周五在活动中心进行招新。",
  "category_id": "1000000000000000004",
  "tags": ["社团", "招新"],
  "is_private": false
}
```

`category_id` 使用字符串承载 BIGINT ID，避免 JavaScript 数值精度损失。

## 帖子读取与作者操作

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/threads?page=1&page_size=20` | 公开帖子列表。 |
| `GET` | `/threads/:id` | 公开帖子详情。 |
| `GET` | `/threads/:id/me` | 按当前用户可见性读取详情。 |
| `PUT` | `/threads/:id` | 作者更新普通帖子。 |
| `DELETE` | `/threads/:id` | 作者删除普通帖子。 |

私密帖子只能由作者和明确允许的治理主体访问。客户端不要仅根据列表中是否出现来判断权限。

## 回复

创建回复：

```http
POST /api/v1/threads/:thread_id/posts
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "content": "收到，我会参加。",
  "parent_id": null
}
```

回复另一层时把 `parent_id` 设置为目标回复 ID。服务会为回复分配楼层号。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/threads/:id/posts` | 公开回复列表。 |
| `GET` | `/threads/:id/posts/me` | 按当前用户可见性读取。 |
| `PUT` | `/threads/:id/posts/:post_id` | 回复作者更新。 |
| `DELETE` | `/threads/:id/posts/:post_id` | 回复作者删除。 |

## 富文本文章

富文本文章与普通帖子共用 `threads` 作为列表入口，正文和图片元数据使用独立表。主要流程为：

1. `POST /richtext/articles` 创建草稿。
2. `POST /richtext/assets` 上传图片。
3. `PUT /richtext/articles/:id` 保存正文。
4. `POST /richtext/preview` 预览清洗后的 HTML。
5. `POST /richtext/articles/:id/publish` 发布。

富文本 HTML 会在后端清洗。客户端不能通过自定义标签、脚本或事件处理器绕过清洗规则。
