# CampusOS 项目官网与官方文档

> 更新时间：2026-10-28（Asia/Shanghai）

本目录是独立的 VitePress 站点：`/` 为项目官网，使用产品场景、能力介绍、开发入口与当前进展组织内容；其余路径提供官方教程。站点不依赖 Admin 登录、CampusOS API、外部字体或在线资源服务。

在仓库根目录运行：

```bash
pnpm --dir docs-site install --frozen-lockfile
pnpm --dir docs-site dev
```

默认地址：`http://localhost:3002`。

| 入口 | 路径 |
| --- | --- |
| 项目官网 | `/` |
| 项目介绍与文档 | `/guide/introduction` |
| 完整入门 | `/guide/getting-started` |
| 开发者学习路线 | `/guide/developer-learning-path` |
| 知识检索与有据问答 | `/guide/public-knowledge-search`、`/guide/cited-rag` |
| v5 插件开发 | `/plugins/overview` |
| 当前进展 | `/project/current-roadmap` |

构建与预览：

```bash
pnpm --dir docs-site build
pnpm --dir docs-site preview
```

文档正文位于本目录的 Markdown 文件，导航与元信息位于 `.vitepress/config.mts`。官网布局与交互位于 `.vitepress/theme/CampusHome.vue`，样式由 `home.css` 管理；`custom.css` 保留文档阅读样式。官网场景为明确标注的静态示意，不连接真实社区或模型。

维护时同步[项目介绍](guide/introduction.md)、[当前进展](project/current-roadmap.md)、仓库根 README 与 `docs/README.md`。运行版本标识保持与 `internal/platform/version/version.go` 一致；开发阶段由 `config/project-status.json` 明确选择主计划，`make version-check` 分别校验两者及四个入口。不能从运行版本推断开发阶段，也不能因为更新页面而宣布正式发布。
