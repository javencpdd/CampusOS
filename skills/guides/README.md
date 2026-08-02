# CampusOS Skills 文档索引

> 更新时间：2026-08-03

本目录集中保存项目 Skills 的调用、维护、验证和同步说明。Skill 规范源文件位于 `skills/sources/<skill-name>/`，
仓库级自动发现入口位于 `.agents/skills/`。从 GitHub clone 后不需要再复制到用户系统目录。

| Skill | 使用说明 | 作用 |
| --- | --- | --- |
| `campusos-readme-update` | [README 更新 Skill 使用说明](CampusOS-README更新Skill使用说明.md) | 保持根 README 精简，将详细内容路由到正确的 docs 分类并检查入口可达性。 |
| `campusos-project-onboarding` | [项目上手 Skill 使用说明](CampusOS-项目上手Skill使用说明.md) | 快速核对当前版本、模块、migration、文档和验证命令。 |
| `campusos-dev-nocommit` / `campusos-dev-workflow` | [开发流程 Skill 使用说明](CampusOS-dev流程Skill使用说明.md) | 按版本阶段完成实现、验证和进度文档。 |
| `campusos-data-architecture-sync` | [数据架构同步 Skill 使用说明](CampusOS-数据架构同步Skill使用说明.md) | 检查 migration、系统表和管理端数据架构视图是否漂移。 |
| `campusos-docker-development` | [Docker 开发 Skill 使用说明](CampusOS-Docker开发Skill使用说明.md) | 处理 Windows/Linux Docker 首配、热更新、代理、LAN、日志和重建边界。 |
| `campusos-webui-regression` | [WebUI 回归 Skill 使用说明](CampusOS-WebUI回归Skill使用说明.md) | 从 Vue、HTTP、Go、PostgreSQL 和运行栈跨层定位 UI 问题。 |
| `campusos-skill-repository-sync` | [仓库 Skill 映射与直用说明](CampusOS-仓库Skill映射与直用说明.md) | 同步规范源文件、使用说明和 `.agents/skills` 可移植发现桥接。 |

完整适用性、修订原因和后续候选见 [Skill 适用性审计与规划](CampusOS-Skill适用性审计与规划.md)。

新增 Skill 使用说明统一放在本目录。每个 `SKILL.md` 正文和对应使用说明必须包含更新时间。历史进度若引用
旧路径，应通过规范入口或迁移说明保持可达，不改写历史验收事实。
