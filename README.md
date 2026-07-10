# CampusOS

CampusOS 是一个基于 Go + Vue 3 的校园社区系统，包含用户社区、管理后台、可扩展插件运行时、个人空间、页面风格包和受控集成能力。

## 当前状态

当前开发基线为 `v0.5.33-dev`。

- 社区：注册、登录、版块与默认标签、普通文本帖子、受控富文本图文文章、带楼层号的回复、私密可见和管理端治理。
- 个人能力：公开个人主页、头像和每用户本地存储、风格包、按学期分离的课表和日历浏览。
- 插件：支持内置、gRPC 和 Wasm Runtime，支持插件包预检/导入/导出；系统级插件重启 API 后生效，用户级插件可由管理员加载和重载。
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
| API | `http://localhost:8080/api/v1` |
| pgAdmin | 默认 `http://localhost:5050` |

默认管理员为 `admin@campusos.local` / `Admin@123456`，仅限本地开发，部署前必须修改。

环境变量、手动启动、数据库迁移、验证命令和贡献脚本见 [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

## 仓库结构

| 路径 | 作用 |
| --- | --- |
| `cmd/server/` | API 服务入口 |
| `internal/` | 社区、身份、插件、个人空间、课表、富文本、AI 和集成服务 |
| `web/` | 用户前台 |
| `admin/` | 管理后台 |
| `data/plugins/` | 内置和已安装插件的代码与 manifest |
| `data/plugin_data/` | 插件运行数据和可编辑风格包源码 |
| `data/personal-space/<user_id>/` | 用户文件、图片、课表和文档 |
| `docs/` | 计划、帮助、架构、API 和进度记录 |

## 关键命令

```bash
make docker-up
make migrate-up
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
(cd web && pnpm build)
(cd admin && pnpm build)
```

`STOP_EXISTING=true make dev-all` 会先清理占用开发端口的旧进程，再启动当前工作区服务。贡献者可使用 `./sh/git_commit.sh "提交说明"` 和 `./sh/git_pr.sh -t "PR 标题"`；参数和检查要求见上方开发指南。

## 文档

从 [文档门户](docs/README.md) 开始。

| 需求 | 文档 |
| --- | --- |
| 架构和数据边界 | [当前架构概览](docs/architecture/当前架构概览.md) |
| 当前 API 分组和契约状态 | [API 索引](docs/api/API索引.md) |
| 插件位置、内置插件和生命周期 | [插件保存位置与当前插件作用汇总](docs/help/插件相关/插件保存位置与当前插件作用汇总.md)、[插件分级与生命周期说明](docs/help/插件相关/插件分级与生命周期说明.md) |
| 风格包操作 | [风格包切换方式说明](docs/help/系统设计相关/风格包切换方式说明.md) |
| 外部协议适配 | [接口协议适配器标准说明](docs/help/系统设计相关/接口协议适配器标准说明.md) |
| v0.5 实现记录 | [v0.5 第二版计划书](docs/项目计划v5/01-v5版本计划书第二版.md)、[最新 v0.5 进度](docs/进度/v0.5-dev/v0.5.33-dev.md) |
| 历史审查和下一阶段 | [v1-v5 计划审查](docs/项目计划v6/00-v1-v5计划审查.md)、[v6 计划书](docs/项目计划v6/01-v6版本计划书.md) |

## License

见 [LICENSE](LICENSE)。
