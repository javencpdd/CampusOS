# CampusOS 完整入门路径

这条路径面向第一次接触 CampusOS 的开发者。完成后，你应能启动四个服务、理解系统为何是“单进程但多模块”、找到课表与风格包管理入口，并完成一次最小改动和验证。

希望按“每阶段完成标志”学习时，先使用 [开发者学习路线](/guide/developer-learning-path)；本页保留完整的
一次性操作说明。

本页在本地文档站的固定地址是：

```text
http://localhost:3002/guide/getting-started
```

## 1. 先理解系统形态

CampusOS 不是单模块应用，也不是一组微服务。它是 **模块化单体**：核心后端通常作为一个 API 进程部署，但内部按依赖、生命周期和数据所有权拆分。

| 类型 | 是否编译进主程序 | 是否可卸载 | 示例 |
| --- | --- | --- | --- |
| Core Module | 是 | 否 | Identity、Community、Moderation、User Storage |
| Built-in Feature | 是 | 否，可启停 | Personal Space、RichText、Personal Schedule、Appearance |
| External Plugin | 否 | 是 | Wasm、受管进程和 Manifest v2 插件 |
| Resource Package | 否，只是数据资源 | 不走插件卸载流程 | 系统主题、首页包、个人主页风格、Skill、Prompt |

`web`、`admin` 和 `docs-site` 是三个独立前端；它们共同访问 Go API。看到一个后端进程，不代表系统只有一个模块。

## 2. 准备开发环境

完整 Docker 路线只要求 Git、Docker 与 Compose v2；原生应用路线另外要求 `go.mod` 对应的 Go、
Node.js 和 pnpm。

```bash
go version
node --version
pnpm --version
docker compose version
git --version
```

获取项目并创建本地配置：

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
./scripts/docker-dev.sh setup
# 编辑 deploy/docker/.env.dev.local
```

Docker 不负责从 GitHub 克隆源码。开发容器绑定挂载这份宿主工作区，平时应在宿主 IDE 中编辑并使用宿主
Git 提交。只有选择原生应用路线时才需要安装三个前端的依赖：

```bash
cd web && pnpm install
cd ../admin && pnpm install
cd ../docs-site && pnpm install
cd ..
```

## 3. 一次启动完整开发环境

只安装 Git 和 Docker 的 Windows/Linux 开发者先按
[Docker 跨平台开发环境](/deployment/docker-development) 执行 `docker-dev.* setup --start`。已经安装本地
Go、Node.js 与 pnpm 时，可在同一份配置和数据上使用下面的原生应用流程。

在仓库根目录执行：

```bash
STOP_EXISTING=true make dev-all
```

该命令会停止完整 Docker 模式的四个应用容器，继续使用同一组 PostgreSQL、Redis、NATS 命名卷与宿主
`data/`，执行 migration 后启动宿主 API、Web、Admin 和 Docs。切回完整 Docker 模式时执行
`./scripts/docker-dev.sh up`；两种模式按单写入者交接，不能同时运行两套 API。

| 服务 | 地址 | 用途 |
| --- | --- | --- |
| 用户前台 | `http://localhost:3000` | 发帖、个人主页、个人课表、用户主题和插件中心 |
| 管理后台 | `http://localhost:3001` | 用户、权限、内容、内置功能、风格包、插件和运维 |
| 官方文档 | `http://localhost:3002` | 当前这套入门、API 和插件开发教程 |
| 后端 API | `http://localhost:8080/api/v1` | 浏览器和扩展调用的统一入口 |

快速检查：

```bash
curl -fsS http://localhost:8080/api/v1/health
curl -fsS -H 'Accept: application/json' http://localhost:8080/api/v1
```

默认开发管理员：

```text
邮箱：admin@campusos.local
密码：Admin@123456
```

该密码只适用于本地开发。共享环境必须修改密码、JWT Secret、数据库密码和第三方凭据。

## 4. 认识管理端入口

登录 `http://localhost:3001` 后，侧栏按业务职责分组。

| 入口 | 路径 | 你会看到什么 |
| --- | --- | --- |
| 角色与权限 | `/permissions` | 系统/自定义角色、稳定 Permission Code、风险等级与授权记录 |
| 版主管理 | `/moderators` | 版主动作上限，以及用户负责的具体板块范围 |
| 内置功能 | `/features` | 个人空间、富文本、个人课表和 Appearance 的状态、生效方式与配置 |
| 外观与风格包 | `/appearance` | 首页包切换/导入/回滚、系统主题目录和个人主页风格边界 |
| 外部插件 | `/plugins` | 插件预检、导入、启停、更新、日志与导出 |
| 插件中心 | `/plugin-center` | 管理员发布目录、用户请求、版本和治理审计 |
| 扩展总览 | `/extensions` | 四类扩展的统一清单，不合并它们的生命周期 |
| 集成中心 | `/integrations` | AI、Webhook、MCP-like 和 Message Local 的能力状态 |

个人课表是 Built-in Feature，所以不出现在“外部插件安装”列表。Admin 只能管理课表功能的开关和公共配置；用户自己的课程内容属于私有数据，只能由该用户在 `http://localhost:3000/schedule` 查看和编辑。

风格包是 Resource Package：

- 首页包由管理员统一切换。
- 系统主题由管理员提供，用户在 `http://localhost:3000/appearance` 自主选择。
- 个人主页风格由主页所有者选择。访问用户 A 的主页时，显示用户 A 保存的风格。

## 5. 走一遍用户端主流程

1. 在 `http://localhost:3000` 注册普通用户或登录已有用户。
2. 打开版块，创建普通文本或富文本帖子。
3. 点击头像进入个人主页、个人主页设置或个人课表。
4. 在 `/schedule` 新建学期，设置第一周，手工录入或导入 Excel/CSV/JSON。
5. 在 `/appearance` 选择管理员提供的系统主题。
6. 在 `/plugins` 查看管理员发布的外部插件并明确授权。

按钮是否显示只是前端体验。后端仍会检查身份、稳定 Permission Code、资源所有权或板块作用域以及操作约束。

## 6. 找到对应代码

| 目标 | 主要目录 |
| --- | --- |
| API 启动与装配 | `cmd/server/`、`internal/server/` |
| HTTP 路由合同 | `internal/transport/httpapi/` |
| 身份与权限 | `internal/modules/core/identity/` |
| 帖子、版块和内容治理 | `internal/modules/core/community/` |
| 个人课表 | `internal/modules/features/schedule/` |
| User Storage | `internal/modules/core/userstorage/` |
| 插件平台 | `internal/plugin/` |
| 风格包与 Appearance | `internal/modules/features/appearance/runtime/`、`internal/modules/features/appearance/stylepack/`、`internal/modules/features/appearance/webtheme/` |
| 用户前台 | `web/src/modules/` |
| 管理后台 | `admin/src/modules/` |
| 官方文档站 | `docs-site/` |

数据目录不要混用：

```text
data/plugins/<id>/                 插件实现和 plugin.yaml
data/plugin_data/<id>/             External Plugin 私有运行数据
data/module_data/<feature>/        Built-in Feature 本地可变数据
data/resources/<type>/<id>/        Resource Package 仓库
data/personal-space/<user-id>/     用户文件、课表和插件用户附件
```

## 7. 完成一次开发闭环

开始修改前：

```bash
git status --short
```

后端基础验证：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
```

按改动范围运行前端：

```bash
cd web && pnpm lint && pnpm format:check && pnpm build
cd ../admin && pnpm test:component && pnpm build
cd ../docs-site && pnpm build
cd ..
```

文档和架构边界：

```bash
make docs-links
make architecture-check
git diff --check
```

版本封板或高风险改动使用统一门禁：

```bash
RUN_RESTORE_DRILL=true RUN_BROWSER_SMOKE=true make release-check
```

不要为了通过测试删除权限、审计、路径、配额、内容清洗或数据约束。

## 8. 根据任务继续阅读

| 你的任务 | 下一页 |
| --- | --- |
| 开发插件 | [以课表为例编写外部插件](/plugins/schedule-plugin-tutorial) |
| 理解插件分类 | [插件体系](/plugins/overview) |
| 调用 HTTP API | [接口约定](/api/overview) |
| 编写风格包 | [风格包、特效与 CampusStyleSDK](/plugins/style-packs) |
| 配置角色、权限或版主 | [权限配置入门](/guide/permission-configuration) |
| 设计新的权限能力 | [系统架构](/guide/architecture) 和仓库内 `v10权限管理设计与使用入门.md` |
| 准备发布 | [构建与发布](/deployment/release) |
| 在 Windows/Linux 用 Docker 开发 | [Docker 跨平台开发环境](/deployment/docker-development) |
| 单主机部署或迁移 | [Docker 单主机部署与迁移](/deployment/docker) |
| 理解历史计划、文档有效性和下一步 | [v1-v14 版本演进](/project/version-evolution)、[文档状态与历史替代](/project/document-lifecycle)、[当前规划与后续路线](/project/current-roadmap) |
| 准备提交代码 | [贡献、Pull Request 与 CI/CD](/contributing/workflow) |

## 9. 常见启动问题

**端口已占用**

重新执行 `STOP_EXISTING=true make dev-all`。完整 Docker 模式在
`deploy/docker/.env.dev.local` 中调整 `CAMPUSOS_DEV_*_PORT`；原生模式也可在命令行覆盖
`WEB_PORT`、`ADMIN_PORT`、`DOCS_PORT` 和 `SERVER_PORT`。

**页面可打开但 API 请求失败**

先检查健康接口，再查看：

```text
.campusos/logs/api.log
.campusos/logs/web.log
.campusos/logs/admin.log
.campusos/logs/docs.log
```

**Admin 看不到课表或风格包**

确认使用当前管理端代码并访问 `/features` 和 `/appearance`。课表用户数据不会出现在 Admin；系统主题的个人选择也在用户前台完成。

**插件中心为空**

这通常表示管理员尚未把已安装 External Plugin 发布到用户目录，不表示 Built-in Feature 丢失。

## 完成标准

- 四个默认地址都能访问。
- 能解释“一个后端进程”和“多个业务模块”的区别。
- 能在 Admin 找到个人课表功能状态和外观资源入口。
- 能指出插件代码、插件数据、资源包和用户文件各自的目录。
- 能按改动范围运行测试，并知道何时使用 `release-check`。

下一步：[以课表为例编写外部插件](/plugins/schedule-plugin-tutorial)。
