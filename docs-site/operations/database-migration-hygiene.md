# 数据库迁移与 Schema 治理

CampusOS 当前使用 v1.0 clean baseline，而不是历史 `000001-000049` 增量链。项目所有者确认全部旧数据为测试数据后，
数据库从当前业务模型重新构建；旧开发库不能原地升级，必须经过明确的 development/test reset。

## 当前结构

| migration | 内容 |
| --- | --- |
| `000001_v1_schema_baseline` | 76 张现行业务表、外键、CHECK、索引、函数和触发器 |
| `000002_v1_plugin_authorization_foundation` | 8 张插件身份、版本、三层授权、Delegation、Secret 和判定证据表 |
| `000003_v1_reference_data` | 4 个系统角色、76 个 Permission Code、最小角色矩阵和身份安全策略 |

执行器另建 `schema_migrations`、`schema_migration_locks`。最终为 84 张业务表和 2 张系统表。
migration 不创建用户、管理员、邮箱、默认密码或默认版块。

旧 `permissions(resource, action)` 已删除，RBAC 统一使用
`permission_definitions + role_permissions`。所有时间点字段统一为 `TIMESTAMPTZ`。

## 常用命令

Linux / Git Bash：

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

- `check` 检查版本、up/down 配对和已执行文件 SHA-256；
- `up` 在单 migration 事务中同时提交 SQL 与版本记录；
- `down` 一次只回滚最新版本；
- 数据库锁阻止并发 migration；
- checksum 漂移或旧格式 `schema_migrations` 会直接失败。

## 重置旧开发库

只允许确认可丢弃的 development/test 数据。示例：

```bash
CAMPUSOS_ENV=development \
CAMPUSOS_RESET_CONFIRM=campusos \
./scripts/migrate.sh reset
```

确认值必须与真实 `DB_NAME` 完全一致。reset 会删除整个 `public` schema；production/staging 禁止使用。
Windows 使用相同环境变量执行 `.\scripts\migrate.ps1 reset`。

重置后管理员由安全 bootstrap/CLI 创建，不存在 migration 默认凭据。

## Schema、数据和冗余门禁

```bash
./scripts/database-check.sh audit
./scripts/database-check.sh schema
./scripts/database-check.sh hygiene
POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
make architecture-check
```

- audit 检查重复、孤儿、非法状态和业务不变量；
- schema 检查必需表、列、约束和索引；
- hygiene 拒绝重复索引、相同谓词下被复合 B-tree 覆盖的窄索引和重复约束；
- baseline drill 在隔离库验证零建库、无测试凭据、checksum drift、单步/全链 down、重新 up 和 reset；
- architecture-check 确认 Admin `/architecture` 与 86 张当前表完全对齐。

## 后续 migration

下一编号是 `000004`。进入共享分支后的 `000001-000003` 不得修改；任何修复都必须新增前向 migration。

新增 Schema 时必须同步：

1. up/down SQL；
2. `scripts/schema-contract.sql` 和必要的数据审计；
3. Admin 数据架构视图；
4. migration README、架构/帮助和进度证据；
5. 受影响 Repository/Service、Go、前端和浏览器测试。

金额使用最小货币单位整数，时间点使用 `TIMESTAMPTZ`，JSONB 约束顶层类型，Token/验证码只存摘要，
Secret 只存密文和 Key 版本，外键必须显式选择删除语义。

更完整的模型、需求和每一步影响见仓库
`docs/项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md`。
