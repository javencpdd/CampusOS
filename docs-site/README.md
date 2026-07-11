# CampusOS Official Docs

这是 CampusOS 官方文档的独立 VitePress 前端。它不依赖 Admin 登录或 CampusOS API，可以在后续迁移到独立仓库和域名。

```bash
pnpm install
pnpm dev
```

默认地址：`http://localhost:3002`

生产构建：

```bash
pnpm build
pnpm preview
```

文档正文直接保存在本目录的 Markdown 文件中；导航和站点配置位于 `.vitepress/config.mts`。
