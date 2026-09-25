# 数据库迁移与 Schema 冗余治理

> 更新时间：2026-09-21 21:22（Asia/Shanghai）
> 适用范围：CampusOS v1.1 clean baseline

## 当前结论

当前迁移链为 `000001_v1_1_schema_baseline`、`000002_v1_1_ui_only_plugin_runtime` 与
`000003_v1_1_trusted_market_sources`。`000001` 把旧开发阶段 `000001`–`000011` 的最终有效结构收敛为一个从零可建的
Schema；后两项是该基线后的前向演进，不是旧库原地升级方案。

当前 Schema 包含 89 张业务表；执行器另创建 `schema_migrations` 和 `schema_migration_locks`，空库迁移完成后为 91 张 public 表。`000003` 的
`plugin_market_sources` 通过 FK 保护市场申请证据，不能被插件私自使用。完整清单和 ER 图见
[migrations README](../../../migrations/README.md)。

## 冗余判断

- 表面上相似的 `storage_objects`、`user_assets`、`personal_documents` 不是重复：前者管理字节与配额，Asset 管理图文附件业务身份，Document 管理私有版本语义。
- `plugin_ui_invocations` 是平台短期访问上下文；外部插件不能为自身 PDF 或其它用途建平台表。
- `plugin_records` 是受控通用记录表；插件 SQLite/config 是插件私有数据，不能替代平台业务事实。
- 旧开发链的 `000002`–`000011` 不再是活跃文件，不能被单独恢复或删除；其最终 DDL 已整合到新基线。当前的 v1.1 `000002` 与 `000003` 是新的、活跃的前向 migration，不应混淆。

## 开发库重建

仅在已确认可丢弃的 development/test 数据库使用 `reset`。它会执行 `DROP SCHEMA public CASCADE`，因此必须同时满足环境、
数据库名和确认值一致的三重条件。Docker 开发环境的完整命令见 [迁移 README](../../../migrations/README.md#3-windows-与-linux-使用)。
生产、staging 或任何有真实数据的库禁止 reset，必须先制订导出与前向迁移方案。

## 后续规则

自此基线被共享后，`000001` 的名称与 checksum 不得修改；当前最高迁移为 `000003`，后续 Schema 从 `000004_<业务名>` 追加 UP/DOWN。每项变更同步更新：

1. `scripts/schema-contract.sql` 和必要的数据审计；
2. Admin `/architecture`、ER PNG/SVG/中文说明；
3. 当前架构、操作文档和进度记录；
4. 隔离库的 up/down/up、checksum、schema/hygiene 验证。

常用检查：

```bash
PSQL_MODE=docker POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
python migrations/tools/generate_er.py --check
python skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```
