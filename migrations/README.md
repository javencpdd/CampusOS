# CampusOS 数据库迁移

> 当前基线：v1.1 图文附件、受控 PDF Viewer、个人文档预览、资产生命周期和插件三层授权
> 更新时间：2026-09-21（Asia/Shanghai）
> 数据库：PostgreSQL 16+
> 数据边界：本仓库当前开发数据均为可丢弃测试数据；本次已将旧链重构为单一 clean baseline。

## 1. 当前结构

`migrations/` 保留一个不可变 clean baseline 和其后的前向修订：

| 版本 | 文件 | 职责 |
| --- | --- | --- |
| `000001` | `000001_v1_1_schema_baseline.up.sql` / `.down.sql` | 从零创建完整 v1.1 Schema、系统角色与权限参考数据、约束、索引、函数、触发器及插件 Runtime 合同；`down` 仅用于可丢弃 development/test 数据库的全量回滚。 |
| `000002` | `000002_v1_1_ui_only_plugin_runtime.up.sql` / `.down.sql` | 把平台 `plugins.runtime` 合同扩展为 `none`，用于无后端进程、仅提供已校验隔离 UI 的 v4 外部插件。回滚前必须先卸载所有 `runtime=none` 插件。 |

该基线整合了此前 `000001`–`000011` 的最终有效结构，包含：

- 88 张业务表，以及执行器管理的 `schema_migrations`、`schema_migration_locks` 两张系统表；
- 身份、RBAC、管理员准入、MFA、会话摘要、插件发布者/版本/三层授权与审计；
- 社区、图文文章、`user_assets`、`richtext_article_attachments`、短期 `plugin_ui_invocations` 和资产生命周期审计；
- 个人空间、对象配额、个人文档、文档版本与预览；
- 学期课表、可靠任务、平台治理、外部集成与当前 Plugin Runtime/市场数据模型。

附件字节仍只由 `storage_objects` 和 Object Port 管理；`user_assets` 只管理业务身份；外部插件不得自行创建平台 PostgreSQL 表。

## 2. 此次 clean baseline 的影响

此前 `000001`–`000011` 是开发阶段逐段演进记录。由于项目所有者已明确当前数据均可清空，它们已被合并为 `000001_v1_1_schema_baseline`，因此 `000008`–`000011` 不再独立存在。`000002` 是基线发布后首次前向修订：它不能重新改写已应用的基线文件，否则 checksum 门禁会拒绝启动。

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

`up` 会核对已执行文件的名称和 SHA-256；`down` 只回滚当前最高版本。先回滚 `000002` 时若仍有 UI-only 插件会被明确拒绝；再回滚 `000001` 才会移除全部应用表与参考数据，但保留两个 migration 元数据表。

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

ER 工具从唯一 UP 文件生成 [PNG、SVG 与中文实体关系说明](er/current/CampusOS数据库实体关系说明.md)，不连接或修改数据库。管理端 `/architecture` 是源码 Schema 的静态投影，不显示真实数据、文件或 Secret。

## 5. 后续规范

这次整体重构是“可丢弃测试数据”的一次性例外。自本基线被团队共享或进入任何不可丢弃环境后，`000001` 必须视为不可变。下一项结构变更从 `000002_<业务名>.up.sql` 与对应 `.down.sql` 开始追加，并同时完成：

1. 说明表归属、外键、索引、状态约束、数据修复和回滚损失；
2. 更新 `scripts/schema-contract.sql`、Admin `/architecture`、ER、架构与操作文档；
3. 在隔离库执行空库 `up`、`down`、`up`、checksum 和 schema 合同检查；
4. 不在 migration 内写入默认账号、邮箱、密码哈希或业务测试记录；机密只保存摘要或密文。

## 6. 验证命令

```bash
PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 \
  DB_NAME=campusos_v1_database_baseline_drill ./scripts/database-check.sh all
python migrations/tools/generate_er.py --check
python skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

基线 drill 会验证空库、无测试用户/账号/管理员凭据、78 项稳定权限、90 张 public 表（包含两张 migration 系统表）、
全部外键前导索引、无无时区 timestamp、checksum 漂移拒绝，以及单一基线的 up/down/up 可重复性。
