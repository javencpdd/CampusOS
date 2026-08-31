# CampusOS v1 插件生态与三层授权体系优化实施计划书

## 1. 项目定位

### 1.1 项目目标

本阶段不以扩充插件数量、建设公开插件市场或正式产品化为主要目标，而以建立稳定的 CampusOS 插件平台基础为核心。

本阶段重点解决四个问题：

1. 建立统一、可解释、可撤销的插件授权体系；
2. 建立稳定的插件 Host API 与 Capability Contract；
3. 完善插件运行、配置、事件、数据和升级生命周期；
4. 建立能够支撑后续长期扩展的插件开发、测试和治理体系。

最终目标是：

> 插件不能因为 CampusOS 内部代码调整而频繁失效，不能绕过授权访问用户数据，也不能因为插件升级而自动继承新增权限。

---

# 2. 核心授权模型

采用：

```text
Capability Declaration
        +
Admin Grant
        +
User Consent
        +
Runtime Authorization
```

前三层分别解决：

```text
插件声明
→ 我需要什么能力

管理员批准
→ 平台允许我申请什么能力

用户授权
→ 用户允许我访问自己的什么数据
```

最后 Runtime Authorization 负责：

```text
“本次具体请求现在是否允许执行？”
```

实际有效权限定义为：

```text
Effective Permission
=
Declared Capability
∩ Admin Grant
∩ User Consent
∩ Plugin Version
∩ Resource Scope
∩ Plugin Operational State
∩ System Policy
```

任何一项不满足：

```text
DENY
```

---

# 3. 项目基本安全原则

以下原则作为 v1 插件平台不可突破的安全边界。

### S1. 默认拒绝

任何未明确声明、未批准或未授权的 Capability：

```text
Default DENY
```

不得采用“未配置即允许”。

### S2. 用户数据默认 self scope

第三方用户插件默认只能：

```text
访问当前授权用户自己的数据
```

不得默认支持：

```text
all users
```

### S3. 插件不得提交可信 user_id

用户身份必须由 CampusOS Runtime / Gateway 注入。

禁止：

```text
Plugin → user_id="someone_else"
```

直接决定访问目标。

### S4. 不向插件开放数据库

禁止：

- PostgreSQL Connection；
- Raw SQL；
- Repository；
- AppContext；
- Internal Service；
- JWT Secret。

### S5. Host API 不返回内部 Domain Entity

插件只能获取：

```text
Stable DTO
```

不得直接序列化 Core Domain Object。

### S6. 权限撤销必须即时生效

不得依赖：

- Plugin Restart；
- User Login；
- Token Expiration；
- Server Restart。

### S7. 新增权限不得自动继承

插件升级后：

```text
New Capability
```

必须重新经过：

```text
Admin Approval
+
User Consent
```

### S8. 插件停用高于所有授权

即使：

```text
Declaration = true
Admin Grant = true
User Consent = true
```

如果：

```text
Plugin State != enabled
```

仍然必须拒绝调用。

---

# 4. 当前计划审查结论

## 4.1 当前方向正确的部分

三层授权总体合理，应继续保留。

特别正确的是：

```text
管理员 ≠ 用户授权代理人
```

管理员负责：

- 插件准入；
- 权限批准；
- 功能限制；
- 基础配置；
- 安全治理。

用户负责：

- 是否启用插件；
- 是否允许插件访问自己的个人数据；
- 是否允许代表自己执行特定行为。

这是合理的责任分离。

---

# 5. 当前方案存在的主要遗漏

现有设计如果只实现：

```text
声明 → 批准 → 用户授权
```

仍不足以进入长期使用。

必须补充：

1. 权限撤销；
2. 权限即时失效；
3. Required / Optional 权限；
4. 插件升级权限 Diff；
5. 权限范围 Scope；
6. 用户拒绝后的降级；
7. 用户重新授权；
8. 插件暂停；
9. 插件卸载；
10. 插件重新安装；
11. 插件稳定身份；
12. 用户离线后台任务；
13. Event 权限；
14. Secret；
15. 已获取数据的生命周期；
16. Audit；
17. Machine-readable Deny Reason；
18. 权限缓存失效；
19. Emergency Suspend；
20. 插件版本与授权关系。

这些均应纳入正式计划。

---

# 6. 任务优先级总体划分

定义：

### P0

不完成则插件平台安全模型或核心合同不成立。

### P1

不阻塞基本授权模型，但正式支持真实插件前应完成。

### P2

生态增强能力，可以在 v1 基础稳定之后实施。

总体顺序：

```text
P0
Architecture
Capability
Identity
Authorization
Grant / Consent
Host API
Revocation
Upgrade Permission

        ↓

P1
Scope
Background Delegation
Event
Config
Secret
Data Lifecycle
Audit
SDK / Tooling

        ↓

P2
Sandbox Hardening
Remote Marketplace
Advanced Capability Provider
```

---

# 7. P0 任务清单

---

## T0-01 插件平台架构冻结

**优先级：P0 / 第一项**

### 目标

在修改数据库和业务代码前，冻结插件平台核心领域模型。

### 必须明确

```text
Plugin
PluginVersion
Capability
CapabilityDeclaration
AdminGrant
UserConsent
RuntimeContext
AuthorizationDecision
```

以及：

```text
Plugin Operational State
```

### 交付物

1. Plugin Platform Architecture ADR；
2. Capability 模型 ADR；
3. Authorization 模型 ADR；
4. Plugin Identity ADR；
5. 数据关系图；
6. API Contract 草案；
7. 旧模型兼容清单。

### 完成标准

团队能够明确回答：

- 什么是 Capability？
- 什么是 Admin Grant？
- 什么是 User Consent？
- Plugin Version 如何影响授权？
- 插件请求如何确定 User？
- 用户撤销后如何失效？

任何两个核心开发人员的答案不得相互矛盾。

### 负责人

技术负责人 / 架构负责人。

### 依赖

无。

---

# 8. T0-02 Capability Catalog

**优先级：P0**

### 目标

建立整个 CampusOS 插件生态的统一授权语言。

### Capability 基本结构

```text
id
resource
action
scope
risk
consent_required
description
```

建议增加：

```text
data_classification
audit_level
```

例如：

```text
schedule.entry.read

identity.profile.read

identity.email.read

community.thread.read

notification.send

plugin.data.read

plugin.data.write
```

### 交付物

1. Capability 数据模型；
2. Capability Catalog；
3. Capability Naming Convention；
4. Go 类型；
5. TypeScript 类型；
6. JSON Schema；
7. Capability 文档。

### 完成标准

现有全部 Host API Method 均可映射到明确 Capability。

不得存在：

```text
Host API Method
→ 无对应权限
```

### 负责人

架构负责人 + 后端负责人。

### 风险

Capability 粒度：

- 太粗 → 数据泄露；
- 太细 → 难以使用和维护。

### 止损条件

第一阶段 Capability 数量控制在合理规模，不为了覆盖未来场景一次设计数百项能力。

---

# 9. T0-03 Plugin Identity / Version Model

**优先级：P0**

### 目标

确保授权绑定到正确插件身份，而不是单纯依赖 plugin_name。

### 建议模型

```text
plugin_id
publisher_id
version
package_checksum
signature_state
permission_fingerprint
```

### 交付物

- Plugin Identity Model；
- Plugin Version Model；
- Permission Fingerprint 规范；
- 数据表；
- Package Verification 更新。

### 完成标准

系统能够判断：

```text
同名但不同 Publisher
≠
同一个插件
```

并能够比较：

```text
v1 permission declaration
vs
v2 permission declaration
```

### 排序理由

后续：

```text
Grant
Consent
Upgrade
Reinstall
```

全部依赖稳定 Plugin Identity。

---

# 10. T0-04 Authorization Decision Engine

**优先级：P0**

### 目标

建立唯一插件授权决策入口。

禁止各 Host API 自己实现授权逻辑。

建议：

```text
AuthorizationService.Authorize(...)
```

输入：

```text
plugin_id
plugin_version
user_id
capability
resource_scope
runtime_context
```

输出：

```text
ALLOW
```

或者：

```text
DENY + ReasonCode
```

### ReasonCode

至少支持：

```text
PLUGIN_DISABLED
PLUGIN_SUSPENDED
CAPABILITY_NOT_DECLARED
ADMIN_GRANT_REQUIRED
ADMIN_GRANT_REVOKED
USER_CONSENT_REQUIRED
USER_CONSENT_DENIED
USER_CONSENT_REVOKED
SCOPE_DENIED
PERMISSION_CHANGED
VERSION_NOT_APPROVED
RESOURCE_NOT_OWNED
```

### 交付物

- AuthorizationService；
- Decision Model；
- Reason Codes；
- 单元测试；
- 性能测试；
- Cache Invalidation 方案。

### 完成标准

任何插件敏感 Host API 调用：

```text
100%
```

经过 AuthorizationService。

不得存在绕过路径。

### 负责人

插件平台后端负责人。

---

# 11. T0-05 Admin Grant

**优先级：P0**

### 目标

把：

```text
插件声明权限
```

和：

```text
管理员允许权限
```

彻底拆开。

### 数据模型

建议：

```text
plugin_capability_grants

plugin_id
capability_id
status
constraints
approved_by
approved_at
revoked_at
```

### 管理员支持

- approve；
- deny；
- revoke；
- suspend；
- 查看风险；
- 权限 Diff。

### 交付物

- Admin Grant Service；
- DB；
- Admin API；
- Admin UI；
- Audit。

### 完成标准

插件即使 Manifest 声明某权限：

```text
管理员未批准
→ Host API 必须拒绝
```

---

# 12. T0-06 User Consent

**优先级：P0**

### 目标

正式实现用户级 Capability Consent。

### 模型

```text
plugin_user_consents

plugin_id
user_id
capability_id
status
permission_fingerprint
granted_at
revoked_at
updated_at
```

状态建议：

```text
pending
granted
denied
revoked
expired
```

### 交付物

- Consent Service；
- User API；
- User UI；
- Grant/Revoke 流程；
- Consent Audit。

### 完成标准

同一插件：

```text
User A granted
User B denied
```

调用结果必须不同。

管理员批准不能替代 User Consent。

---

# 13. T0-07 Host API Capability 化

**优先级：P0**

### 目标

解除 Host API 与内部 Service/Domain Entity 的直接绑定。

### 首轮重点

优先处理高风险接口。

例如把：

```text
GetUser()
```

拆成：

```text
GetPublicProfile()
```

与：

```text
GetUserContact()
```

分别对应：

```text
identity.profile.read

identity.email.read
```

### 原则

Host API：

```text
Feature Adapter
    ↓
Stable Plugin DTO
```

而不是：

```text
Domain Entity
    ↓
JSON
```

### 交付物

- Host API vNext Contract；
- Stable DTO；
- Capability Mapping；
- Compatibility Adapter；
- SDK 更新。

### 完成标准

插件不能通过：

```text
低风险 Capability
```

间接获得：

```text
更高敏感数据
```

---

# 14. T0-08 Revocation & Emergency Stop

**优先级：P0**

### 目标

完成授权生命周期闭环。

必须支持：

```text
User Revoke
Admin Revoke
Plugin Disable
Plugin Suspend
Plugin Uninstall
Emergency Quarantine
```

### 完成标准

发生 Revocation 后：

```text
下一次 Host API 请求
```

立即失败。

后台任务也必须失败。

### 安全要求

不得允许：

```text
旧授权缓存继续使用数分钟
```

高风险 Capability 必须立即失效。

### 负责人

授权后端负责人。

---

# 15. T0-09 Plugin Permission Upgrade Diff

**优先级：P0**

### 目标

解决插件版本更新后权限偷偷扩张的问题。

### Permission Diff

支持：

```text
Added
Removed
Expanded
Reduced
PurposeChanged
Unchanged
```

### 规则

#### Added

```text
Admin reapproval
+
User reconsent
```

#### Expanded

同 Added。

#### Removed

自动失效。

#### Reduced

通常无需重新授权。

#### Purpose Changed

按 Risk 判断是否重新授权。

### 交付物

- Permission Diff Engine；
- Permission Fingerprint；
- Upgrade Precheck；
- Admin UI；
- User Reconsent UI。

### 完成标准

任何新增 Capability：

```text
不得自动继承旧授权
```

---

# 16. P1 任务

---

## T1-01 Required / Optional Capability

### 目标

允许用户拒绝非核心权限后继续使用插件。

例如：

```text
required:
schedule.entry.read

optional:
notification.send
```

### 验收

拒绝 Optional：

```text
Plugin remains usable
```

拒绝 Required：

```text
Plugin cannot enter ready state
```

---

# 17. T1-02 Scope Model

### 第一阶段只建议支持

```text
self
system
```

以：

```text
self
```

为用户插件默认值。

### 暂不实现

```text
group
organization
arbitrary-user
```

除非出现真实需求。

### 原因

Scope 是权限复杂度迅速增长的主要来源之一。

---

# 18. T1-03 Delegated Background Execution

### 目标

支持：

- 课表提醒；
- 邮件监听；
- 数据同步；
- 定时任务。

用户离线时：

```text
Plugin
→ on behalf of User
```

但不提供永久 User Token。

每次运行重新验证：

```text
Plugin Enabled
Admin Grant
User Consent
Capability
```

### 验收

用户撤销权限后：

```text
下一次 Scheduled Job
→ DENY
```

---

# 19. T1-04 Plugin Configuration Framework

### 目标

完善当前 ConfigSchema。

支持：

```text
string
text
number
boolean
select
multiselect
email
url
duration
cron
secret
```

以及：

```text
required
default
min/max
regex
help
section
visibility condition
```

### UI

字段较少：

```text
Dialog
```

字段较多：

```text
Settings Page
```

CampusOS 控制 UI。

插件声明配置内容。

---

# 20. T1-05 Secret Store

### 目标

解决真实插件的：

```text
API Key
SMTP Password
OAuth Secret
Webhook Secret
```

### 安全原则

普通 Config API 不得返回 Secret 明文。

UI 只能看到：

```text
configured = true
```

### 交付物

- SecretStore Interface；
- Local encrypted provider；
- Rotation；
- Delete；
- Audit。

---

# 21. T1-06 Event Authorization

### 目标

Events 不得成为权限绕过路径。

例如插件订阅：

```text
schedule.entry.updated
```

仍然必须具备：

```text
schedule.event.subscribe
```

同时 Event Payload 应根据权限裁剪。

### 验收

没有：

```text
identity.email.read
```

的插件不能因为订阅 Event 获得 email。

---

# 22. T1-07 Event Contract v1

### 定义稳定 Event Envelope

```text
event_id
event_type
event_version
source
subject
actor
occurred_at
trace_id
data
```

### 分离

```text
Guard Event
```

与：

```text
Domain Event
```

#### Guard

同步、短超时、可拒绝。

#### Domain

异步、观察型、不能阻断主业务。

---

# 23. T1-08 Managed Data Contract

### 继续支持

```text
CRUD
Filter
Sort
Pagination
Search
Version
Quota
Schema Validation
```

### 明确禁止

```text
Raw SQL
JOIN
Foreign Key
Cross Plugin Query
Custom Index
Stored Procedure
```

### 原因

Managed Data 必须保持可治理和可迁移。

---

# 24. T1-09 Data Retention

### 目标

定义权限撤销和插件卸载后的数据生命周期。

区分：

```text
停止未来访问
```

与：

```text
删除历史数据
```

### 支持策略

```text
delete immediately
delete on uninstall
retain N days
user-managed deletion
```

---

# 25. T1-10 Audit & Diagnostics

### 授权审计

记录：

```text
plugin_id
plugin_version
user_id
capability
decision
reason
trace_id
timestamp
```

### Runtime Diagnostics

记录：

```text
runtime_state
started_at
restart_count
last_health
last_error
permission_denied_count
host_api_latency
event_failure_count
```

### 目的

系统必须能够回答：

> 为什么这一次调用被允许或拒绝？

---

# 26. T1-11 Process Runtime v1

建议将历史：

```text
runtime: grpc
```

迁移为：

```text
runtime: process
```

并定义正式：

```text
campusos.process/v1
```

### 协议包含

```text
Handshake
Ready
Health
Request
Response
Event
Shutdown
```

旧：

```text
grpc
```

作为兼容 Alias。

---

# 27. T1-12 SDK / CLI / Harness

### CLI

完善：

```text
plugin build
plugin test
plugin verify
plugin doctor
plugin dev --watch
plugin conformance
```

### SDK

官方：

```text
Go
TypeScript
```

### Test Harness

模拟：

```text
Allow
Deny
Revocation
Timeout
Managed Data
Config
Event
Background User Context
```

---

# 28. T1-13 Plugin Conformance Suite

这是插件生态非常重要的一项。

必须自动验证：

```text
Manifest
Capability
Authorization
Runtime
Host API
Config
Event
Data
Upgrade
Package
Lifecycle
Security
```

任何 Runtime、SDK、Reference Plugin：

```text
必须通过同一套 Conformance
```

---

# 29. P2 任务

以下任务建议暂缓，不进入当前关键路径：

### T2-01 完整 Process Sandbox

未来完善：

```text
CPU Limit
Memory Limit
Network Policy
Filesystem Policy
Process Tree Limit
```

当前先完成协议与 Host 权限隔离。

---

### T2-02 Remote Marketplace

暂不建设：

```text
Developer Account
Rating
Review
Payment
Remote Store
```

原因：

基础 Contract 尚未完全稳定。

---

### T2-03 Plugin-to-Plugin Capability Provider

当前插件之间不允许直接 RPC。

未来如果出现真实需求：

```text
Plugin A
→ Host Capability Registry
→ Plugin B
```

而不是：

```text
Plugin A
→ Plugin B
```

直接调用。

---

# 30. 数据库重构计划

数据库属于高风险任务。

不得在架构冻结前执行 Reset。

## Phase DB-0

先完成：

```text
Capability Model
Plugin Identity
Grant
Consent
Authorization
```

设计。

## Phase DB-1

建立新 Platform Schema。

建议：

```text
capabilities

plugins
plugin_versions

plugin_capability_declarations
plugin_capability_grants
plugin_user_consents

plugin_configs
plugin_secrets

plugin_records
plugin_files

plugin_audits
```

## Phase DB-2

Repository + Service 改造。

## Phase DB-3

Integration Test。

## Phase DB-4

最后执行开发数据库 Reset。

---

# 31. 数据库安全边界

即使允许删除开发数据库，也必须满足以下条件才能执行。

### Reset 前置条件

- 新 Schema Review 通过；
- 所有 Repository 已完成；
- Seed 数据可自动生成；
- CI 可以从空库启动；
- 当前 Schema 已导出备份；
- migration rollback branch 已保留；
- Feature smoke tests 通过。

### 止损条件

发现以下任一情况：

```text
核心用户认证无法通过
Feature 大面积需要重写
权限模型出现循环依赖
新 Schema 仍持续频繁修改
```

立即暂停 Reset。

回退到旧数据库继续开发。

---

# 32. 风险登记表

| 风险 | 级别 | 影响 | 缓解措施 |
|---|---|---|---|
| Capability 设计过度复杂 | 高 | 所有后续 API 复杂 | 第一版限制范围 |
| Host API 大改 | 高 | 旧插件失效 | Compatibility Adapter |
| DB Reset | 高 | 开发数据丢失 | Snapshot + Seed + Branch |
| Auth Cache | 高 | 撤销不即时 | Event-driven invalidation |
| Plugin Upgrade | 高 | 新权限绕过 | Permission Diff |
| User Context 伪造 | 极高 | 越权读取其他用户 | Host injection only |
| Event Payload 泄漏 | 高 | 绕过 Host API | Event Capability + Redaction |
| Secret 泄漏 | 极高 | 外部账户被接管 | Separate SecretStore |
| Process Plugin | 高 | 主机资源风险 | Host boundary + future sandbox |
| Manifest 膨胀 | 中 | 协议难维护 | Manifest only declares contracts |

---

# 33. 关键止损规则

以下行为一旦在实现中出现，必须暂停相关 PR：

### STOP-01

第三方插件需要直接访问：

```text
Repository / Database
```

### STOP-02

Host API 需要暴露：

```text
Internal Domain Entity
```

### STOP-03

用户身份由插件请求参数决定。

### STOP-04

权限新增可以继承旧 Consent。

### STOP-05

用户撤销后后台任务仍然成功。

### STOP-06

某个 Event 能绕过正常 Capability 获取敏感数据。

### STOP-07

为了支持一个插件不断向 Core 暴露大量内部 Service。

出现这些现象说明架构边界正在失效。

---

# 34. 时间安排建议

建议按 **10 周开发周期**规划。

如果单人或双人开发，可将周期放宽至 12～16 周，但顺序不要改变。

---

# 35. Phase 0：架构冻结

## Week 1

完成：

```text
T0-01 Architecture Freeze
T0-02 Capability Draft
T0-03 Plugin Identity Draft
```

### Milestone M0

输出：

```text
Architecture ADR Approved
Capability Catalog v0
ER Draft
Host API Draft
```

### 禁止

这一阶段：

```text
不得进行大规模 DB Reset
不得同时重写 Runtime
```

---

# 36. Phase 1：授权核心

## Week 2～3

完成：

```text
Capability Catalog
Plugin Identity
Authorization Engine
Admin Grant
User Consent
```

### Milestone M1

课表插件场景：

```text
schedule.entry.read

Declaration ✓
Admin Grant ✓
User Consent ✓
→ ALLOW

任一失败
→ DENY
```

全部自动测试通过。

---

# 37. Phase 2：Host API 与撤销

## Week 4～5

完成：

```text
Host API Capability Migration
Runtime Context
Scope self
Revocation
Emergency Suspend
```

### Milestone M2

必须通过：

```text
User A cannot access User B data
```

以及：

```text
Revoke
→ next request DENIED
```

---

# 38. Phase 3：升级与生命周期

## Week 6

完成：

```text
Permission Fingerprint
Permission Diff
Upgrade Precheck
Required / Optional
Uninstall / Reinstall Rules
```

### Milestone M3

测试：

```text
Plugin v1
schedule.read

→ Plugin v2
schedule.read
identity.email.read
```

必须触发：

```text
Admin Reapproval
+
User Reconsent
```

---

# 39. Phase 4：真实插件支撑能力

## Week 7～8

完成：

```text
Config Framework
Secret Store
Background Delegation
Event Authorization
Event Contract
Managed Data
Data Retention
```

### Milestone M4

能够开发一个真实：

```text
schedule-reminder
```

或者：

```text
mail-watcher
```

而不需要访问任何 internal package。

---

# 40. Phase 5：开发生态

## Week 9

完成：

```text
SDK
CLI
Dev Watch
Doctor
Harness
Conformance
Diagnostics
```

### Milestone M5

开发者能够：

```text
plugin init
↓
plugin dev
↓
plugin test
↓
plugin verify
↓
plugin package
```

完成完整流程。

---

# 41. Phase 6：集成与数据库基线

## Week 10

完成：

```text
Platform Schema Final
Development DB Reset
Seed
Regression
Reference Plugins
Documentation
Architecture Review
```

### Milestone M6

形成：

```text
CampusOS Plugin Platform v1 Baseline
```

---

# 42. 建议资源配置

最小可执行团队：

| 角色 | 投入 | 核心职责 |
|---|---:|---|
| 技术负责人/架构 | 0.5～1 | Contract、架构、Review |
| 后端核心开发 | 1 | Authorization、DB、Host API |
| 插件 Runtime 开发 | 1 | Runtime、Event、SDK、CLI |
| 前端开发 | 0.5 | Admin Grant、Consent、Config UI |
| 测试/质量 | 0.5 | Conformance、Regression、安全测试 |
| 项目管理 | 0.2～0.5 | Milestone、依赖、风险管理 |

如果只有 1～2 名核心开发：

优先保障：

```text
Architecture
Authorization
Host API
Revocation
Upgrade
```

UI、CLI、Diagnostics 可以后移。

---

# 43. 任务关键依赖图

关键路径：

```text
Architecture Freeze
       ↓
Capability Catalog
       ↓
Plugin Identity
       ↓
Authorization Engine
       ↓
Admin Grant + User Consent
       ↓
Host API Capability
       ↓
Revocation
       ↓
Upgrade Permission Diff
       ↓
Runtime / Event / Config / Data
       ↓
SDK + Conformance
       ↓
DB v1 Baseline
```

其中最重要的是：

```text
Capability
→ Authorization
→ Host API
```

任何情况下都不建议改变这条顺序。

---

# 44. 不进入本阶段关键路径的事项

明确暂缓：

```text
Remote Marketplace
Plugin Payment
Rating / Review
复杂前端 Plugin Workspace
任意第三方 Vue/React 注入
Plugin-to-Plugin RPC
任意 Plugin DB Schema
完整容器化 Sandbox
Dify Integration
Blockchain
Microservice Split
```

除非出现新的强需求，否则不要占用 v1 核心开发资源。

---

# 45. Reference Plugin 验收方案

建议维护三个参考插件。

## RP-01 schedule-reminder

验证：

```text
User Consent
schedule.entry.read
Background Delegation
notification.send
Config
Revocation
```

这是授权体系核心验收插件。

---

## RP-02 mail-watcher

验证：

```text
Process Runtime
Secret
Config
Cron/Interval
External Network
Background Job
```

---

## RP-03 content-enhancer

验证：

```text
Community Host API
Event
Capability
User Context
Permission Denial
```

三个插件比大量 Hello World 更有验证价值。

---

# 46. Definition of Done

CampusOS v1 插件生态必须满足以下标准才可视为阶段完成。

## Authorization

```text
Declaration
Admin Grant
User Consent
Runtime Check
```

全部有效。

## Isolation

第三方插件：

```text
不能访问 DB
不能访问 Internal Service
不能伪造 User Context
```

## Revocation

任何权限：

```text
可撤销
可审计
即时生效
```

## Upgrade

新增权限：

```text
不会自动继承旧授权
```

## API

插件只使用：

```text
Stable Host API
```

## Development

开发者无需阅读：

```text
internal/
```

即可开发插件。

## Diagnostics

管理员能够解释：

```text
插件为什么无法运行
权限为什么被拒绝
事件为什么未送达
```

---

# 47. 项目管理检查点

每个 Milestone 都进行一次：

```text
Architecture Review
Security Review
Regression Review
Scope Review
```

重点检查：

### 是否发生范围漂移？

例如突然要求：

```text
完整插件 Marketplace
复杂 Workspace
Plugin RPC
```

应放入 Backlog，不进入当前 Sprint。

### 是否破坏边界？

发现：

```text
直接访问内部 Service
```

立即退回。

### 是否为了兼容历史代码牺牲新架构？

由于当前仍处开发阶段：

> 宁可保留有限 Compatibility Adapter，也不要把明显错误的历史模型继续固化成 v1 Contract。

---

# 48. 最终优先级结论

## 第一梯队：必须立即启动

```text
P0

Architecture Freeze
Capability Catalog
Plugin Identity
Authorization Engine
Admin Grant
User Consent
Host API Capability
Revocation
Upgrade Permission Diff
```

理由：

> 它们共同组成插件安全与稳定性的最小闭环。

---

## 第二梯队：核心模型稳定后立即推进

```text
P1

Required / Optional
Scope
Background Delegation
Config
Secret
Event Contract
Event Authorization
Managed Data
Retention
Audit
Process Runtime
SDK
CLI
Conformance
Diagnostics
```

理由：

> 这些能力决定插件是否真正可用、可维护、可调试，但依赖 P0 Contract。

---

## 第三梯队：延后

```text
P2

Process Sandbox Hardening
Remote Marketplace
Plugin Provider Registry
Advanced Cross-plugin Composition
```

理由：

> 当前投入收益低，而且过早建设会放大尚未稳定的接口。

---

# 49. 最终项目经理建议

v1 最容易犯的错误不是“做得太少”，而是同时启动：

```text
权限重构
数据库重构
Runtime 重构
Host API 重构
插件市场
复杂 UI
SDK
```

最后每一层都处于变化状态。

本计划建议采用：

```text
Contract First
        ↓
Authorization First
        ↓
Security Closure
        ↓
Platform Capability
        ↓
Developer Experience
        ↓
Ecosystem Expansion
```

执行原则可以概括成一句话：

> **先让插件“只能正确地做有限的事情”，再逐步让插件“能够做更多事情”。**

相比先增加大量 API、Runtime 或 Marketplace 功能，这条路径更适合作为 CampusOS 从 v0.x 插件管理框架进入 v1 插件平台阶段的核心路线。