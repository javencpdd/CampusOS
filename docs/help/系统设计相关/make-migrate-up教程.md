# `make migrate-up` 当前实现原理

> 适用项目：CampusOS
> 文档状态：当前实现专项教程
> 更新时间：2026-09-01
> 第一次操作数据库前请先读 [数据库管理指南](数据库管理指南.md)。

## 1. 命令入口

Linux、WSL2 或 Git Bash：

```bash
make migrate-up
# 等价于
./scripts/migrate.sh up
```

Windows PowerShell 不依赖 Make：

```powershell
.\scripts\migrate.ps1 up
```

两份执行器提供相同动作：`up`、`down`、`reset`、`status`、`check`。默认优先使用宿主机 `psql`；显式
设置 `PSQL_MODE=docker` 后，通过 `POSTGRES_CONTAINER` 指定的容器运行 PostgreSQL 客户端。

## 2. 当前 migration 链

当前不是历史追加链，而是从空库建立的三组 clean baseline：

```text
000001_v1_schema_baseline
000002_v1_plugin_authorization_foundation
000003_v1_reference_data
```

- `000001` 创建当前业务 Schema。
- `000002` 创建 v1 插件生态与三层授权的数据基础。
- `000003` 写入角色、Permission Code 和认证策略，不创建用户或默认管理员。

已有旧 `000001-000049` 开发库不能直接 `up`。必须先备份需要保留的数据，再按第 6 节显式重置。

## 3. `up` 的真实执行顺序

执行器会：

1. 校验文件名必须使用六位版本号，且每个版本同时存在 `.up.sql` 和 `.down.sql`。
2. 创建或检查 `public.schema_migrations` 与 `public.schema_migration_locks`。
3. 拒绝缺少 checksum 字段的旧 migration 元数据表。
4. 对已执行文件计算 SHA-256；只要数据库记录与仓库文件不一致就终止。
5. 获取数据库互斥锁，防止两个发布任务同时改 Schema。
6. 按版本顺序执行尚未应用的 `.up.sql`。
7. 将 SQL 和 `schema_migrations` 记录放在同一事务中提交。
8. 成功或失败后释放执行锁。

因此 `up` 可以重复执行，但“可重复”只表示已应用版本会被安全跳过，不表示旧 SQL 可以原地修改。

## 4. 迁移记录

`schema_migrations` 保存：

| 字段 | 作用 |
| --- | --- |
| `version` | 六位 migration 版本号 |
| `name` | 文件中的语义名称 |
| `checksum` | `.up.sql` 的 SHA-256 |
| `execution_ms` | 执行耗时证据 |
| `executor` | 执行入口标识 |
| `applied_at` | 带时区应用时间 |

`schema_migration_locks` 只用于执行器互斥，不承载业务数据。`check` 是只读合同校验，不获取或清除该锁。

## 5. 常用配置

Linux/Git Bash 示例：

```bash
DB_HOST=127.0.0.1 \
DB_PORT=5432 \
DB_NAME=campusos \
DB_USER=campusos \
DB_PASSWORD='从本地安全配置读取' \
./scripts/migrate.sh status
```

Docker 开发栈示例：

```bash
PSQL_MODE=docker \
POSTGRES_CONTAINER=campusos-dev-postgres-1 \
DB_NAME=campusos \
DB_USER=campusos \
DB_PASSWORD=campusos \
./scripts/migrate.sh check
```

PowerShell 对应设置 `$env:DB_NAME`、`$env:POSTGRES_CONTAINER` 等变量，再调用 `.\scripts\migrate.ps1`。
配置值只应放在被 Git 忽略的本地环境文件或 Secret 管理系统，不要写回本文。

## 6. 不兼容开发库重置

`reset` 会执行 `DROP SCHEMA public CASCADE`，不可由 down migration 恢复。脚本同时要求环境和数据库名确认：

```bash
CAMPUSOS_ENV=development \
CAMPUSOS_RESET_CONFIRM=campusos \
DB_NAME=campusos \
./scripts/migrate.sh reset
```

PowerShell：

```powershell
$env:CAMPUSOS_ENV = 'development'
$env:CAMPUSOS_RESET_CONFIRM = 'campusos'
$env:DB_NAME = 'campusos'
.\scripts\migrate.ps1 reset
```

只有 `development` 或 `test` 被接受，并且确认值必须与 `DB_NAME` 完全一致。共享或生产数据库应走备份、恢复演练
和正式发布流程，不得使用 reset。

## 7. 新增后续 migration

从 `000004` 开始追加成对文件：

```text
migrations/000004_descriptive_name.up.sql
migrations/000004_descriptive_name.down.sql
```

新文件应满足：稳定主键、明确数据所有权、`TIMESTAMPTZ`、可验证约束、以真实查询为依据的索引、可逆 down，
并同步更新 Schema 合同、Admin `/architecture`、系统设计文档和进度证据。已共享的 `000001-000003` 禁止修改。

## 8. 验证

```bash
./scripts/migrate.sh check
POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
POSTGRES_CONTAINER=campusos-dev-postgres-1 ./scripts/database-check.sh all
python skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

`v1-database-baseline-check` 使用独立临时数据库验证空库建立、checksum 漂移拒绝、最新回滚、全量回滚和
up/down/up，不会重置主开发库。
