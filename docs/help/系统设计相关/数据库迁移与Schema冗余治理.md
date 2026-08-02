# 数据库迁移与 Schema 冗余治理

> 当前基线：顺序 migration `000001-000042`。本文重点说明 `000041` 的索引治理；`000042` 追加用户空间配额授权，不改变本页的冗余审计结论。

## 1. 结论

对空库完整应用 `000001-000040` 后，PostgreSQL 系统目录审计得到：

- 完全相同的重复索引：0。
- 完全相同的重复约束：0。
- 数据审计：78 项检查、0 条违规。
- 同表、同谓词、同访问方法且被更长 B-tree 严格左前缀覆盖的普通索引：9 个。

因此没有证据支持删除业务表、业务列、约束、种子记录或旧 migration。可确认的冗余仅是九个维护成本重复的
窄索引，现由 `000041_v13_schema_index_hygiene` 前向迁移删除。

## 2. 为什么不修改或合并旧 migration

`000001-000040` 已可能记录在部署实例的 `schema_migrations` 中。旧文件中的建表、补列、数据回填、
约束修复和种子更新共同描述从任意历史版本升级到当前版本的顺序；即使最终 Schema 中某些中间状态已经被
后续迁移取代，也不能据此删除、改名或重写旧文件。

项目继续采用：

1. 历史 migration 不变。
2. 优化通过新编号前向迁移完成。
3. 生产故障优先 forward-fix；down 文件只用于隔离演练。
4. 不把“空库可以直接建成最终结构”误当成“旧实例可以跳过历史数据迁移”。

## 3. 本次删除的九个索引

| 删除的窄索引 | 保留的覆盖索引 | 共同查询前缀 |
| --- | --- | --- |
| `idx_notifications_user_id` | `idx_notifications_user_read` | `user_id` |
| `idx_plugin_permissions_plugin` | `uk_plugin_permissions` | `plugin_name` |
| `idx_plugin_records_search` | `idx_plugin_records_owner_collection_updated` | `plugin_name, owner_type, owner_id` |
| `idx_posts_thread_id` | `idx_posts_thread_floor` | `thread_id` |
| `idx_role_permissions_role` | `uk_role_permissions_active` | `role_id` |
| `idx_sessions_user_id` | `idx_sessions_user_active` | `user_id` |
| `idx_threads_author_id` | `idx_threads_author_visibility_v10` | `author_id` |
| `idx_user_roles_user_id` | `idx_user_roles_scope_lookup` | `user_id` |
| `idx_webhook_deliveries_status` | `idx_webhook_deliveries_status_created` | `status` |

这些索引的较长版本具有相同的表、B-tree 方法和 partial predicate。PostgreSQL 可以用复合索引的左前缀执行
原窄索引查询；保留两份会让 INSERT、UPDATE、DELETE、VACUUM、备份和缓存同时维护重复结构。

`000041` 使用 `DROP INDEX CONCURRENTLY`，必须通过项目迁移器执行，不要手工包在 `BEGIN/COMMIT` 中。

## 4. 看起来重复但当前必须保留的数据

| 现象 | 当前决策 |
| --- | --- |
| `users.email` 与 `accounts(type=email)` 都保存邮箱 | `accounts` 是权威登录事实，`users.email` 是旧 API 的兼容投影；Identity 在同一命令中同步，当前不能直接删列。 |
| `permissions` 与 `permission_definitions/role_permissions` 并存 | 前者是旧资源/动作目录，后者是当前稳定权限代码目录；兼容调用和迁移遥测完成前不能物理删除。 |
| `threads.status` 与 publication/moderation/deletion 三轴并存 | `status` 仍服务旧接口和领域兼容，三轴是当前治理事实；只能由 Community 归一化写入。 |
| `plugins.status` 与 backend/frontend/health 三轴并存 | 单值状态仍承担旧读取兼容，三轴表达当前 Runtime 状态，不能当作重复列直接删除。 |
| 后续 migration 重复出现 `INSERT ... ON CONFLICT` 或 `UPDATE` | 这是版本化种子和历史数据回填，不是重复业务行；唯一约束和审计负责阻止真实重复。 |

删除这些兼容事实需要先完成读写调用清单、遥测退出标准、API 兼容窗口和独立数据迁移，不属于本次索引治理。

## 5. 自动检查

完整数据库检查：

```bash
./scripts/database-check.sh all
```

只检查 Schema 冗余：

```bash
./scripts/database-check.sh hygiene
```

`scripts/migration-hygiene.sql` 会拒绝：

- 定义完全相同的索引；
- 同一表和谓词下，被同键唯一索引或更长 B-tree 严格左前缀覆盖的非唯一索引；
- 定义完全相同的约束。

迁移回归：

```bash
make v13-migration-check
```

其中 `test-v13-schema-index-hygiene-migration.sh` 在独立临时数据库执行 `000001-000040`、`000041`
up/down/up、数据审计、Schema 合同和冗余门禁，不接触现有业务数据库。

## 6. 部署与回滚

升级前照常备份数据库并确认恢复路径：

```bash
make migrate-up
make migrate-status
make database-check
```

索引删除不删除表中数据。虽然使用 concurrent 模式降低写阻塞，仍应观察数据库锁、I/O、复制延迟和迁移日志。
若迁移中断，不要手工修改 `schema_migrations`；先检查无效索引并重试同一前向迁移。生产环境不建议执行
`000041.down.sql`，因为重新建立九个索引只会恢复额外写放大；down 仅用于隔离兼容演练。

## 7. 后续新增索引规则

新增索引必须同时提供：

1. 对应 Repository 查询或明确运维查询。
2. 与现有索引的列顺序、谓词、排序和唯一性比较。
3. 代表性数据上的 `EXPLAIN (ANALYZE, BUFFERS)` 证据。
4. migration up/down/up 和 `database-check.sh hygiene` 结果。
5. 对写放大、构建锁、磁盘与回滚的说明。

低数据量环境的 `pg_stat_user_indexes.idx_scan=0` 不能单独作为删索引依据；必须结合查询合同和覆盖关系判断。
