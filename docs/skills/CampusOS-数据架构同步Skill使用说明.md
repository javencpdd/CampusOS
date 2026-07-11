# CampusOS 数据架构同步 Skill 使用说明

`campusos-data-architecture-sync` 用于维护管理端 `/architecture` 数据架构页。它解决的是“仓库 schema 与静态架构图是否同步”的问题，不会连接或暴露运行中的数据库记录。

## 什么时候使用

遇到以下改动时使用：

- 新增、删除或调整 `migrations/*.up.sql` 中的数据表和关键字段。
- 修改 `scripts/migrate.sh`、`scripts/migrate.ps1` 中的系统表或迁移机制。
- 修改 `data/`、`.campusos/logs/`、个人空间或插件数据目录的职责。
- 修改 `admin/src/views/SystemArchitectureView.vue`，或审核数据架构图是否遗漏。

## 基本命令

在仓库根目录执行：

```bash
python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

检查器会比较：

| 来源 | 检查内容 |
| --- | --- |
| `migrations/*.up.sql` | 应用 PostgreSQL 表和显式 foreign key。 |
| `scripts/migrate.sh`、`scripts/migrate.ps1` | 系统管理的 `schema_migrations` 表。 |
| `SystemArchitectureView.vue` | `databaseTables` 表项和 `relations` 的 source/target。 |

以下情况会返回非零：迁移表没有出现在图谱、图谱包含已不存在的表、关系连线指向未知表。没有连线的表只会提示复核，因为有些表本来就是独立日志、配置或多态数据。

## 同步步骤

1. 先运行检查器并阅读 migration、服务模型和数据目录改动。
2. 更新 `databaseTables` 的名称、用途、关键字段和 migration 来源。
3. 仅在关系有真实业务依据时更新 `relations`；没有 foreign key 时继续标注为逻辑关联。
4. 目录职责变化时更新 `storageRows`、架构文档和备份说明。
5. 重新运行检查器、`cd admin && pnpm build`、`GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` 和 `git diff --check`。

## 非实时边界

当前管理端图谱是版本控制中的 schema 快照，不能显示真实行数、索引大小或当前 migration 状态。若需要这些运维信息，应新增受管理员权限保护、字段白名单明确的只读 API；不要把此 Skill 或页面扩展成任意 SQL 或文件浏览器。
