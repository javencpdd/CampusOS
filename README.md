# CampusOS

CampusOS 是一个基于 Go + Vue 3 的校园社区系统，包含用户社区、管理后台、可扩展插件运行时、个人空间、页面风格包和受控集成能力。

## 当前状态

当前开发基线为 `v0.6.8-dev`。

- 社区：注册、登录、版块与默认标签、普通文本帖子、受控富文本图文文章、带楼层号的回复、私密可见和管理端治理。
- 个人能力：公开个人主页、头像和每用户本地存储、风格包、按学期分离的课表和日历浏览。
- 风格系统：个人主页风格归主页所有者；管理员可提供覆盖完整用户前台的系统主题，用户按本机账号选择；动态特效和只读数据调用运行在权限受控沙箱中。
- 插件：支持 Built-in、gRPC process 和 Wasm Runtime，提供 Host API v1 权限目录、Go SDK、CLI、Mock Host、三类模板、更新前快照和管理端回滚；系统级插件重启生效，用户级插件可热加载。
- 权限：角色操作和插件、集成、日志、富文本等管理权限已拆分；版主按指定版块治理；运行中插件使用可过期、可轮换和可撤销的 Host token。
- 集成：AI Gateway、Webhook 投递、内部 MCP-like 只读工具和 Message local adapter。

项目当前**尚未**提供标准 MCP Server、真实 Discord/OneBot 适配器、公开插件市场、任意 JavaScript 页面风格或生产级高可用部署。下一阶段边界见 [v6 计划书](docs/项目计划v6/01-v6版本计划书.md)。

## 快速开始

需要：Go、Node.js + pnpm、Docker Compose，以及可用的 PostgreSQL/Redis/NATS 开发环境。

```bash
cp .env.example .env
STOP_EXISTING=true make dev-all
```

开发入口：

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |

环境变量、开发账号、pgAdmin、手动启动、数据库迁移和故障排查见 [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

## 仓库结构

| 路径 | 作用 |
| --- | --- |
| `cmd/server/` | API 服务入口 |
| `internal/` | 社区、身份、插件、个人空间、课表、富文本、AI 和集成服务 |
| `web/` | 用户前台 |
| `admin/` | 管理后台 |
| `docs-site/` | 可独立部署和迁移的官方文档前端 |
| `data/plugins/` | 内置和已安装插件的代码与 manifest |
| `data/plugin_data/` | 插件运行数据和可编辑风格包源码 |
| `data/personal-space/<user_id>/` | 用户文件、图片、课表和文档 |
| `docs/` | 计划、帮助、架构、API 和进度记录 |

## 验证

```bash
make release-check
```

完整的前端构建、migration、smoke、贡献和 PR 命令见 [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

## 文档

面向使用者和插件开发者的文档由 `docs-site/` 提供，本地地址为 `http://localhost:3002`。仓库内的计划、进度和内部维护资料从 [文档门户](docs/README.md) 开始。

| 入口 | 文档 |
| --- | --- |
| 架构和数据边界 | [当前架构概览](docs/architecture/当前架构概览.md) |
| 当前 API 分组和契约状态 | [API 索引](docs/api/API索引.md) |
| 插件位置、内置插件和生命周期 | [插件保存位置与当前插件作用汇总](docs/help/插件相关/插件保存位置与当前插件作用汇总.md)、[插件分级与生命周期说明](docs/help/插件相关/插件分级与生命周期说明.md) |
| 风格包边界和 SDK 权限 | [风格包能力边界与 CampusStyleSDK 说明](docs/help/系统设计相关/风格包能力边界与CampusStyleSDK说明.md) |
| 项目 Skills | [Skills 文档索引](docs/skills/README.md) |
| 当前计划与进度 | [v6 计划书](docs/项目计划v6/01-v6版本计划书.md)、[v0.6.8 版本验收进度](docs/进度/v0.6-dev/v0.6.8-dev.md) |

## License

见 [LICENSE](LICENSE)。
