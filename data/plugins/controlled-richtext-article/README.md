# controlled-richtext-article

`controlled-richtext-article` is a built-in CampusOS plugin that turns new user threads into controlled image-text articles when enabled.

It keeps `threads` as the top-level community record and stores rich content in:

```text
richtext_article_contents
richtext_article_assets
data/images/richtext/
```

The MVP editor uses a controlled HTML textarea instead of bundling a heavy WYSIWYG editor. This keeps the first version easy to audit. Later versions can swap the editor surface to Tiptap, wangEditor, BlockNote, or Quill while preserving the backend API and sanitizer.

## Public and User APIs

```text
GET    /api/v1/richtext/status
GET    /api/v1/richtext/articles/:id
GET    /api/v1/richtext/assets/:user_id/:filename

POST   /api/v1/richtext/articles
GET    /api/v1/richtext/articles/:id/me
PUT    /api/v1/richtext/articles/:id
POST   /api/v1/richtext/preview
POST   /api/v1/richtext/assets
POST   /api/v1/richtext/articles/:id/publish
POST   /api/v1/richtext/articles/:id/offline
DELETE /api/v1/richtext/articles/:id

POST   /api/v1/richtext/articles/:id/admin/offline
POST   /api/v1/richtext/articles/:id/admin/restore
DELETE /api/v1/richtext/articles/:id/admin
```

## Safety Boundary

The backend sanitizes HTML during draft save, preview, publish, and detail rendering. Scripts, event handlers, unsafe URLs, unsupported tags, and dangerous CSS are removed before HTML is returned to the frontend.

Disabling the plugin stops richtext create/edit/preview/read/publish APIs and image asset access, but does not delete stored article data. Existing plain text threads continue to use the original thread APIs.
