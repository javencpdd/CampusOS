# CampusOS 数据库迁移

> 当前基线：v1.0 clean baseline
> 更新时间：2026-09-01
> 数据库：PostgreSQL 16+
> 兼容边界：不支持从旧 `000001-000049` 链原地升级；现有开发/测试库必须显式重置

## 1. 本次重构结论

原 49 组增量 migration 已压缩为一个可重复零建库的业务 Schema 基线、一个插件三层授权基础和一个不含账号凭据的参考数据 migration。当前结果是：

- 84 张业务表，另有执行器管理的 `schema_migrations`、`schema_migration_locks` 两张系统表；
- 所有时间点字段统一为 `TIMESTAMPTZ`，日期类字段继续使用 `DATE`；
- 删除旧 `permissions(resource, action)` 表，RBAC 只保留 `permission_definitions + role_permissions`；
- `sessions` 删除明文 `refresh_token` 与原始 `ip_address` 列，只保留必填 SHA-256 摘要和 `ip_hash`；
- 数据库触发函数固化 `pg_catalog, public` search path，避免调用会话改变对象解析边界；
- 不再 migration 内置默认管理员、邮箱、密码哈希、默认版块或任何历史测试数据；
- 为 v1 插件生态预建发布者、不可变版本、能力声明、管理员 Grant、用户 Consent、短期 Delegation、密文 Secret 与判定证据；
- migration 文件使用 SHA-256 防篡改，执行使用数据库互斥锁，单个 migration 与版本记录在同一事务提交；
- 当前 97 个外键均有可用的引用端前导索引，Schema 合同会阻止后续新增无索引外键；
- `down` 一次只回滚最新版本；全量清库只能走带环境和数据库名双确认的 `reset`。

## 2. 当前 migration

| 版本 | 文件 | 职责 | 影响 |
| --- | --- | --- | --- |
| `000001` | `000001_v1_schema_baseline.*.sql` | 从零创建当前 76 张业务表、约束、索引、函数与触发器 | 取代全部旧建表/补列/修复链；删除旧权限双轨；时间统一为带时区 |
| `000002` | `000002_v1_plugin_authorization_foundation.*.sql` | 新增 8 张 v1 插件身份与三层授权基础表，并为 `plugins` 增加 `publisher_id` | 为后续授权服务、密钥托管、版本升级和判定审计提供稳定数据边界 |
| `000003` | `000003_v1_reference_data.*.sql` | 写入 4 个系统角色、76 个权限定义、最小角色矩阵、验证码和管理员 MFA 策略 | 不写入用户、账号、邮箱、管理员凭据或业务测试记录 |

已进入本基线的旧业务结构不再保留原 migration 编号。管理端 `/architecture` 按当前三段结构展示，而不是模拟历史 49 段升级过程。

## 3. 数据域清单

| 数据域 | 当前表 |
| --- | --- |
| 身份、认证与授权 | `users`、`accounts`、`sessions`、`roles`、`user_roles`、`permission_definitions`、`role_permissions`、`route_operations`、`route_permission_bindings`、`authorization_audits` |
| 管理员准入与身份安全 | `identity_admin_accounts`、`identity_email_challenges`、`identity_challenge_rate_limits`、`identity_challenge_policies`、`identity_account_recovery_cases`、`identity_legacy_email_placeholders`、`identity_reserved_identifiers`、`identity_mfa_totp_methods`、`identity_mfa_tickets`、`identity_mfa_recovery_codes`、`identity_mfa_policies` |
| 社区与内容治理 | `categories`、`category_thread_type_policies`、`threads`、`posts`、`tags`、`likes`、`notifications`、`content_revisions`、`content_moderation_cases`、`content_moderation_actions`、`richtext_article_contents`、`richtext_article_assets`、`mutual_aid_details`、`secondhand_details` |
| 用户空间、对象与文档 | `user_spaces`、`user_space_contents`、`user_space_style_snapshots`、`user_storage_quotas`、`user_storage_accounts`、`user_storage_reservations`、`storage_objects`、`personal_documents`、`personal_document_versions`、`personal_document_previews` |
| 学期与课表 | `academic_terms`、`user_schedule_terms`、`user_schedule_preferences` |
| 当前插件 Runtime/市场 | `plugins`、`api_keys`、`plugin_permissions`、`plugin_logs`、`plugin_records`、`plugin_file_metadata`、`plugin_user_grants`、`plugin_catalog_entries`、`plugin_install_requests`、`plugin_releases`、`plugin_market_audits` |
| v1 插件授权基础 | `plugin_publishers`、`plugin_versions`、`plugin_capability_declarations`、`plugin_admin_grants`、`plugin_user_consents`、`plugin_delegations`、`plugin_secret_values`、`plugin_authorization_decisions` |
| 可靠性与平台治理 | `platform_outbox`、`platform_outbox_attempts`、`outbox_consumer_receipts`、`platform_command_audits`、`platform_worker_leases`、`platform_operation_runs`、`platform_compatibility_usage`、`platform_retention_runs`、`builtin_feature_states`、`configurations`、`audit_logs` |
| 外部集成与观测 | `webhook_endpoints`、`webhook_deliveries`、`message_bindings`、`message_logs`、`mcp_audit_logs`、`ai_call_logs` |
| migration 元数据 | `schema_migrations`、`schema_migration_locks`，由跨平台执行器创建，不属于业务 migration |

## 4. Windows 与 Linux 使用

Linux、WSL2 或 Git Bash：

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

`up` 只应用未执行版本，并核对所有已执行文件的名称与 SHA-256。`down` 只回滚当前最高版本，避免一次命令意外撤销整个数据库。

### 重置旧开发库

旧 `schema_migrations` 没有 checksum，执行器会明确拒绝继续。确认数据均为可丢弃测试数据后：

Linux / Git Bash：

```bash
CAMPUSOS_ENV=development \
CAMPUSOS_RESET_CONFIRM=campusos \
./scripts/migrate.sh reset
```

Windows PowerShell：

```powershell
$env:CAMPUSOS_ENV = "development"
$env:CAMPUSOS_RESET_CONFIRM = "campusos"
.\scripts\migrate.ps1 reset
Remove-Item Env:CAMPUSOS_ENV, Env:CAMPUSOS_RESET_CONFIRM
```

如果通过 Docker 连接，请同时设置真实容器名，例如 `POSTGRES_CONTAINER=campusos-dev-postgres-1`，并确认 `DB_NAME` 与确认值完全相同。重置会执行 `DROP SCHEMA public CASCADE`，不可恢复；生产环境不允许使用该入口。

重置后管理员由应用启动时的安全 bootstrap 创建，必须通过 `AUTH_BOOTSTRAP_ADMIN_SECRET` 或受控 CLI 提供凭据。migration 不再携带默认密码。

## 5. 后续 migration 规范

从 `000004` 起只追加新 migration，不再修改已经进入共享分支的 `000001-000003`：

1. 文件名使用六位连续编号和清晰业务名，必须同时提供 `.up.sql`、`.down.sql`。
2. 一个 migration 只表达一个可审查的 Schema/参考数据变化；结构和大规模数据回填应拆开。
3. 时间点使用 `TIMESTAMPTZ`，纯日期使用 `DATE`；金额使用最小货币单位整数；密钥、Token、验证码只保存摘要或密文。
4. 状态字段必须有 CHECK；JSONB 必须约束顶层类型；计数、版本和字节数必须约束非负或正数。
5. 业务归属优先使用显式外键，并明确 `CASCADE`、`RESTRICT` 或 `SET NULL`，不能依赖默认删除行为。
6. 唯一约束表达业务不变量；普通索引必须对应已知查询、外键清理、状态扫描或时间排序，禁止重复左前缀索引。
7. 高并发领取使用可证明的 fencing/lease 语义；审计、授权判定和版本记录采用追加写。
8. migration 合并后 SHA-256 成为环境合同；如文件必须修正，应新增前向 migration，不能改写旧文件。
9. 新表必须同步更新 `scripts/schema-contract.sql`、Admin 数据架构页、架构说明和进度证据。
10. 至少执行 `make v1-database-baseline-check`、`make architecture-check` 和受影响 Go/前端测试。

历史 `test-v10...test-v14...migration.sh` 文件仅保留为旧命令入口，统一转发到当前 v1 baseline drill，不再验证已删除的历史 SQL 文件。

## 6. 验证

```bash
POSTGRES_CONTAINER=campusos-dev-postgres-1 make v1-database-baseline-check
POSTGRES_CONTAINER=campusos-dev-postgres-1 DB_NAME=campusos_v1_database_baseline_drill ./scripts/database-check.sh all
python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
```

baseline drill 在固定的隔离测试库中验证：

- 从零建库、无测试用户/账号/管理员凭据；
- 84 张业务表和 2 张 migration 系统表；
- 76 个稳定权限定义与最小角色矩阵；
- 旧 `permissions` 表已移除；
- Session 不存在明文 Refresh Token/原始 IP 列，摘要为必填且全局唯一；
- 97 个外键均有引用端前导索引，索引/约束 hygiene 问题为 0；
- 全库不存在 `timestamp without time zone`；
- checksum 漂移会失败；
- 最新版本回滚、全链回滚、重新 up 和显式 reset 均可重复。
