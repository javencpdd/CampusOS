# CampusOS Official Docs

这是 CampusOS 官方文档的独立 VitePress 前端。它不依赖 Admin 登录或 CampusOS API，可以在后续迁移到独立仓库和域名。

```bash
pnpm install
pnpm dev
```

默认地址：`http://localhost:3002`

新开发者入口：`http://localhost:3002/guide/getting-started`

权限配置入门：`http://localhost:3002/guide/permission-configuration`

课表插件教程：`http://localhost:3002/plugins/schedule-plugin-tutorial`

版本演进：`http://localhost:3002/project/version-evolution`

当前规划：`http://localhost:3002/project/current-roadmap`

生产构建：

```bash
pnpm build
pnpm preview
```

文档正文直接保存在本目录的 Markdown 文件中；导航和站点配置位于 `.vitepress/config.mts`。
