# CampusOS 个人主页 Space 说明

> 适用阶段：v0.4-dev
> 更新时间：2026-07-01

## 1. 功能定位

个人主页 Space 是 v0.4 “个人主页/博客园插件”的后端基础能力。本阶段先提供公开主页入口、用户主页配置模型、持久化仓储和登录用户自助保存接口。

当前实现重点是稳定数据边界：

| 能力 | 状态 |
| --- | --- |
| 公开主页入口 | 已支持 |
| 用户主页配置持久化 | 已支持 |
| 登录用户编辑自己的主页配置 | 已支持 |
| 按用户名访问主页 | 已支持 |
| 内容事件同步 | 已支持 |
| 公开主页内容列表 | 已支持 |
| 前端主页渲染页 | 后续任务 |
| 风格包校验、预览和导出 | 已支持 |
| 风格包应用和回滚 | 后续任务 |

## 2. API 接口

### 2.1 按用户 ID 访问公开主页

```bash
curl http://localhost:8080/api/v1/space/1001
```

接口：

```text
GET /api/v1/space/:user_id
```

说明：

| 行为 | 结果 |
| --- | --- |
| 用户存在且未保存主页配置 | 返回默认主页配置，`is_default=true` |
| 用户存在且主页为 `public` | 返回已保存主页配置 |
| 用户存在但主页为 `private` 或 `unlisted` | 返回 403 |
| 用户不存在 | 返回 404 |

### 2.2 按用户名访问公开主页

```bash
curl http://localhost:8080/api/v1/u/alice
```

接口：

```text
GET /api/v1/u/:username
```

该接口适合后续前端做类似博客园的短链接入口。

### 2.3 按用户 ID 访问主页同步内容

```bash
curl "http://localhost:8080/api/v1/space/1001/contents?page=1&page_size=20"
```

接口：

```text
GET /api/v1/space/:user_id/contents
```

该接口只返回公开主页的同步内容。若用户主页为 `private` 或 `unlisted`，返回 403。

### 2.4 按用户名访问主页同步内容

```bash
curl "http://localhost:8080/api/v1/u/alice/contents?page=1&page_size=20"
```

接口：

```text
GET /api/v1/u/:username/contents
```

返回值使用统一列表响应，`items` 中包含同步后的帖子摘要：

| 字段 | 说明 |
| --- | --- |
| `thread_id` | 来源帖子 ID |
| `title` | 来源帖子标题 |
| `excerpt` | 来源帖子内容摘要 |
| `category_id` | 来源版块 ID |
| `tags` | 来源帖子标签 |
| `thread_created_at` | 来源帖子创建时间 |
| `thread_updated_at` | 来源帖子更新时间 |
| `synced_at` | 最近同步时间 |

### 2.5 获取当前登录用户主页

```bash
curl http://localhost:8080/api/v1/spaces/me \
  -H "Authorization: Bearer <access_token>"
```

接口：

```text
GET /api/v1/spaces/me
```

该接口会返回当前登录用户的主页配置。即使配置为 `private`，用户本人也可以查看。

### 2.6 更新当前登录用户主页

```bash
curl -X PUT http://localhost:8080/api/v1/spaces/me \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Alice Space",
    "bio": "记录校园开发和学习笔记",
    "theme": "default",
    "layout": "blog",
    "visibility": "public",
    "sync_enabled": true,
    "sync_categories": ["blog"],
    "sync_tags": ["go", "campusos"]
  }'
```

接口：

```text
PUT /api/v1/spaces/me
```

注意：

| 字段 | 说明 |
| --- | --- |
| `title` | 主页标题，最长 120 字符 |
| `bio` | 主页简介，最长 500 字符 |
| `avatar` | 主页头像 URL |
| `cover_image` | 主页封面图 URL |
| `theme` | 主题标识，后续会与风格包关联 |
| `layout` | 布局标识，当前默认 `blog` |
| `visibility` | 可选 `public`、`unlisted`、`private` |
| `sync_enabled` | 是否启用内容同步 |
| `sync_categories` | 后续内容同步使用的版块/分类筛选标识 |
| `sync_tags` | 后续内容同步使用的标签筛选标识 |

## 3. 数据表

迁移文件：

```text
migrations/000007_add_user_spaces.up.sql
migrations/000007_add_user_spaces.down.sql
```

核心表：

```text
user_spaces
user_space_contents
```

主要字段：

| 字段 | 用途 |
| --- | --- |
| `user_id` | 主页归属用户 |
| `title` / `bio` | 主页标题和简介 |
| `avatar` / `cover_image` | 主页视觉资源 |
| `theme` / `layout` | 风格包导出和后续前端渲染使用 |
| `visibility` | 公开、隐藏链接、私有 |
| `sync_enabled` | 是否参与内容同步 |
| `sync_categories` / `sync_tags` | 内容同步筛选条件 |

`user_space_contents` 记录从社区帖子同步到个人主页的内容摘要。

| 字段 | 用途 |
| --- | --- |
| `user_id` | 内容归属用户 |
| `thread_id` | 来源帖子 |
| `title` / `excerpt` | 主页展示用标题和摘要 |
| `category_id` / `tags` | 筛选和展示字段 |
| `status` | 来源帖子状态 |
| `thread_created_at` / `thread_updated_at` | 来源帖子时间 |
| `synced_at` | 最近同步时间 |

## 4. 同步规则

Space Service 会订阅以下事件：

```text
thread.created
thread.updated
thread.deleted
```

同步规则：

| 条件 | 行为 |
| --- | --- |
| 主页为 `public` 且 `sync_enabled=true` | 允许同步 |
| 主页为 `private` 或 `unlisted` | 不进入公开内容列表 |
| 帖子状态不是 `published` | 从同步内容中移除 |
| `sync_categories` 为空 | 不限制版块 |
| `sync_categories` 非空 | 只同步匹配 `category_id` 的帖子 |
| `sync_tags` 为空 | 不限制标签 |
| `sync_tags` 非空 | 至少一个帖子标签命中才同步 |
| `thread.deleted` | 删除对应同步内容 |

如果用户尚未保存个人主页配置，系统按默认公开主页处理，允许同步其已发布帖子。

## 5. 安全边界

当前阶段不允许用户提交任意 JavaScript 或 HTML 代码。主页配置只保存结构化字段，为后续风格包和受控组件渲染打基础。

登录用户只能通过 `/api/v1/spaces/me` 修改自己的主页配置，不能通过请求体指定其他用户 ID。公开接口只读取 `public` 主页。

## 6. 后续任务

| 优先级 | 任务 |
| --- | --- |
| P1 | 增加用户侧主页管理前端 |
| P1 | 增加管理员查看、禁用和恢复用户主页能力 |
| P0 | 与主页风格包一键应用和回滚能力对接 |
