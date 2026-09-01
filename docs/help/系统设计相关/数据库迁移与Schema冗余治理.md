# 数据库迁移与 Schema 冗余治理

> 当前合同：v1.0 clean baseline
> 更新时间：2026-09-01
> 权威来源：`migrations/`、`scripts/schema-contract.sql`、Admin `/architecture`

## 1. 为什么本次可以压平

项目所有者已明确说明全部现存数据为测试数据，并要求不保留旧版本数据库兼容。因此历史
`000001-000049` 不再是部署合同，已由三段新建库链替代。这个许可只适用于本次 clean baseline 决策；
`000001-000003` 进入共享分支后重新冻结，后续必须从 `000004` 追加。

当前结构和逐步影响见
[v1.0 数据库全面重构方案](../../项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md)。

## 2. 当前治理结构

| 层 | 责任 |
| --- | --- |
| `000001_v1_schema_baseline` | 76 张现行业务表及其关系、约束、索引、函数和触发器 |
| `000002_v1_plugin_authorization_foundation` | 8 张 v1 插件身份/版本/授权/Secret 表 |
| `000003_v1_reference_data` | 无用户凭据的稳定角色、Permission Code 和安全策略 |
| `schema_migrations` | version、name、SHA-256、execution_ms、executor、applied_at |
| `schema_migration_locks` | 跨进程互斥，避免两个 migration 同时改变 Schema |
| `schema-contract.sql` | 必需表、列、约束和索引合同 |
| `database-audit.sql` | 当前数据状态、孤儿、重复和业务不变量 |
| `migration-hygiene.sql` | 重复索引、冗余 B-tree 前缀和重复约束 |

## 3. 已消除的冗余

- 删除旧 `permissions` 表，角色授权只读取 `permission_definitions + role_permissions`。
- 历史补列、约束重命名、修复 seed 和 no-op migration 被吸收到最终 Schema。
- migration 不再保存默认管理员、邮箱、密码哈希、默认版块或真实历史标识。
- 时间点统一为 `TIMESTAMPTZ`，消除部分表有时区、部分表无时区的语义差异。
- 历史 v10-v14 migration drill 的命令入口统一转发到当前 baseline drill，不再读取不存在的旧 SQL。
- Admin 数据架构页只展示三段当前 migration，不模拟已经删除的升级历史。

当前 hygiene 会拒绝：

1. 完全相同的索引；
2. 相同表、字段、类型和定义的重复约束；
3. 被相同谓词、表达式和排序选项的复合 B-tree 严格左前缀覆盖的普通窄索引。

## 4. checksum 与并发

每个 `up.sql` 应用前计算 SHA-256。已执行版本的文件名或 checksum 与数据库记录不一致时，
`up/status/check` 立即失败，不会静默接受被修改的 migration。

执行开始时向 `schema_migration_locks(id=1)` 插入 owner；冲突表示另一个执行器正在工作。正常成功、失败或中断会尝试释放。
只有已经通过进程和数据库活动确认锁确实遗留时，才允许：

```bash
CAMPUSOS_MIGRATION_LOCK_FORCE=true ./scripts/migrate.sh up
```

`check` 是只读校验，不获取也不清理执行锁；只有 `up`/`down` 会处理该锁。不能把强制清锁作为日常命令，
命令结束后应立即清除环境变量。

## 5. reset 安全边界

`reset` 执行 `DROP SCHEMA public CASCADE`，只接受：

- `CAMPUSOS_ENV=development` 或 `test`；
- `CAMPUSOS_RESET_CONFIRM` 与解析后的 `DB_NAME` 完全相同。

旧三列 `schema_migrations` 会被明确识别为不兼容。确认开发数据可删除后应 reset，而不是手工补 checksum 列。

生产/共享数据库不允许使用 reset。未来真实数据升级必须从当前 `000001-000003` 基线向前追加 migration。

## 6. 新 migration 审查清单

1. 六位连续编号，up/down 成对，文件名表达一个业务变化。
2. 先写数据模型和业务不变量，再决定字段、约束、外键、索引与回滚。
3. 时间点用 `TIMESTAMPTZ`；金额用最小单位整数；Secret/Token 只存摘要或密文。
4. JSONB 约束顶层类型；状态、计数、版本、字节数有 CHECK。
5. 外键明确 `CASCADE/RESTRICT/SET NULL`。
6. 索引必须对应唯一性、外键清理、真实列表/Worker 查询；无查询证据不增加。
7. migration 内不植入环境账号或测试业务数据。
8. 同步 schema contract、Admin 架构页、帮助、计划/进度文档。
9. 运行零建库、down/up、checksum drift、audit、hygiene、Go/前端影响测试。
10. 合并后文件不可改写，修复只能新增前向 migration。

## 7. 验证命令

```bash
./scripts/migrate.sh check
POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
POSTGRES_CONTAINER=campusos-dev-postgres-1 ./scripts/database-check.sh all
python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

Windows 使用对应 `.\scripts\migrate.ps1`。baseline drill 使用固定隔离数据库，并验证：

- 84 张业务表、2 张 migration 系统表；
- 无测试用户/账号/管理员凭据；
- 旧 `permissions` 表不存在；
- 全库时间点已统一；
- checksum 漂移失败；
- 单步 down、全链 down、重新 up 和显式 reset 可重复。
