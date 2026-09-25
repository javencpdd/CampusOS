# 数据库迁移与 Schema 治理

> 更新时间：2026-09-16（Asia/Shanghai）

CampusOS 当前使用单一 `000001_v1_1_schema_baseline`。项目所有者已确认当前开发数据均为测试数据，因此此前开发阶段
`000001`–`000011` 已收敛为这一个可从零创建的 v1.1 基线；旧开发库不是原地升级目标，必须在明确的 development/test
环境重置后重建。

## 当前结构

`000001_v1_1_schema_baseline` 同时定义 88 张业务表、稳定角色/权限参考数据、105 个物理外键及其引用端前导索引，
并覆盖核心业务、插件三层授权、图文附件、资产治理、三种 PDF Invocation 上下文、个人文档、课表、可靠任务和集成。
执行器额外维护 `schema_migrations`、`schema_migration_locks` 两张系统表，因此空库完成迁移后 `public` 共 90 张表。

迁移不写入用户、管理员、邮箱、默认密码、默认版块或业务测试记录。旧 `permissions(resource, action)` 已删除，RBAC 统一使用
`permission_definitions + role_permissions`；所有时间点字段使用 `TIMESTAMPTZ`。

## 使用与 reset

Linux/Git Bash 使用 `./scripts/migrate.sh status|check|up|down`，Windows PowerShell 使用
`.\scripts\migrate.ps1 status|check|up|down`。`check` 验证版本、UP/DOWN 配对和 SHA-256；`up` 在一个事务中提交 SQL 与版本记录；
`down` 一次只回滚最高版本。当前只有一个 baseline，development/test 中一次 `down` 会清除全部应用表和参考数据。

对旧开发库必须执行带环境和数据库名双确认的 reset：

```bash
CAMPUSOS_SKIP_DOTENV=true CAMPUSOS_ENV=development CAMPUSOS_RESET_CONFIRM=campusos \
PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 DB_NAME=campusos \
./scripts/migrate.sh reset
```

reset 会执行 `DROP SCHEMA public CASCADE`，不可恢复；production/staging 或任何需保留数据的数据库禁止使用。此类环境必须采取导出、
转换与前向迁移方案，不能绕过 checksum。

## 门禁与后续变更

```bash
./scripts/database-check.sh all
PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
python migrations/tools/generate_er.py --check
make architecture-check
```

baseline drill 在隔离临时库验证零建库、无测试凭据、checksum 漂移拒绝、单一基线 up/down/up 和结构合同。
从本基线进入共享或不可丢弃环境开始，`000001` 不可再改写；下一项结构变更必须从 `000002_<业务名>` 追加 UP/DOWN，
同步更新 schema contract、ER、Admin `/architecture`、架构/操作文档与进度证据。
