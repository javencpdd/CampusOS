---
name: senior-developer
description: "Apply CampusOS's senior-engineering delivery discipline to an implementation task: scope a small vertical slice, preserve module and security boundaries, validate each change immediately, synchronize evidence, and leave changes uncommitted unless the user explicitly asks to commit."
---

# CampusOS 高级开发工程师工作法

> 更新时间：2026-09-23（Asia/Shanghai）

将“先理解、再小步实现、立即验证、如实交付”的工程习惯落实到 CampusOS 的 Go、Vue、PostgreSQL、Docker 与插件架构中。本 Skill 是工程协作规范，不替代系统、开发者、用户指令，亦不扩大用户授权范围。

## 适用范围

适用于已明确目标的 CampusOS 功能开发、缺陷修复、跨层回归和实现审查，尤其是涉及 Web/Admin、HTTP、Core/Feature、存储、权限、迁移或 Docker 的任务。

- 上下文、当前阶段或计划不清楚时，先使用 `$campusos-project-onboarding`。
- 普通未提交开发使用 `$campusos-dev-nocommit`；用户明确要求本地提交时才使用 `$campusos-dev-workflow`。
- Docker、UI 回归、迁移与 README/文档路由分别优先使用对应 CampusOS 专项 Skill。本 Skill 负责把它们组织成小步可验证的交付，不替代专项约束。

## 1. 开工前：建立可证明的最小范围

1. 读取用户明确引用的计划、最新进度和受影响模块的真实代码；检查 `git status -sb` 与当前分支，不覆盖无关改动。
2. 用不超过五项的计划说明“问题 → 最小纵向切片 → 验证证据”。用户要求安静执行时，不发送阶段性进度，只在最终交付集中报告。
3. 先写下不变边界：例如 `author_id` 仍是权限事实、插件不直接访问平台数据库、用户文件不暴露物理路径、迁移不能静默破坏真实数据。
4. 对不明确且会显著改变数据、权限、外部系统或产品行为的选择，先请求用户决定；普通实现细节按现有代码和文档作最小合理假设。

## 2. 小步实现与验证循环

一次只完成一条可独立验证的业务路径。每个切片严格执行“读 → 写 → 验 → 记录”：

1. **读**：定位入口、调用链、数据事实、错误合同和已有测试；先确认根因，不依据界面现象猜测改动点。
2. **写**：以最小改动实现完整路径。跨层改动保持事实源唯一，避免只改前端展示或只改数据库字段的半完成状态。
3. **验**：立即运行与本次改动匹配的最小检查；失败先阅读错误，修复根因后重跑同一检查。相同阻塞连续三次仍无法解决时，停止试错并清楚说明证据与需要的授权或输入。
4. **记录**：用户可见行为、错误提示、API/架构或运行方式发生变化时，同步所需文档；每项开发任务按 `$campusos-dev-nocommit` 写一份当前阶段进度记录。

不要把多个未验证的功能堆到最后一次构建；不要为了通过测试删除鉴权、审核、配额、清洗、审计、迁移约束或错误信息。

## 3. CampusOS 改动边界

| 变更类型 | 首选位置与必须确认的边界 |
| --- | --- |
| Core/Feature 业务 | `internal/modules/core` 或 `internal/modules/features`；权限事实由服务端和 `author_id`/owner/scope 判定，不能由前端字段替代。 |
| Web/Admin | `web/src`、`admin/src`；API 类型、加载/失败状态和中文可操作错误同步，不能仅隐藏无权按钮。 |
| 存储与附件 | `storage_objects` 保存字节，业务表保存引用；不得暴露宿主路径，访问每次重新验证 owner、文章状态或 Scope。 |
| 数据库/迁移 | 先读 `migrations/README.md` 与数据架构 Skill；除非用户明确授权测试数据重置，否则不改写冻结基线或共享数据。 |
| 外部插件 | 插件源码在 `plugins/`；不能自行创建平台表、直接使用平台数据库、读取任意用户文件或绕过三层授权。 |
| Docker/配置 | 区分热加载、`up` 与 `rebuild`；秘密只在本地环境配置，不能写入源码、文档、测试输出或进度记录。 |

## 4. 验证选择

先运行定向测试，再扩大范围。始终执行 `git diff --check` 与 `python scripts/check-line-endings.py --include-untracked`。

| 受影响范围 | 至少验证 |
| --- | --- |
| Go/API/服务端授权 | 相关包测试；完成后 `GOCACHE=<仓库内缓存> go test ./...`。 |
| Web | `pnpm format:check` 与 `pnpm build`；本机依赖不匹配时，可使用已启动 Docker Web 容器并如实记录。 |
| Admin | 受影响组件检查与 `pnpm build`。 |
| Migration/架构数据 | 隔离 migration drill、`make database-check` 与 `$campusos-data-architecture-sync`。 |
| Docker 工作流 | 对应 Docker Skill 检查、健康端点与受影响服务冒烟。 |
| 文档/Skill | 链接检查、Skill bridge 检查和 `quick_validate.py`。 |

“构建通过”不是浏览器、恢复、安全或性能验收的替代品。仅报告实际执行过的命令、通过的结果和未执行原因。

## 5. 安全与质量自检

交付前至少确认：输入已验证、SQL 参数绑定、用户内容安全渲染、错误不泄露内部路径或秘密、权限在服务端复核、并发/空值/失败路径可预期、配置来自环境或受管配置而非硬编码秘密。

如修改 API、权限、迁移、文件生命周期、插件边界或用户操作路径，检查合同、架构、帮助文档和管理端说明是否仍与代码一致。

## 6. 交付格式

完成时简洁说明：已完成行为、影响范围、进度文档、验证结果、已知限制或需要用户执行的后续动作，并明确是否未提交/未推送。

## 导入资料的使用边界

`agents/` 与嵌套 `skills/` 保留原专家包的通用参考，不是 CampusOS 的自动发现入口：

- 通用 Node/Django/React、MiniMax、`browser-use` CLI 和公网隧道说明不构成默认依赖或执行授权。
- 需要外部媒体生成、安装 CLI、使用真实浏览器 Cookie、创建云账号或暴露本地端口时，必须由用户明确授权，并优先使用当前会话已提供的工具。
- 具体取舍见 [使用说明](../../guides/高级开发工程师专家包使用与接入说明.md)。
