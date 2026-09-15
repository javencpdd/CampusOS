# `make migrate-up` 教程

> 更新时间：2026-09-16（Asia/Shanghai）
> 当前 Schema：`000001_v1_1_schema_baseline`

## 它做什么

`make migrate-up` 调用 `scripts/migrate.sh up`，按编号执行尚未记录在 `schema_migrations` 中的 UP 文件，并将文件名、SHA-256、
执行耗时和执行者记录在同一事务。当前只有一份 v1.1 baseline：

```text
000001_v1_1_schema_baseline
```

它同时建立完整业务 Schema、角色与权限参考数据、插件三层授权、附件/资产、个人文档和受控 PDF Invocation 上下文。
它不会创建业务用户或管理员密码。

## 使用前检查

```bash
make migrate-status
make migrate-check
```

在 Docker 开发环境应明确使用 Docker PostgreSQL，避免 Git Bash、WSL2 或本机 psql 连到其它库：

```bash
CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker \
POSTGRES_CONTAINER=campusos-dev-postgres-1 DB_NAME=campusos \
make migrate-up
```

PowerShell 可直接设置相同环境变量后运行 `make migrate-up`，或使用：

```powershell
.\scripts\migrate.ps1 status
.\scripts\migrate.ps1 check
.\scripts\migrate.ps1 up
```

## 遇到 checksum drift

旧开发库记录的是已删除的 `000001`–`000011` 链，或记录的是旧 baseline checksum 时，脚本会拒绝继续执行。这是保护，不是缺陷。
仅在确认数据库中的数据均可丢弃后，设置 `CAMPUSOS_ENV=development` 和与 `DB_NAME` 完全相同的
`CAMPUSOS_RESET_CONFIRM`，执行 `migrate reset`。完整跨平台命令见[数据库管理指南](数据库管理指南.md)。

生产、staging 或任何需保留数据的库不能 reset，也不能编辑已经应用的 baseline；应先制定数据导出和从 `000002` 起的前向迁移。

## 后续新 migration

自 clean baseline 共享后，下一项结构变更从：

```text
migrations/000002_descriptive_name.up.sql
migrations/000002_descriptive_name.down.sql
```

开始，并同步更新 `scripts/schema-contract.sql`、ER、Admin `/architecture`、架构文档和隔离数据库回归。执行
`make v1-database-baseline-check` 可验证当前基线的 reset/checksum/up/down/up 合同。
