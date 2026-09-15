# 数据库迁移与 Schema 冗余治理

> 更新时间：2026-09-16（Asia/Shanghai）
> 适用范围：CampusOS v1.1 clean baseline

## 当前结论

当前迁移链只有 `000001_v1_1_schema_baseline`。它把旧开发阶段 `000001`–`000011` 的最终有效结构收敛为一个从零可建的
Schema；这是基于“当前开发数据均可丢弃”的一次性授权完成的，不是旧库原地升级方案。

该基线包含 88 张业务表、稳定角色与 78 项权限参考数据、105 个物理外键及引用端索引。执行器另创建
`schema_migrations` 和 `schema_migration_locks`，因此空库迁移完成后有 90 张 public 表。完整清单和 ER 图见
[migrations README](../../../migrations/README.md)。

## 冗余判断

- 表面上相似的 `storage_objects`、`user_assets`、`personal_documents` 不是重复：前者管理字节与配额，Asset 管理图文附件业务身份，Document 管理私有版本语义。
- `plugin_ui_invocations` 是平台短期访问上下文；外部插件不能为自身 PDF 或其它用途建平台表。
- `plugin_records` 是受控通用记录表；插件 SQLite/config 是插件私有数据，不能替代平台业务事实。
- 旧 `000002`–`000011` 不再是活跃文件，不能被单独恢复或删除；其最终 DDL 已整合到新基线。

## 开发库重建

仅在已确认可丢弃的 development/test 数据库使用 `reset`。它会执行 `DROP SCHEMA public CASCADE`，因此必须同时满足环境、
数据库名和确认值一致的三重条件。Docker 开发环境的完整命令见 [迁移 README](../../../migrations/README.md#3-windows-与-linux-使用)。
生产、staging 或任何有真实数据的库禁止 reset，必须先制订导出与前向迁移方案。

## 后续规则

自此基线被共享后，`000001` 的名称与 checksum 不得修改；后续 Schema 从 `000002_<业务名>` 追加 UP/DOWN。每项变更同步更新：

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
