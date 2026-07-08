你现在是一个资深全栈工程师，请帮我在我的 GitHub 项目中完成一个功能设计与代码实现。

项目地址：

https://github.com/javencpdd/CampusOS

我的需求是：将“受控富文本图文帖子”功能以插件形式集成到 CampusOS 项目中。当该插件启用后，系统中的所有帖子默认都使用“受控富文本文章”的框架，包括富文本编辑、图片插入、草稿保存、发布、预览、详情页渲染和 HTML 安全清洗。

请你先完整阅读并分析项目结构，不要直接假设技术栈。需要重点查看：

1. 项目的后端入口、路由结构、模块划分。
2. 当前是否已有帖子、社区、文章、动态、论坛等相关模块。
3. 当前数据库结构、migrations 目录、ORM 或 SQL 使用方式。
4. 当前是否已有插件机制。如果没有，请设计一个轻量插件机制。
5. 当前前端或管理端结构，例如 admin、web、sdk 等目录。
6. 当前鉴权、用户 ID、角色权限、文件上传方式是否已经存在。

## 一、核心目标

我要实现一个插件：

```text
RichTextArticlePlugin / controlled-richtext-article
```

插件启用后：

```text
普通帖子 → 默认变成受控富文本图文帖子
```

也就是说，帖子不再只是简单的纯文本内容，而是统一走下面的模型：

```text
标题
摘要
封面图
受控富文本正文
正文图片
草稿状态
发布状态
预览渲染
安全清洗后的 HTML
文章详情页统一渲染
```

## 二、实现原则

请遵守这些原则：

1. 插件化集成，尽量不要破坏现有项目结构。
2. 如果项目已有插件机制，优先复用。
3. 如果项目没有插件机制，请设计一个轻量级插件注册系统。
4. 不要大规模重构无关模块。
5. 保留现有帖子模块能力，但在插件启用后让帖子默认走富文本文章流程。
6. 需要兼容已有帖子数据，避免旧数据无法访问。
7. 受控富文本 HTML 必须在后端清洗后才能发布或展示。
8. 图片资源和正文内容分开管理。
9. 前端编辑器可以先用简单方案，后续再替换为 Tiptap / ProseMirror。
10. 先完成 MVP 闭环，再考虑高级功能。

## 三、推荐数据设计

请根据项目实际情况决定是扩展原有 posts 表，还是新增插件表。

如果项目已有 posts 表，优先采用低侵入方式：

```text
posts 仍然作为帖子主表
新增 richtext_article_contents 表保存富文本内容
```

推荐新增表：

```sql
CREATE TABLE richtext_article_contents (
    id BIGSERIAL PRIMARY KEY,
    post_id BIGINT NOT NULL,

    title VARCHAR(255) NOT NULL,
    summary TEXT,
    cover_url TEXT,

    content_html TEXT,
    content_json JSONB,

    sanitized_html TEXT,

    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    -- draft
    -- published
    -- offline
    -- deleted

    created_by BIGINT NOT NULL,
    updated_by BIGINT,

    published_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

推荐新增图片资源表：

```sql
CREATE TABLE richtext_article_assets (
    id BIGSERIAL PRIMARY KEY,

    post_id BIGINT NULL,
    article_content_id BIGINT NULL,
    uploader_id BIGINT NOT NULL,

    file_url TEXT NOT NULL,
    file_name VARCHAR(255),
    file_size BIGINT,
    mime_type VARCHAR(100),

    width INT,
    height INT,

    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

如果项目原有 posts 表已经包含 title、content、status 等字段，请你自行评估是否复用这些字段，但需要给出理由。

## 四、插件开关设计

需要支持通过配置启用或禁用该插件。

可以是类似：

```yaml
plugins:
  controlled_richtext_article:
    enabled: true
```

或者：

```env
CAMPUSOS_PLUGIN_CONTROLLED_RICHTEXT_ARTICLE=true
```

请根据项目现有配置方式选择最合适的实现。

当插件启用后：

```text
1. 创建帖子时默认创建 richtext_article_contents 记录。
2. 编辑帖子时默认使用富文本编辑接口。
3. 获取帖子详情时默认返回富文本文章结构。
4. 发布帖子时必须经过 HTML sanitizer。
5. 文章详情页使用统一 article-content 容器渲染。
```

当插件禁用后：

```text
1. 系统应尽量回退到原有帖子逻辑。
2. 已有富文本文章数据不应被删除。
3. 如果访问富文本帖子，可以返回降级后的纯文本或保留 HTML 内容。
```

## 五、后端接口设计

请根据项目现有路由风格实现接口，不要机械照搬路径。如果项目已有 posts API，请尽量在原 API 上扩展。

建议接口：

### 1. 创建富文本草稿

```http
POST /api/posts/richtext
```

请求示例：

```json
{
  "title": "文章标题",
  "summary": "文章摘要",
  "coverUrl": "https://example.com/cover.jpg",
  "contentJson": {},
  "contentHtml": "<p>正文内容</p>"
}
```

返回：

```json
{
  "postId": 1,
  "articleContentId": 1,
  "status": "draft"
}
```

### 2. 更新富文本草稿

```http
PUT /api/posts/{postId}/richtext
```

请求：

```json
{
  "title": "新的标题",
  "summary": "新的摘要",
  "coverUrl": "https://example.com/cover.jpg",
  "contentJson": {},
  "contentHtml": "<p>新的正文内容</p>"
}
```

### 3. 上传正文图片

```http
POST /api/posts/richtext/assets
```

使用 multipart/form-data：

```text
file: 图片文件
postId: 可选
articleContentId: 可选
```

返回：

```json
{
  "url": "https://example.com/uploads/article/xxx.jpg",
  "width": 1080,
  "height": 720,
  "mimeType": "image/jpeg"
}
```

### 4. 预览富文本文章

```http
POST /api/posts/richtext/preview
```

请求：

```json
{
  "contentHtml": "<p>正文内容</p>"
}
```

返回：

```json
{
  "sanitizedHtml": "<p>正文内容</p>",
  "renderHtml": "<article class=\"article-content\">...</article>"
}
```

### 5. 发布文章

```http
POST /api/posts/{postId}/richtext/publish
```

发布时必须执行：

```text
1. 校验作者权限。
2. 校验标题不能为空。
3. 校验正文不能为空。
4. 清洗 content_html。
5. 保存 sanitized_html。
6. 将状态改为 published。
7. 设置 published_at。
```

### 6. 下架文章

```http
POST /api/posts/{postId}/richtext/offline
```

### 7. 获取文章详情

```http
GET /api/posts/{postId}
```

插件启用后，返回结果中需要包含：

```json
{
  "postId": 1,
  "type": "richtext_article",
  "title": "文章标题",
  "summary": "文章摘要",
  "coverUrl": "https://example.com/cover.jpg",
  "contentHtml": "<p>清洗后的正文</p>",
  "status": "published",
  "authorId": 1001,
  "publishedAt": "2026-07-08T10:00:00Z"
}
```

## 六、HTML 安全清洗要求

后端必须实现 HTML sanitizer。

如果后端是 Go，优先使用：

```text
github.com/microcosm-cc/bluemonday
```

如果后端是 Node.js，优先使用：

```text
sanitize-html
```

如果后端是 Java，选择合适的 HTML sanitizer 库。

允许的标签建议包括：

```text
p
br
strong
em
span
h1
h2
h3
ul
ol
li
blockquote
img
a
section
div
pre
code
table
thead
tbody
tr
td
th
```

禁止：

```text
script
iframe
object
embed
form
input
button
style
link
meta
svg
math
```

禁止事件属性：

```text
onclick
onerror
onload
onmouseover
onfocus
```

禁止危险链接：

```text
javascript:
data:
vbscript:
```

图片只允许：

```text
http
https
站内上传资源 URL
```

发布、预览、详情返回前都必须使用清洗后的 HTML，不允许直接返回用户原始 HTML。

## 七、前端功能要求

请根据项目实际前端技术栈实现。如果当前前端还没有完整帖子编辑页面，可以先提供一个最小页面或组件。

需要实现：

```text
1. 标题输入框
2. 摘要输入框
3. 封面图上传
4. 富文本正文编辑器
5. 正文图片上传
6. 保存草稿按钮
7. 预览按钮
8. 发布按钮
9. 文章详情页渲染
```

第一版编辑器可以使用：

```text
textarea + 简单 HTML 输入
```

或者：

```text
wangEditor / Quill / Tiptap
```

请优先选择与项目当前前端技术栈最容易集成的方案。

文章详情页需要使用统一容器：

```html
<article class="article-content">
  <!-- sanitized_html -->
</article>
```

推荐基础样式：

```css
.article-content {
  max-width: 760px;
  margin: 0 auto;
  padding: 16px;
  font-size: 16px;
  line-height: 1.8;
  color: #222;
}

.article-content p {
  margin: 12px 0;
}

.article-content img {
  max-width: 100%;
  height: auto;
  display: block;
  margin: 16px auto;
  border-radius: 8px;
}

.article-content h1,
.article-content h2,
.article-content h3 {
  margin: 24px 0 12px;
  line-height: 1.5;
}

.article-content blockquote {
  margin: 16px 0;
  padding: 12px 16px;
  background: #f6f8fa;
  border-left: 4px solid #ddd;
}
```

## 八、插件架构建议

如果项目没有插件系统，请设计最小插件机制。

例如后端可以设计：

```go
type Plugin interface {
    Name() string
    Enabled() bool
    RegisterRoutes(router Router)
    Migrate(db DB) error
}
```

然后创建：

```text
internal/plugins/
  registry.go
  controlledrichtext/
    plugin.go
    handler.go
    service.go
    repository.go
    sanitizer.go
    asset.go
    model.go
```

插件注册逻辑类似：

```go
registry.Register(controlledrichtext.NewPlugin(config, db))
```

应用启动时：

```go
for _, plugin := range registry.EnabledPlugins() {
    plugin.Migrate(db)
    plugin.RegisterRoutes(router)
}
```

如果项目已有路由、依赖注入、模块初始化方式，请按照项目原有风格实现，不要强行套用上面的代码。

## 九、目录结构建议

请根据实际项目调整。

后端推荐：

```text
campusos-server/
  internal/
    plugins/
      registry.go
      controlledrichtext/
        plugin.go
        handler.go
        service.go
        repository.go
        model.go
        sanitizer.go
        asset.go
```

数据库迁移推荐：

```text
migrations/
  xxxx_create_richtext_article_contents.up.sql
  xxxx_create_richtext_article_contents.down.sql
  xxxx_create_richtext_article_assets.up.sql
  xxxx_create_richtext_article_assets.down.sql
```

前端推荐：

```text
admin/src/
  pages/
    posts/
      RichTextPostEditor.vue
      RichTextPostPreview.vue
      RichTextPostDetail.vue

  components/
    richtext/
      RichTextEditor.vue
      CoverUploader.vue
      ArticleRenderer.vue
```

如果项目不是 Vue，请按实际技术栈调整。

## 十、兼容已有帖子

需要考虑已有普通帖子。

插件启用后：

```text
1. 新创建的帖子默认 type = richtext_article。
2. 老帖子仍然可以正常展示。
3. 如果老帖子没有 richtext_article_contents 记录，则使用原来的 content 字段展示。
4. 可以提供一个迁移函数，把老帖子 content 转换成简单的 richtext HTML：
   原 content → <p>原 content</p>
```

如果项目没有 post type 字段，可以新增：

```sql
ALTER TABLE posts ADD COLUMN content_type VARCHAR(32) DEFAULT 'plain_text';
```

新帖子默认：

```text
content_type = richtext_article
```

老帖子默认：

```text
content_type = plain_text
```

但是否需要新增字段，请你分析项目现有 posts 表后再决定。

## 十一、权限要求

请复用项目现有鉴权机制。

基本规则：

```text
1. 登录用户才能创建草稿。
2. 作者本人可以编辑自己的草稿。
3. 作者本人可以发布、下架自己的文章。
4. 管理员可以管理所有文章。
5. 未发布草稿不能被普通用户访问。
6. 已发布文章所有用户可访问。
```

## 十二、测试要求

请补充必要测试。

至少包括：

```text
1. 创建富文本草稿成功。
2. 更新富文本草稿成功。
3. 发布时 HTML 被清洗。
4. script 标签会被删除。
5. onerror 等事件属性会被删除。
6. 非作者不能修改文章。
7. 已发布文章可以正常获取详情。
8. 插件禁用时不影响原帖子接口。
```

## 十三、交付要求

请按以下顺序完成：

1. 分析项目当前结构，并说明帖子相关模块在哪里。
2. 判断是否已有插件机制。
3. 给出最终设计方案。
4. 创建数据库迁移。
5. 实现插件注册机制，或接入已有插件机制。
6. 实现 controlled-richtext-article 插件。
7. 实现后端 API。
8. 实现 HTML sanitizer。
9. 实现图片上传逻辑，优先复用项目已有上传模块。
10. 实现前端最小可用编辑、预览、详情展示。
11. 补充测试。
12. 给出运行方式和验证步骤。

## 十四、验收标准

最终需要满足：

```text
1. 项目可以正常启动。
2. 插件可以通过配置开启或关闭。
3. 开启插件后，新帖子默认使用受控富文本文章结构。
4. 可以创建草稿。
5. 可以编辑标题、摘要、封面、正文。
6. 可以上传并插入图片。
7. 可以预览文章。
8. 可以发布文章。
9. 文章详情页展示的是清洗后的 HTML。
10. 恶意 HTML 不会被执行。
11. 旧帖子不会因为插件启用而无法访问。
12. 数据库 migration 可以正常执行和回滚。
```

请直接开始分析仓库并实现代码。实现过程中，如果发现项目结构和上述设想不一致，请优先遵循项目现有架构，并说明你做出的调整原因。
