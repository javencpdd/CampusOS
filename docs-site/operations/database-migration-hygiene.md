# 数据库迁移与 Schema 冗余治理

CampusOS 当前按 `000001-000041` 顺序升级 PostgreSQL。历史 migration 是已部署实例的数据升级合同，
不能为了让目录看起来更短而合并、改写或删除。

## 当前审计结果

在独立空库应用 `000001-000040` 后：

- 78 项数据一致性审计全部为 0 违规；
- 没有定义完全相同的重复索引或重复约束；
- 存在九个被同表、同谓词复合 B-tree 严格左前缀覆盖的普通索引。

`000041_v13_schema_index_hygiene` 只删除这九个索引，不删除业务数据、列、约束或历史 migration：

| 删除 | 保留的覆盖索引 |
| --- | --- |
| `idx_notifications_user_id` | `idx_notifications_user_read` |
| `idx_plugin_permissions_plugin` | `uk_plugin_permissions` |
| `idx_plugin_records_search` | `idx_plugin_records_owner_collection_updated` |
| `idx_posts_thread_id` | `idx_posts_thread_floor` |
| `idx_role_permissions_role` | `uk_role_permissions_active` |
| `idx_sessions_user_id` | `idx_sessions_user_active` |
| `idx_threads_author_id` | `idx_threads_author_visibility_v10` |
| `idx_user_roles_user_id` | `idx_user_roles_scope_lookup` |
| `idx_webhook_deliveries_status` | `idx_webhook_deliveries_status_created` |

复合 B-tree 可以使用左侧列执行这些查询。清理后减少写入、VACUUM、备份和缓存需要维护的重复索引结构。

## 执行检查

```bash
./scripts/database-check.sh all
```

只运行索引和约束冗余检查：

```bash
./scripts/database-check.sh hygiene
```

检查器会拒绝完全重复索引、完全重复约束，以及同表同谓词下被同键唯一索引或更长 B-tree 严格左前缀
覆盖的普通索引。
迁移回归使用：

```bash
make v13-migration-check
```

该门禁在独立临时数据库执行 `000041` up/down/up，不修改正在使用的数据库。

## 升级注意事项

```bash
make migrate-up
make migrate-status
make database-check
```

`000041` 使用 `DROP INDEX CONCURRENTLY`；项目迁移器不会在文件外增加事务。不要手工把该迁移包进
`BEGIN/COMMIT`。升级前仍需完成数据库备份和恢复验证，并观察锁、I/O 与复制延迟。

索引删除不会删除表数据。生产环境出现问题时优先 forward-fix，不建议运行 down 重新制造写放大。

## 不应误删的兼容事实

- `accounts(type=email)` 是登录邮箱权威事实，`users.email` 暂为旧 API 兼容投影。
- `permission_definitions/role_permissions` 是当前权限代码目录，旧 `permissions` 仍处于兼容窗口。
- Thread 和 Plugin 的单值状态仍服务旧读取，当前多轴状态才表达完整治理或 Runtime 事实。
- 后续 migration 中的种子 upsert 和历史回填用于升级旧实例，不等同于重复业务行。

这些对象只有在读写调用、兼容遥测、API 窗口和独立迁移全部闭合后才能删除。仓库内更完整的对象级分析见
`docs/help/系统设计相关/数据库迁移与Schema冗余治理.md`。

## 新增索引要求

新增索引应提供对应查询、与现有索引的列/谓词比较、代表性数据上的
`EXPLAIN (ANALYZE, BUFFERS)`、写放大说明和 up/down/up 证据。仅凭开发库
`pg_stat_user_indexes.idx_scan=0` 不能判定索引无用。
