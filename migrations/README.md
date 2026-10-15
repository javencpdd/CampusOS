# CampusOS 数据库迁移

> 当前结构：v1.1 冻结基线与前向修订，加上 v1.2 授权审计、插件版本/配置、身份委托、独立管理身份域与帖子事实版本修订（000001–000023）
> 更新时间：2026-10-14（Asia/Shanghai）
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
| `000009` | `000009_v1_2_admin_identity_domain.up.sql` / `.down.sql` | 新增 `admin_accounts`、`admin_credentials`、`admin_roles`、`admin_role_bindings` 四张管理域独立存储，不外键依赖 users/accounts；只播种 security_admin 参考角色，不播种任何账号/凭据。存在管理员账号时拒绝回滚。 |
| `000010` | `000010_v1_2_admin_sessions.up.sql` / `.down.sql` | 新增 `admin_sessions` 管理域独立会话存储，承载 refresh 摘要、家族轮换、重放检测整族撤销与认证强度事实；存在会话时拒绝回滚。 |
| `000011` | `000011_v1_2_admin_mfa_recovery.up.sql` / `.down.sql` | 新增 `admin_mfa_totp`、`admin_mfa_tickets`、`admin_recovery_tickets` 三张管理域独立安全存储，与用户域 MFA/恢复表完全分离；存在数据时拒绝回滚。 |
| `000012` | `000012_v1_2_remove_admin_role_auto_admission.up.sql` / `.down.sql` | 删除 user_roles 自动同步管理员准入的 SQL 触发器与辅助函数（前向移除，基线文件不可变）；应用层角色授予/撤销不再触碰准入，管理员只能经独立管理域显式开设。回滚按基线原文恢复触发器。 |
| `000013` | `000013_v1_2_admin_role_permissions.up.sql` / `.down.sql` | 新增 `admin_role_permissions`，security_admin 绑定全量非弃用权限目录；独立管理域路由按此表授权。 |
| `000014` | `000014_v1_2_admin_permission_codes.up.sql` / `.down.sql` | 新增 identity.admin_account.create/revoke、identity.admin_session.revoke、identity.admin_mfa.manage 四个管理域权限码并绑定 security_admin。 |
| `000015` | `000015_v1_2_admin_recovery_index.up.sql` / `.down.sql` | 为 admin_recovery_tickets.account_id 外键补前导索引，满足 schema 合同 FK 索引要求；不改写不可变历史。 |
| `000016` | `000016_v1_2_thread_facts_version.up.sql` / `.down.sql` | 新增 `threads.facts_version`，以序列和触发器维护公开详情授权/内容快照版本；owner、版块、三维状态、正文/修订、标签与治理事实变化推进版本，仅计数变化不推进。回滚移除列/触发器并保留内容、修订及分配序列。 |
| `000017` | `000017_v1_2_admin_permission_facts_version.up.sql` / `.down.sql` | `admin_role_permissions.facts_version` 由非循环序列/触发器维护，删除再加入、元数据ABA与回滚分配不复用；down保留所有权限/角色绑定行及分配高水位。 |
| `000018` | `000018_v1_2_business_actor_domains.up.sql` / `.down.sql` | 八张现有表九组明确主体域/opaque ID；保留旧用户列/外键，历史NULL/文本未知域不推断，FK置空保留历史主体；Thread事实版本覆盖新actor。down遇到主体域/历史ID损失拒绝。 |
| `000019` | `000019_v1_2_post_authority_facts.up.sql` / `.down.sql` | posts非复用事实版本/守卫及明确删除actor域/ID；旧历史unknown，typed主体要求已删除，down遇到typed删除历史拒绝并保留原行及序列高水位。 |
| `000020` | `000020_v1_2_article_authority_facts.up.sql` / `.down.sql` | 图文完整正文/JSON/metadata/creator/typed updater/删除时间的非复用事实版本及受管守卫；down保留已有行和actor字段，序列高水位跨回滚/重up保留。 |
| `000021` | `000021_v1_2_user_scoped_execution_facts.up.sql` / `.down.sql` | 四张现有用户授权表的完整私有事实非复用版本；实际作用域行锁、角色/绑定/权限/定义ABA及重插可见，回滚保留原行与共享序列高水位；不扩展冻结委派或Admin域，业务接入另行验收。 |
| `000022` | `000022_v1_2_richtext_asset_binding_facts.up.sql` / `.down.sql` | 资产与文章附件完整事实的非复用版本；完整metadata、生命周期version、关系/排序ABA和重插可见；回滚保留全部原数据、引用与共享序列高水位，业务命令另行验收。 |
| `000023` | `000023_v1_2_personal_space_facts.up.sql` / `.down.sql` | 既有 user_spaces 与 user_space_style_snapshots 完整事实版本；仅空间同步时间/错误及 updated_at 忽略，快照 created_at 为排序事实。down 只移除列/守卫，保留全部原 metadata、typed actor18、行与非循环序列高水位，re-up 不重用旧证明。业务命令需独立验收。 |

该基线整合了此前 `000001`–`000011` 的最终有效结构，包含：

- 100 张业务表，以及执行器管理的 `schema_migrations`、`schema_migration_locks` 两张系统表；`000004`–`000006` 只调整现有表，`000007` 新增一张受管配置表，`000008` 新增身份委托表，`000009` 新增四张管理域身份表，`000010` 新增管理域会话表，`000011` 新增三张管理域安全表，`000012` 移除自动准入触发器，`000013` 新增管理域角色权限绑定表，`000014` 追加四个管理域权限码，`000015` 补管理域恢复表 FK 索引，`000016` 新增帖子事实版本，`000017` 新增管理域执行权限事实版本列、序列和触发器，均不新增表；`000018` 在八张现有表增加九组actor字段，旧外键/表数不变；
- 身份、RBAC、管理员准入、MFA、会话摘要、插件发布者/版本/三层授权与审计；
- 社区、图文文章、`user_assets`、`richtext_article_attachments`、短期 `plugin_ui_invocations` 和资产生命周期审计；
- 个人空间、对象配额、个人文档、文档版本与预览；
- 学期课表、可靠任务、平台治理、外部集成与当前 Plugin Runtime/市场数据模型。

附件字节仍只由 `storage_objects` 和 Object Port 管理；`user_assets` 只管理业务身份；外部插件不得自行创建平台 PostgreSQL 表。

## 2. 此次 clean baseline 的影响

此前 `000001`–`000011` 是开发阶段逐段演进记录。由于项目所有者已明确当前数据均可清空，它们已被合并为 `000001_v1_1_schema_baseline`，因此旧 `000008`–`000011` 不再独立存在。v1.1 的 `000002`、`000003` 和 v1.2 的 `000004`–`000023` 都是基线发布后的前向修订：不能重新改写已应用的迁移文件，否则 checksum 门禁会拒绝启动。

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

`up` 会核对已执行文件的名称和 SHA-256；`down` 每次只回滚当前最高版本一个迁移。`000015 down` 仅移除该索引；`000014/000013 down` 直接移除各自新增对象；`000012 down` 按 v1.1 基线原文重建触发器与辅助函数，随后按原链继续回滚。`000011 down` 在存在管理域 MFA/恢复数据时拒绝；空表回滚移除三张表。`000010 down` 在存在任何管理会话时拒绝；空表回滚移除该表。`000009 down` 在存在任何管理员账号时拒绝，必须先按授权流程导出或删除；空域回滚移除四张管理域表。`000008 down` 先恢复 moderator 角色的两个治理码目录，存在种子转换之后写入的委托行时拒绝，防止静默抹掉运行期授权；空表回滚移除该表。`000007 down` 在存在任何受管配置行时拒绝，必须先按授权流程导出或删除测试数据；空表回滚移除该表及其校验函数。`000006 down` 仅移除发布/声明集合/退役委托守卫，保留首次激活时间、版本、声明及已撤销委托；降级后不再阻止声明增删或保证新退役自动撤销委托。`000005 down` 仅移除版本和声明的 UPDATE 守卫，保留数据；`000004 down` 在存在非旧版可无损表示的审计主体（含前导零 ID）时拒绝，防止丢失主体域；随后 `000003 down` 在存在可信市场来源或来源型申请时拒绝，`000002 down` 在仍有 UI-only 插件时拒绝。最后回滚 `000001` 才会移除全部应用表与参考数据，但保留两个 migration 元数据表。

`000016 down` 前必须停止或回退依赖事实版本的公开详情读取路径；该回滚仅移除事实版本列、约束和触发器，保留正文、状态、内容修订，并将版本分配序列设为 `OWNED BY NONE` 保留高水位。再次 `up` 复用该序列并继续 `nextval`，不重用已分配版本。浏览/回复等计数与普通 `updated_at` 变化不推进事实版本；当前切片只接入公开详情，列表和批量资源策略仍在后续范围。

`000017 down` 前必须停止或回退依赖管理权限事实版本的治理读取。down保留所有角色、角色绑定和权限码及其创建元数据，仅移除事实列/守卫，保留序列高水位；再次up为既有行分配更高的版本。治理业务、actor域存储和权限预览仍需后继接入，不由新增列自动完成。

`000018 down` 前停止依赖typed actor的业务写入；任何Admin主体、与旧列不一致的typed身份（包括User删除后仍保留的历史ID）都使整个down事务拒绝，不静默清除历史。可无损表示的旧数据才允许移除新增字段与legacy同步守卫，并恢复000016事实版本函数。旧User列/外键及原资产actor_type不转换；旧空间文本保留legacy_unknown，不能凭ID前缀或标签推断域。

`000019 down` 前停止依赖Post事实版本和typed删除actor的写入；存在任何typed User/Admin删除历史时原子拒绝，不能抹掉记录来强制降级。无此类历史时仅移除新字段/守卫，保留原回复/metadata及非循环序列高水位；re-up为既有回复分配更高版本。计数和普通updated_at变动不推进版本，实际回复业务接入独立验收。

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

这次整体重构是“可丢弃测试数据”的一次性例外。自本基线被团队共享或进入任何不可丢弃环境后，`000001` 必须视为不可变。当前最高版本为 `000022`，下一项结构变更从 `000023_<业务名>.up.sql` 与对应 `.down.sql` 开始追加，并同时完成：

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
