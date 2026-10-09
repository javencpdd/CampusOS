# CampusOS 数据库迁移

> 当前结构：v1.1 冻结基线与前向修订，加上 v1.2 授权审计主体域、插件版本身份/发布封存、v5 受管配置与身份委托修订
> 更新时间：2026-10-10（Asia/Shanghai）
> 数据库：PostgreSQL 16+
> 数据边界：本仓库当前开发数据均为可丢弃测试数据；本次已将旧链重构为单一 clean baseline。

## 1. 当前结构

`migrations/` 保留一个不可变 v1.1 clean baseline 和其后的前向修订；v1.2 继续追加，不改写已发布文件：

| 版本 | 文件 | 职责 |
| --- | --- | --- |
| `000001` | `000001_v1_1_schema_baseline.up.sql` / `.down.sql` | 从零创建完整 v1.1 Schema、系统角色与权限参考数据、约束、索引、函数、触发器及插件 Runtime 合同；`down` 仅用于可丢弃 development/test 数据库的全量回滚。 |
| `000002` | `000002_v1_1_ui_only_plugin_runtime.up.sql` / `.down.sql` | 把平台 `plugins.runtime` 合同扩展为 `none`，用于无后端进程、仅提供已校验隔离 UI 的 v4 外部插件。回滚前必须先卸载所有 `runtime=none` 插件。 |
| `000003` | `000003_v1_1_trusted_market_sources.up.sql` / `.down.sql` | 建立管理员维护的 HTTPS + Ed25519 可信市场来源，并为既有申请保存来源、市场插件 ID 和签名目录快照。存在市场来源或来源型申请时拒绝回滚，保留治理审计。 |
| `000004` | `000004_v1_2_authorization_audit_actor.up.sql` / `.down.sql` | 授权审计新增 `actor_kind`，将 `actor_id` 改为最长 128 字符的不透明 ID；主体域与 ID 形状由 CHECK 保护，索引按两者组合。历史 NULL 标为 `legacy_unknown`；存在无法无损表示为 v1.1 数值用户 ID 的记录时拒绝回滚。 |
| `000005` | `000005_v1_2_plugin_version_identity.up.sql` / `.down.sql` | 阻止已有 `plugin_versions` 的版本身份、包摘要、Manifest、API 版本、权限指纹和已有能力声明原地改写；允许生命周期切换、`created_by` 外键置空及旧插件卸载级联删除。回滚只移除更新守卫，保留版本、声明及授权行。 |
| `000006` | `000006_v1_2_plugin_publication_seal.up.sql` / `.down.sql` | 以首次激活时间封存已发布版本，阻止其回到 `staged` 或增删能力声明；版本从 `active` 退役时，同事务永久撤销该版本尚有效的短期委托。对历史已发布但缺失激活时间的行回填；回滚只移除三组守卫，不恢复已撤销委托或清除回填值。 |
| `000007` | `000007_v1_2_plugin_v5_configurations.up.sql` / `.down.sql` | 新增宿主受管 `plugin_configurations`，按不可变插件版本与系统/用户作用域独立保存普通值和 opaque Secret 引用，以 revision 做 CAS；外键、唯一作用域及 JSONB CHECK 拒绝无效结构、明文/重复引用。版本 Manifest 是定义事实源；无数据时才允许回滚。 |
| `000008` | `000008_v1_2_identity_delegations.up.sql` / `.down.sql` | 新增 `identity_delegations`（management/bound/grant 三类有限期委托），分离版块治理执行权与委托权；种子从 active 准入、叶子版块与 moderator 范围转换，并把两个治理动作移出 moderator 角色目录。存在种子后写入的数据时拒绝回滚，回滚先恢复 moderator 角色目录。 |

该基线整合了此前 `000001`–`000011` 的最终有效结构，包含：

- 91 张业务表，以及执行器管理的 `schema_migrations`、`schema_migration_locks` 两张系统表；`000004`–`000006` 只调整现有表，`000007` 新增一张受管配置表，`000008` 新增身份委托表；
- 身份、RBAC、管理员准入、MFA、会话摘要、插件发布者/版本/三层授权与审计；
- 社区、图文文章、`user_assets`、`richtext_article_attachments`、短期 `plugin_ui_invocations` 和资产生命周期审计；
- 个人空间、对象配额、个人文档、文档版本与预览；
- 学期课表、可靠任务、平台治理、外部集成与当前 Plugin Runtime/市场数据模型。

附件字节仍只由 `storage_objects` 和 Object Port 管理；`user_assets` 只管理业务身份；外部插件不得自行创建平台 PostgreSQL 表。

## 2. 此次 clean baseline 的影响

此前 `000001`–`000011` 是开发阶段逐段演进记录。由于项目所有者已明确当前数据均可清空，它们已被合并为 `000001_v1_1_schema_baseline`，因此旧 `000008`–`000011` 不再独立存在。v1.1 的 `000002`、`000003` 和 v1.2 的 `000004`–`000007` 都是基线发布后的前向修订：不能重新改写已应用的迁移文件，否则 checksum 门禁会拒绝启动。

这不是兼容升级：任何记录了旧迁移名称或校验和的数据库执行 `up`/`status` 都会被 checksum 门禁拒绝。必须先确认目标为测试库，再执行 `reset`。不得将本规则用于生产库或包含需保留数据的库；这类环境必须先制定导出、转换和前向迁移方案。

## 3. Windows 与 Linux 使用

Linux、WSL2 或 Git Bash：

```bash
./scripts/migrate.sh status
./scripts/migrate.sh check
./scripts/migrate.sh up
./scripts/migrate.sh down
```

Windows PowerShell：

```powershell
.\scripts\migrate.ps1 status
.\scripts\migrate.ps1 check
.\scripts\migrate.ps1 up
.\scripts\migrate.ps1 down
```

`up` 会核对已执行文件的名称和 SHA-256；`down` 只回滚当前最高版本。`000008 down` 先恢复 moderator 角色的两个治理码目录，存在种子转换之后写入的委托行时拒绝，防止静默抹掉运行期授权；空表回滚移除该表。`000007 down` 在存在任何受管配置行时拒绝，必须先按授权流程导出或删除测试数据；空表回滚移除该表及其校验函数。`000006 down` 仅移除发布/声明集合/退役委托守卫，保留首次激活时间、版本、声明及已撤销委托；降级后不再阻止声明增删或保证新退役自动撤销委托。`000005 down` 仅移除版本和声明的 UPDATE 守卫，保留数据；`000004 down` 在存在非旧版可无损表示的审计主体（含前导零 ID）时拒绝，防止丢失主体域；随后 `000003 down` 在存在可信市场来源或来源型申请时拒绝，`000002 down` 在仍有 UI-only 插件时拒绝。最后回滚 `000001` 才会移除全部应用表与参考数据，但保留两个 migration 元数据表。

### 重置当前 Docker 开发库

确认数据可以删除后，在仓库根目录运行：

```powershell
$env:CAMPUSOS_SKIP_DOTENV = "true"
$env:CAMPUSOS_ENV = "development"
$env:CAMPUSOS_RESET_CONFIRM = "campusos"
$env:PSQL_MODE = "docker"
$env:POSTGRES_CONTAINER = "campusos-dev-postgres-1"
$env:DB_NAME = "campusos"
.\scripts\migrate.ps1 reset
Remove-Item Env:CAMPUSOS_SKIP_DOTENV, Env:CAMPUSOS_ENV, Env:CAMPUSOS_RESET_CONFIRM, Env:PSQL_MODE, Env:POSTGRES_CONTAINER, Env:DB_NAME
```

Linux/Git Bash 等价命令：

```bash
CAMPUSOS_SKIP_DOTENV=true CAMPUSOS_ENV=development CAMPUSOS_RESET_CONFIRM=campusos \
PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 DB_NAME=campusos \
./scripts/migrate.sh reset
```

`reset` 会对精确的 `DB_NAME` 执行 `DROP SCHEMA public CASCADE`，不可恢复；脚本只允许 `development` 或 `test`，且确认值必须与数据库名完全相同。Docker 环境的用户名、密码、主机和端口由 `deploy/docker/.env.dev.local` 提供，切勿把其中的 Secret 写入文档或提交。

## 4. ER 图与结构投影

```bash
python migrations/tools/generate_er.py
python migrations/tools/generate_er.py --check
python skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

ER 工具从当前前向迁移链的 UP 文件生成 [PNG、SVG 与中文实体关系说明](er/current/CampusOS数据库实体关系说明.md)，不连接或修改数据库。管理端 `/architecture` 是源码 Schema 的静态投影，不显示真实数据、文件或 Secret。

## 5. 后续规范

这次整体重构是“可丢弃测试数据”的一次性例外。自本基线被团队共享或进入任何不可丢弃环境后，`000001` 必须视为不可变。当前最高版本为 `000007`，下一项结构变更从 `000008_<业务名>.up.sql` 与对应 `.down.sql` 开始追加，并同时完成：

1. 说明表归属、外键、索引、状态约束、数据修复和回滚损失；
2. 更新 `scripts/schema-contract.sql`、Admin `/architecture`、ER、架构与操作文档；
3. 在隔离库执行空库 `up`、`down`、`up`、checksum 和 schema 合同检查；
4. 不在 migration 内写入默认账号、邮箱、密码哈希或业务测试记录；机密只保存摘要或密文。

## 6. 验证命令

```bash
bash scripts/v12-01a-exit-drill.sh \
  --report docs/项目计划书v1/项目计划v1.2/evidence/v12-01a-exit-linux.json
python3 -m unittest migrations.tools.test_generate_er
python3 migrations/tools/generate_er.py --check
python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

当前 01a 退出脚本自行创建绑定 `127.0.0.1`、数据目录位于 tmpfs 的 PostgreSQL 16 容器，退出即清理；不会重置当前开发数据库。它验证 000001–000006 顺序迁移、发布声明封存、版本切换后的委托撤销、`up/down/up`、checksum、`make database-check`、PostgreSQL 仓储定向测试及最小 API 启动。最终通过状态和原始结果以[退出报告](../docs/项目计划书v1/项目计划v1.2/evidence/v12-01a-exit-linux.json)为准。000004 [授权审计](../docs/项目计划书v1/项目计划v1.2/evidence/v12-01a-audit-actor-linux.json)与 000005 [版本身份](../docs/项目计划书v1/项目计划v1.2/evidence/v12-01a-plugin-version-linux.json)的隔离证据保留为当时源码快照，不替代 000006 后的整阶段复验。
