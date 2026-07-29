# CampusOS 个人主页 Space 说明

> 文档状态：Personal Space 专项参考，不是新开发者必读入口。
> 当前基线：`v0.13.0`；接口最终以 OpenAPI 和路由矩阵为准。
> 更新时间：2026-07-29

## 1. 功能定位

个人主页 Space 是 `personal-space` Built-in Feature，负责公开主页、主页设置和个人主页风格装配。实现位于 `internal/modules/features/personalspace/`，描述符位于 `modules/features/personal-space/module.yaml`。用户文件的安全路径、所有权和配额由独立的 User Storage Core 负责，资源包位于 `data/resources`。

当前实现重点是稳定数据边界：

| 能力 | 状态 |
| --- | --- |
| 公开主页入口 | 已支持 |
| 用户主页配置持久化 | 已支持 |
| 登录用户编辑自己的主页配置 | 已支持 |
| 按用户名访问主页 | 已支持 |
| 内容事件同步 | 已支持 |
| 公开主页内容列表 | 已支持 |
| 前端主页渲染页 | 已支持 |
| 用户侧主页管理页 | 已支持 |
| JSON 风格包校验、预览、导出和应用 | 已支持 |
| 文件夹/zip 页面拓展风格包校验、示例生成和应用 | 已支持 |
| 受限 HTML/CSS 风格编辑、检测、示例生成和应用 | 已支持 |
| 风格包回滚、恢复默认 | 已支持 |
| 头像上传到个人空间 | 已支持 |
| 默认 10MB 本地个人空间 | 已支持，可通过 Feature 配置调整 |
| 头像源文件保留最近 3 个 | 已支持，可通过 Feature 配置调整 |
| 个人云盘接入 | 后续任务，当前只保留配置占位 |

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

用户侧前端入口：

```text
http://localhost:3000/space/settings
```

公开主页入口：

```text
http://localhost:3000/u/<username>
```

用户侧导航规则：

| 操作 | 路径 |
| --- | --- |
| 点击顶部“个人主页” | 直接进入 `/u/<username>` 公开主页 |
| 点击自己的头像 | 进入 `/space/settings` 主页设置 |

### 2.7 查看个人空间存储状态

```bash
curl http://localhost:8080/api/v1/spaces/me/storage \
  -H "Authorization: Bearer <access_token>"
```

接口：

```text
GET /api/v1/spaces/me/storage
```

返回字段：

| 字段 | 说明 |
| --- | --- |
| `quota_bytes` | 当前用户本地个人空间配额，默认 10MB |
| `used_bytes` | 已使用字节数 |
| `available_bytes` | 剩余可用字节数 |
| `avatar_keep_limit` | 头像源文件保留数量，默认 3 |

### 2.8 上传个人主页头像

```bash
curl -X POST http://localhost:8080/api/v1/spaces/me/avatar \
  -H "Authorization: Bearer <access_token>" \
  -F "file=@avatar.png"
```

接口：

```text
POST /api/v1/spaces/me/avatar
```

规则：

| 项目 | 当前默认 |
| --- | --- |
| 支持格式 | `jpeg`、`png`、`gif`、`webp` |
| 单文件上限 | 2MB |
| 用户默认配额 | 10MB |
| 源文件保留 | 最近 3 个 |
| 存储目录 | `data/personal-space/<user_id>/img/avatars/` |

上传成功后，`user_spaces.avatar` 会更新为公开头像 URL，公开主页和顶部头像会优先使用该 URL。

受控富文本图文文章的上传图片保存在同一用户目录的 `img/richtext/`，同样计入默认 10MB 的个人空间配额。

### 2.9 访问头像文件

```text
GET /api/v1/spaces/files/:user_id/avatars/:filename
```

文件名和用户 ID 都会经过路径段校验，避免路径穿越。

### 2.10 HTML 风格检测、示例和应用

个人空间允许用户在风格包基础上编写受限 HTML 片段，用于更开放地定制公开个人主页。该能力归属于 Personal Space Feature；Feature 停用后页面接口会被 Gate 拒绝，但 User Storage、课表、富文本资产和已有数据保持不变。

检测接口：

```bash
curl -X POST http://localhost:8080/api/v1/spaces/me/styles/html/validate \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"html":"<section><h2>Alice Space</h2></section>"}'
```

接口：

```text
POST /api/v1/spaces/me/styles/html/validate
```

基于当前风格生成示例：

```text
GET /api/v1/spaces/me/styles/html-example
```

应用 HTML 风格：

```text
POST /api/v1/spaces/me/styles/html/apply
```

应用接口会先把 HTML 写入当前风格 manifest 的 `custom_html_enabled` 和 `custom_html` 字段，再走风格包完整校验、快照保存和持久化流程。检测失败时不会写入 `user_spaces.style_manifest`。

当前允许的是受限 HTML 子集，不允许脚本、事件处理器、`javascript:` / `data:` / `file:` URL、危险 CSS、过深嵌套或过大片段。公开主页接口在返回历史 `style_manifest` 前也会再次检查，不会把历史不合规 HTML 原样交给前端 `v-html`。

### 2.11 页面拓展风格包检测、示例和应用

页面拓展风格包使用 `page-style-pack.v1` 文件夹/zip 标准。用户可以在 `/space/settings` 下载基于当前风格生成的示例 zip，修改其中的 `templates/page.html`、`templates/card.html`、`styles/theme.css`、`assets/` 和 `config.schema.json`，再上传 zip 进行筛查和应用。后端会检测所有 HTML/CSS 文件、声明资源、相对路径和配置 schema，通过后才会应用。

接口：

```text
POST /api/v1/spaces/me/styles/packs/validate
GET  /api/v1/spaces/me/styles/packs/example
GET  /api/v1/spaces/me/styles/packs/example.zip
POST /api/v1/spaces/me/styles/packs/apply
POST /api/v1/spaces/me/styles/packs/apply-source
```

`apply-source` 用于应用源码目录中的内置风格包：

```text
data/resources/space-style-packs/<name>/
```

筛查通过后，服务会把风格包中的 HTML/CSS 编译到当前用户的 `style_manifest.custom_html`、`style_manifest.custom_css` 和 `style_manifest.source_style_pack`，并继续复用应用前快照、回滚和恢复默认流程。

## 3. 数据表

迁移文件：

```text
migrations/000007_add_user_spaces.up.sql
migrations/000007_add_user_spaces.down.sql
migrations/000009_add_user_space_styles.up.sql
migrations/000009_add_user_space_styles.down.sql
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
| `style_name` / `style_version` | 当前已应用风格包的名称和版本 |
| `style_manifest` | 当前已应用风格包的规范化 manifest，可包含检测通过的 `custom_html`、`custom_css` 和 `source_style_pack` |
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

个人空间文件当前不新增数据库表。头像 URL 写入 `user_spaces.avatar`，文件本体由 User Storage Core 保存在 Feature 配置指定的本地根目录。后续如接入个人云盘，可以在不改变公开主页字段的前提下扩展 Provider 配置和文件索引表。

## 3.1 Feature 配置

默认配置描述符位于：

```text
modules/features/personal-space/module.yaml
```

关键配置：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `styles_dir` | `data/module_data/personal-space/styles` | 默认 JSON 风格包数据目录 |
| `file_root` | `data/personal-space` | 个人空间本地文件根目录 |
| `file_url_prefix` | `/api/v1/spaces/files` | 文件公开访问 URL 前缀 |
| `default_quota_mb` | `10` | 每个用户初始本地空间 |
| `avatar_keep_limit` | `3` | 头像源文件保留数量 |
| `max_avatar_mb` | `2` | 单个头像大小上限 |
| `future_storage_provider` | `local` | 后续个人云盘接入预留字段 |

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

当前阶段不允许用户提交任意 JavaScript 或未审查 HTML 代码。个人主页只渲染后端安全检测通过的受限 HTML 子集，脚本、事件处理器、不安全 URL 和危险 CSS 会被拒绝。

登录用户只能通过 `/api/v1/spaces/me` 修改自己的主页配置，不能通过请求体指定其他用户 ID。公开接口只读取 `public` 主页。

## 6. 前端页面

当前 `web/` 已提供两个用户侧页面：

| 页面 | 路径 | 作用 |
| --- | --- | --- |
| 主页设置 | `/space/settings` | 编辑主页配置、选择示例风格包、校验、预览、应用、导出风格和编辑受限 HTML |
| 公开主页 | `/u/:username` | 按用户名展示用户个人主页、同步内容、当前风格 token 和检测通过的 HTML 片段 |

前端示例风格包位于：

```text
web/src/data/spaceStyleExamples.ts
```

后端同源 JSON 示例位于：

```text
data/module_data/personal-space/styles/
```

文件夹拓展风格包示例位于：

```text
data/resources/space-style-packs/
```

## 7. 后续任务

| 优先级 | 任务 |
| --- | --- |
| P1 | 增加站内风格分享列表和管理员审核入口 |
