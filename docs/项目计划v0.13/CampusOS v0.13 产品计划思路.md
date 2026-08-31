# v0.13 立项输入中的历史讨论（非正式计划）

> 文档生命周期：原始对话摘录，主体内容实际讨论 v0.12 可信账号和结构化社区。
> v0.13 当前范围只以 [正式计划](00-v0.13版本计划书.md) 和
> [最终专业审计](02-v0.13最终专业审计与后续路线.md) 为准。

message time: 2026-07-17 00:57:33

请结合当前项目仓库代码，再次检查v0.12计划书，对其进行深度检查与优化

---

# chatgpt response

我会先核对仓库最新主分支、迁移编号、Identity、Category、Thread、RichText 与 Reliability 的当前实现，再逐项验证 v0.12 计划是否与现有边界冲突。

最新主分支只比上次多了一次文档合并，核心代码基线仍接近 `v0.11.0`。我正在重点验证计划中最容易误判的三处：共享邮箱迁移、验证码可靠投递、特殊帖子跨模块事务。

目前已确认一处需要修正：现有 Refresh Token 是纯无状态 JWT，仓库没有会话表或 Token 版本校验，因此计划中的“重置密码后撤销全部 Refresh Token”不能直接列为已有能力，必须先补令牌失效机制。

已按当前 `main` 分支重新核对。以下版本不是对上一版简单扩充，而是根据现有 Identity、Reliability、Category、RichText、模块装配和发布脚本重新收敛后的计划书。

**Document Title: **

# CampusOS v0.12 可信账号、分层社区与结构化帖子实施计划书

> 文档状态：代码审查优化版
> 审查分支：`main`
> 审查基线：`cb128267b89e2024624e8cd14e76b1e9e44c21f5`
> 当前应用版本：`v0.11.0`
> 最后更新：2026-07-17

---

## 一、代码审查结论

当前 `main` 最新提交为 `cb128267...`，该提交只修改了文档思考文件，没有改变核心业务代码；运行时版本仍为 `0.11.0`。因此，v0.12 可以继续以已经完成的 v0.11 Reliability 基线为起点。

上一版 v0.12 的总体方向正确，但结合代码后需要进行八项关键修正：

1. 不再新增一套完整的 `account_email_bindings` 作为第三份邮箱事实；
2. `1904650862@qq.com` 只能作为历史共享占位邮箱，不能成为登录和找回密码凭据；
3. 历史账号不能简单标记为“邮箱已验证”，应使用兼容状态；
4. 邮件投递不能设计为可关闭的普通 Feature；
5. 必须补齐 Session、Refresh Token 轮换和密码重置后的令牌失效；
6. Category 已经拥有 `parent_id` 和 `is_closed`，应增量强化，而不是重新设计一套层级模型；
7. 特殊帖子应使用各子模块的类型化 API，不能让通用帖子接口接收任意 JSON；
8. 在互助和二手模块之前，必须先修复普通帖子与 RichText 的非原子创建问题。

---

# 二、当前代码中必须优先处理的问题

## 2.1 邮箱事实目前已经重复两份

数据库同时保存：

```text
users.email
accounts.identifier
```

两处都存在唯一索引，但唯一性是区分大小写的。`accounts` 已包含 `verified` 字段，因此它更适合作为邮箱登录、验证和找回密码的权威事实。

当前注册流程会：

```text
创建 users
→ 创建 accounts
→ accounts.verified = false
→ 返回注册成功
```

但登录只读取邮箱和密码，并没有检查 `verified`。

因此，v0.12 不应再增加第三套“个人邮箱绑定表”。优化后的所有权应为：

| 数据 | 权威所有者 |
|---|---|
| 个人登录邮箱 | `accounts.identifier` |
| 邮箱验证状态 | `accounts.verification_state` |
| 个人资料中的邮箱展示 | `users.email`，作为兼容投影 |
| 共享临时邮箱 | 独立的历史占位记录 |
| 邮箱验证码 | Identity Challenge |
| 邮件投递 | Integration Email |

---

## 2.2 共享邮箱不能写入现有登录字段

当前 `users.email` 和 `accounts(type, identifier)` 都具有唯一约束，无法把同一个邮箱写入所有账号。即使删除约束，也会导致一个邮箱匹配多个账号，密码重置时无法确定目标用户。

因此，安全实现为新增：

```text
identity_legacy_email_placeholders
```

建议字段：

```text
user_id
email_normalized
source
created_at
deleted_at
```

为现有用户批量写入：

```text
email_normalized = 1904650862@qq.com
source = v0.12_legacy_bootstrap
```

这张表不参与：

```text
登录
注册
邮箱唯一性判断
验证码验证
密码重置
账号所有权证明
安全通知
```

它只用于：

- 标记该账号尚未完成个人邮箱治理；
- 在管理端展示临时联系状态；
- 辅助历史账号迁移；
- 提醒用户绑定个人邮箱。

若该 QQ 邮箱还要作为验证码发件邮箱，应通过独立 SMTP 配置设置：

```text
MAIL_FROM=1904650862@qq.com
MAIL_SMTP_USERNAME=1904650862@qq.com
MAIL_SMTP_AUTH_SECRET=<QQ邮箱SMTP授权码>
```

“邮件发件人”和“用户绑定邮箱”是两个完全不同的概念。

---

## 2.3 历史账号不能伪装成已验证账号

现有普通账号注册时 `accounts.verified=false`，只有默认管理员种子账号被写为 `TRUE`。因此不能在迁移中把所有历史邮箱直接标记为真正验证过。

建议将布尔状态升级为：

```text
verification_state:
- unverified
- legacy_accepted
- verified
```

规则如下：

| 状态 | 可登录 | 可密码重置 | 是否证明拥有邮箱 |
|---|---:|---:|---:|
| `unverified` | 否 | 否 | 否 |
| `legacy_accepted` | 是 | 否 | 否 |
| `verified` | 是 | 是 | 是 |

迁移规则：

```text
默认管理员已验证邮箱 → verified
其他历史邮箱账号     → legacy_accepted
v0.12 新注册账号       → verified
```

这样既不阻断历史用户登录，也不会错误地宣称他们已经验证过邮箱。

---

## 2.4 密码重置目前无法真正撤销 Refresh Token

当前 Access Token 和 Refresh Token 都是无状态 JWT，只验证签名和过期时间；仓库虽然已经有 `sessions` 表，但业务代码没有使用它，也不存在 `/auth/refresh` 路由。

因此，上一版计划中的：

```text
密码重置后撤销全部 Session 和 Refresh Token
```

不能直接实现，必须先补会话体系。

v0.12 应完成：

```text
登录
→ 创建 Session
→ 返回 Access JWT + 不透明 Refresh Token
→ 数据库只保存 Refresh Token 哈希
```

新增接口：

```text
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
POST /api/v1/auth/logout-all
```

建议扩展 `sessions`：

```text
refresh_token_hash
token_family_id
rotated_from_id
revoked_at
revoke_reason
last_used_at
expires_at
```

同时向 `users` 增加：

```text
auth_version
```

Access JWT 增加：

```text
session_id
auth_version
```

密码重置时必须原子完成：

```text
更新密码
→ auth_version + 1
→ 撤销全部 sessions
→ 消耗 Reset Ticket
→ 写 required audit
→ 写 Outbox
→ COMMIT
```

这样旧 Refresh Token 会立即失效，旧 Access Token 也会因为 `auth_version` 不匹配而失效。

---

## 2.5 默认管理员初始化存在安全问题

当前代码将默认管理员邮箱、默认密码直接写在代码中，并在服务启动时把默认密码输出到日志。

v0.12 的账号安全阶段必须同时完成：

1. 删除默认密码日志；
2. 生产环境禁止使用 `Admin@123456`；
3. 管理员初始密码从部署 Secret 注入；
4. 默认管理员首次登录必须修改密码；
5. `admin@campusos.local` 不允许使用邮件找回；
6. 提供本机管理命令作为管理员应急恢复入口。

建议：

```bash
campusosctl identity reset-password --username admin
```

该命令必须：

- 只能在服务器本机或可信运维环境执行；
- 记录 required audit；
- 增加 `auth_version`；
- 撤销全部 Session；
- 不输出明文密码到日志。

---

# 三、优化后的邮箱身份模型

## 3.1 Accounts 继续作为个人邮箱权威事实

建议扩展 `accounts`：

```text
identifier_normalized
verification_state
verified_at
verification_source
password_changed_at
credential_version
```

约束：

```text
一个规范化邮箱只能对应一个有效账号
一个用户最多拥有一个有效 email 类型账号
```

唯一索引建议：

```sql
CREATE UNIQUE INDEX ...
ON accounts(type, lower(btrim(identifier)))
WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX ...
ON accounts(user_id)
WHERE type = 'email' AND deleted_at IS NULL;
```

同时对 `users.email` 增加大小写不敏感的兼容唯一索引。

邮箱规范化只做：

```text
trim
lowercase
```

v0.12 不应实现 QQ、Gmail 的点号合并或 `+alias` 折叠，避免错误地把不同地址视为同一个邮箱。

---

## 3.2 邮箱写操作必须统一进入 Identity 可靠命令

禁止 Handler、Admin 或其他模块直接修改：

```text
users.email
accounts.identifier
accounts.verified
```

所有邮箱变更必须通过：

```text
identity.email.bind
identity.email.replace
identity.email.verify
identity.password.reset
```

个人邮箱绑定成功时，原子更新：

```text
accounts.identifier
accounts.identifier_normalized
accounts.verification_state
accounts.verified
accounts.verified_at
users.email
users.updated_at
required audit
Outbox
```

`users.email` 在 v0.12 中只是兼容投影，后续版本可逐渐从 User 领域实体中移除。

---

# 四、邮箱验证码与票据设计

## 4.1 Challenge 数据模型

新增：

```text
identity_email_challenges
```

字段建议：

```text
id
public_id
purpose
email_normalized
account_id
key_id
nonce
expires_at
attempt_count
max_attempts
verified_at
ticket_digest
ticket_expires_at
consumed_at
invalidated_at
requested_ip_hash
created_at
updated_at
```

`purpose`：

```text
registration
email_binding
password_reset
```

不同 Purpose 的验证码和 Ticket 绝对不能互换。

---

## 4.2 验证码不能只存摘要后再要求邮件 Worker 读取明文

上一版“数据库只保存验证码摘要，Outbox 只保存 challenge_id”的设计存在一个问题：

> 邮件 Worker 无法从摘要恢复验证码。

优化方案是采用可重建验证码：

```text
code = Truncate6(
    HMAC(
        verification_secret[key_id],
        public_id + nonce + purpose
    )
)
```

数据库只保存：

```text
public_id
nonce
purpose
key_id
```

邮件 Worker 和验证码校验服务都通过密钥环重新计算验证码。

优势：

- 数据库和 Outbox 不保存验证码明文；
- 不需要可逆加密验证码；
- 邮件重试可以重建同一个验证码；
- 密钥轮换通过 `key_id` 兼容；
- 数据库泄漏后无法离线计算验证码。

验证码建议：

```text
6 位数字
10 分钟有效
最多尝试 5 次
60 秒内禁止重复请求
新 Challenge 创建后旧 Challenge 失效
```

---

## 4.3 Ticket 使用一次性随机值

验证码校验成功后返回：

```text
verification_ticket
```

服务器只保存 Ticket 的哈希。

Ticket 必须绑定：

```text
challenge_id
purpose
email_normalized
有效期
```

注册、绑定邮箱或重置密码时，使用 `SELECT ... FOR UPDATE` 锁定 Challenge，并检查：

```text
已验证
未过期
未失效
未消费
Ticket 哈希匹配
Purpose 匹配
邮箱匹配
```

成功后设置 `consumed_at`。

---

# 五、邮件投递架构

## 5.1 不复用现有 integration.message

当前 `integration.message` 只实现本地测试 Adapter，并把消息正文和 `raw_payload` 写入 `message_logs`。这不适合保存注册验证码或密码重置内容。

v0.12 应新增：

```text
integration.email
```

但它不是可被管理员关闭的普通功能开关。

架构：

```text
core.identity
    │
    │ Reliability Outbox
    ▼
identity.email.challenge.requested
    │
    ▼
integration.email consumer
    │
    ▼
platform.mail.Sender
    ├── SMTP Provider
    └── Fake Provider
```

依赖关系：

```text
integration.email
→ core.identity
→ core.reliability
```

Identity 不依赖 SMTP。即使 SMTP 暂时不可用，Challenge 和 Outbox 仍然可以提交，邮件由 Worker 重试。

---

## 5.2 邮件事件只保存 Challenge ID

Outbox Payload：

```json
{
  "challenge_id": "..."
}
```

邮件 Consumer 通过 Identity 的内部端口读取：

```text
收件地址
邮件模板
可重建验证码
有效期
```

该端口只能供编译内模块使用，不能暴露给：

```text
External Plugin
MCP
Agent Runner
浏览器
Host API
```

Replay 时，如果 Challenge 已过期、失效或已消费，Consumer 应安全跳过，不再发送旧验证码。

---

# 六、注册流程

## 6.1 推荐流程

```text
填写邮箱
→ 请求注册验证码
→ 输入验证码
→ 获得一次性 Registration Ticket
→ 填写用户名、昵称和密码
→ 提交正式注册
```

API：

```text
POST /api/v1/auth/registration-challenges
POST /api/v1/auth/registration-challenges/:id/verify
POST /api/v1/auth/register
```

正式注册请求：

```json
{
  "username": "jack",
  "nickname": "Jack",
  "password": "********",
  "verification_ticket": "..."
}
```

邮箱不再由客户端在最终注册请求中重复自由指定，而是从 Ticket 对应的 Challenge 中读取，防止更换邮箱绕过验证。

可靠命令：

```text
锁定并检查 Registration Ticket
→ 检查邮箱唯一性
→ 创建 users
→ 创建 verified email account
→ 消耗 Ticket
→ 写 required audit
→ 写 user.created Outbox
→ COMMIT
```

当前 `Register` 已经接入 `reliability.Execute`，v0.12 应扩展已有命令，而不是另建第二套注册入口。

---

## 6.2 前端调整

当前注册页面会直接提交用户名、昵称、邮箱和密码，成功后跳转登录页。

应改成两个步骤：

```text
步骤一：邮箱验证
步骤二：账号资料
```

同时修复现有文案冲突：

当前页面显示“用户名用于登录”，但后端登录实际只按邮箱查询。

建议改为：

```text
用户名用于个人主页地址和公开标识，登录使用邮箱。
```

---

## 6.3 防批量注册

第一阶段使用持久化控制：

| 维度 | 默认策略 |
|---|---:|
| 同邮箱重发间隔 | 60 秒 |
| 同邮箱每日请求 | 5 次 |
| 同 IP 每小时请求 | 10 次 |
| 同 IP 每日成功注册 | 5 个 |
| 验证码有效期 | 10 分钟 |
| 最大错误次数 | 5 次 |
| Registration Ticket | 15 分钟 |

不单独引入复杂限流表，可以先通过 Challenge 表及索引统计时间窗口。

IP 只保存 HMAC 哈希，不保存完整明文。

当异常注册量明显上升后，再增加人机验证；不应把第三方验证码作为 v0.12 的强制依赖。

---

# 七、密码重置和历史账号恢复

## 7.1 普通密码重置

API：

```text
POST /api/v1/auth/password-reset-challenges
POST /api/v1/auth/password-reset-challenges/:id/verify
POST /api/v1/auth/password-reset/confirm
```

请求接口统一返回：

```text
如果该邮箱符合条件，系统将发送验证码。
```

不得暴露：

```text
邮箱是否存在
账号是否被封禁
邮箱是否已验证
```

只有：

```text
verification_state = verified
```

的个人邮箱才能重置密码。

`legacy_accepted` 和共享邮箱均不能重置密码。

---

## 7.2 历史用户绑定个人邮箱

历史用户成功登录后，在账号安全页完成：

```text
输入新邮箱
→ 获取绑定验证码
→ 验证
→ 原子替换个人邮箱
→ verification_state = verified
```

绑定完成后：

- 可使用邮箱找回密码；
- 共享临时邮箱标记为已完成治理；
- 撤销其他 Session，防止账号被占用后偷偷修改邮箱。

---

## 7.3 历史用户忘记密码且没有验证邮箱

不能把验证码发送到共享邮箱，否则共享邮箱持有人可以重置所有历史账号。

安全流程：

```text
用户联系管理员
→ 管理员线下核实校园身份
→ 管理员录入用户提供的新邮箱
→ 系统向新邮箱发送验证邮件
→ 用户完成验证
→ 系统允许设置新密码
```

增加权限：

```text
identity.account.recovery.override
```

该流程必须写：

```text
操作者
目标用户
原因
验证邮箱
时间
IP
required audit
```

管理员不能直接查看或设置用户明文密码。

---

# 八、Category 两级板块优化

## 8.1 不重新发明已有字段

Category 当前已经包含：

```text
parent_id
is_closed
sort_order
```

但 Service 没有检查父节点、层级、循环或关闭状态，Repository 也没有接入 `transaction.ExecutorFor`。

因此建议新增：

```text
node_kind
lifecycle_status
version
color
```

其中：

```text
node_kind:
- group
- board

lifecycle_status:
- active
- archived
```

不再新增 `posting_enabled`，发帖资格直接由以下条件计算：

```text
node_kind == board
AND lifecycle_status == active
AND is_closed == false
```

这样避免 `posting_enabled` 与 `is_closed` 出现互相矛盾。

---

## 8.2 层级规则

```text
根级 group
    └── 二级 board

根级 board
```

约束：

1. 最大两级；
2. group 只能位于根级；
3. group 不能作为帖子目标；
4. board 可以位于根级或 group 下；
5. board 不能继续拥有子节点；
6. 节点不能把自己或自己的子节点设为父节点；
7. archived 节点不能接收新帖子；
8. archived group 下不得新增子板块。

现有所有 Category 回填：

```text
node_kind = board
lifecycle_status = active
version = 1
```

---

## 8.3 必须先可靠化 Category 写命令

新增命令：

```text
community.category.create
community.category.update
community.category.move
community.category.archive
community.category.restore
community.category.bootstrap
```

首先把 `PgCategoryRepository` 全部改为：

```go
transaction.ExecutorFor(ctx, pool)
```

随后使用 Reliability：

```text
锁定当前节点和父节点
→ 检查版本
→ 检查层级
→ 检查权限
→ 写 Category
→ 写 required audit
→ 写 Outbox
→ COMMIT
```

---

## 8.4 单独设计移动接口

现有 `UpdateCategoryRequest.ParentID *string` 无法清晰区分：

```text
未提交 parent_id
显式设置 parent_id = null
```

因此父节点移动不再混在普通 Update 中。

API：

```text
PUT /api/v1/admin/categories/:id/parent
```

请求：

```json
{
  "parent_id": null,
  "version": 3
}
```

---

## 8.5 删除改为归档

当前 Delete 只是设置 `deleted_at`，不会检查已有帖子。由于帖子引用 Category，即使 Category 从列表消失，帖子仍然存在。

v0.12 改成：

```text
GET  /api/v1/admin/categories/:id/archive-impact
POST /api/v1/admin/categories/:id/archive
POST /api/v1/admin/categories/:id/restore
```

影响预览返回：

```text
子板块数量
帖子数量
回复数量
权限授权数量
允许的帖子类型
```

归档后：

- 禁止新帖子；
- 现有帖子和审计继续保留；
- 管理端可查看并恢复；
- 不做物理删除。

---

## 8.6 修复现有 Color 合同错误

Admin 页面始终会把 `color` 放进请求，但后端 Category DTO 没有该字段；严格 JSON 解码器会拒绝未知字段，因此当前创建或更新板块存在真实合同错误。

v0.12 直接把 `color` 正式纳入 Category：

```text
VARCHAR(9)
支持 #RRGGBB
支持 #RRGGBBAA
```

并增加后端校验和合同测试。

---

# 九、结构化帖子核心

## 9.1 Thread Type

新增：

```text
threads.thread_type
```

取值：

```text
discussion
article
mutual_aid
secondhand
```

回填：

```text
存在 richtext_article_contents → article
其他帖子                     → discussion
```

`content_format` 继续表示正文渲染格式，`thread_type` 表示业务类型，二者不能混用。

---

## 9.2 Category 帖子类型策略

新增：

```text
category_thread_type_policies
```

字段：

```text
category_id
thread_type
is_enabled
created_at
updated_at
```

现有板块默认允许：

```text
discussion
article
```

校园互助板块允许：

```text
mutual_aid
```

二手交易板块允许：

```text
secondhand
```

Core Community 在创建帖子前必须检查：

```text
板块存在
板块可发帖
Thread Type 已注册
板块允许该 Thread Type
对应子模块已启用
```

---

## 9.3 不允许通用接口接收任意详情 JSON

不推荐：

```text
POST /threads
{
  "thread_type": "secondhand",
  "detail": {任意 JSON}
}
```

这会削弱当前严格 JSON 合同，也会让 Community Core 承担所有业务字段。

优化后使用类型化接口：

```text
POST /api/v1/mutual-aid/threads
POST /api/v1/secondhand/threads
```

每个模块负责自己的：

```text
DTO
字段验证
状态机
详情表
API
前端表单
管理规则
```

Core Community 只提供编译内事务端口。

---

## 9.4 编译内事务参与者

建议接口：

```go
type StructuredThreadParticipant interface {
    ThreadType() domain.ThreadType
    PersistDetail(ctx context.Context, threadID string) error
    EventPayload(threadID string) any
}
```

Community 提供：

```go
type StructuredThreadCommand interface {
    Create(
        ctx context.Context,
        actor Actor,
        base CreateThreadRequest,
        participant StructuredThreadParticipant,
    ) (*Thread, error)
}
```

事务：

```text
检查 Category 和 Thread Type Policy
→ 创建 Thread
→ 创建 Revision
→ 调用子模块 PersistDetail
→ 写 required audit
→ 写 Outbox
→ COMMIT
```

这个端口只发布给编译内 Built-in Module。External Plugin 不得获取。

---

# 十、先修复普通帖子和 RichText 原子性

当前普通帖子创建流程为：

```text
Create Thread
→ Record Revision
→ EventBus
```

它没有被可靠命令包裹；Revision 失败时，普通 Thread 可能已经写入。

RichText 创建则是：

```text
创建 Thread
→ 创建 Article
→ Article 失败时把 Thread 移入回收站
```

这属于补偿，不是真正原子回滚。更新和发布同样是先写 Thread、再写 Article。

同时，RichText PostgreSQL Store 直接调用 `pool.Exec`，无法参与 TxKernel。

所以结构化帖子实施前必须完成：

1. 普通 `CreateThread` 接入 Reliability；
2. Thread、Revision、Audit、Outbox 同事务；
3. RichText Store 改用 `transaction.ExecutorFor`；
4. RichText 创建、保存、发布、下线改为可靠命令；
5. RichText 作为第一种 `article` Structured Thread 迁入新内核；
6. 通过 RichText 验证扩展事务合同后，再开发互助和二手交易。

这一步是 v0.12 结构化帖子方案的真正前置条件。

---

# 十一、校园互助子模块

模块：

```text
feature.mutual-aid
```

详情表：

```text
mutual_aid_thread_details
```

字段：

```text
thread_id
aid_type
aid_status
deadline
location_scope
contact_mode
version
created_at
updated_at
```

状态：

```text
open
in_progress
resolved
closed
```

允许转换：

```text
open → in_progress
open → resolved
open → closed
in_progress → open
in_progress → resolved
in_progress → closed
resolved → closed
```

限制：

- 联系方式默认脱敏；
- 不保存身份证、银行卡和完整宿舍门牌；
- 不支持付款和担保；
- 治理下架状态优先于互助业务状态；
- Feature 停用后禁止新建和状态修改，但历史数据和普通帖子正文仍可读取。

---

# 十二、二手交易子模块

模块：

```text
feature.secondhand
```

详情表：

```text
secondhand_thread_details
```

字段：

```text
thread_id
price_minor
currency
item_condition
trade_method
trade_status
location_scope
version
created_at
updated_at
```

约束：

```text
price_minor >= 0
currency = CNY
```

状态：

```text
available
reserved
sold
closed
```

平台只提供：

```text
信息发布
状态标记
内容审核
举报处理
```

不提供：

```text
支付
订单
托管
担保
退款
物流
交易仲裁
```

禁售规则由子模块管理，但最终治理状态仍由 Community/Moderation Core 控制。

---

# 十三、模块装配需要同步修改的代码

当前模块由 `bootstrap.go` 显式创建、绑定 Adapter、注册生命周期，再由 `application.go` 和 `httpapi.Dependencies` 显式注入 Handler。新增模块不能只创建目录，必须修改整条装配链。

v0.12 必须同步修改：

```text
internal/server/server.go
internal/server/bootstrap.go
internal/server/application.go
internal/transport/httpapi/router.go
internal/platform/feature
admin/src/modules/features/catalog.ts
web/src/router
admin/src/router
```

新增：

```text
integration.email
feature.mutual-aid
feature.secondhand
```

每个业务 Feature 都要同时提供：

```text
Memory Adapter
PostgreSQL Adapter
Module
Service
Handler
前端 Path Gate
Feature Registry 定义
健康检查
模块边界测试
```

`integration.email` 不进入可关闭的 Feature Catalog；互助和二手交易可以进入。

---

# 十四、优化后的迁移规划

当前 v0.11 的可靠性迁移为 `000027`，因此 v0.12 从 `000028` 开始。

建议：

```text
000028_v12_identity_email_state
000029_v12_identity_challenges
000030_v12_identity_sessions
000031_v12_category_hierarchy
000032_v12_thread_types
000033_v12_mutual_aid
000034_v12_secondhand
```

## 000028

包含：

```text
accounts.verification_state
accounts.verified_at
accounts.verification_source
accounts.password_changed_at
accounts.credential_version
users.auth_version
大小写不敏感邮箱唯一索引
identity_legacy_email_placeholders
```

迁移前必须检查：

```text
lower(trim(users.email)) 重复
lower(trim(accounts.identifier)) 重复
同一用户存在多个 email account
users.email 与 accounts.identifier 不一致
孤立 accounts
```

发现冲突必须中止迁移并输出对账报告，不能自动选择保留对象。

## 000029

包含：

```text
identity_email_challenges
Challenge 索引
过期时间索引
邮箱/IP 时间窗口索引
```

## 000030

升级现有 `sessions`：

```text
refresh_token_hash
token_family_id
rotated_from_id
revoked_at
revoke_reason
last_used_at
```

不得保存原始 Refresh Token。

## 000031

包含：

```text
categories.node_kind
categories.lifecycle_status
categories.version
categories.color
相关 CHECK 和索引
```

## 000032

包含：

```text
threads.thread_type
category_thread_type_policies
RichText article 回填
现有板块策略回填
```

---

# 十五、实施阶段

| 阶段 | 内容 | 出口 |
|---|---|---|
| A0 | 基线修复、Color 合同、默认管理员安全、文档元数据 | v0.11 基线稳定 |
| A1 | 邮箱状态与历史数据预检 | Migration Ready |
| A2 | Challenge、Ticket、持久限流 | Identity Core Ready |
| A3 | Integration Email、SMTP、Fake Provider、Outbox Consumer | Mail Ready |
| A4 | 注册验证码前后端 | Registration Ready |
| A5 | Session、Refresh、Logout、Token Rotation | Session Ready |
| A6 | 密码重置、个人邮箱绑定、管理员辅助恢复 | `v0.12.0-alpha` |
| B1 | Category Repository 事务化和可靠命令 | Category Command Ready |
| B2 | 两级层级、归档、移动、影响预览 | Hierarchy Ready |
| B3 | Admin 树形管理和 Web 导航 | `v0.12.0-beta` |
| C0 | Thread Type、Policy、可靠创建、RichText 原子化 | Structured Kernel Ready |
| C1 | 校园互助模块 | Mutual Aid Ready |
| C2 | 二手交易模块 | `v0.12.0-rc` |
| Final | 迁移、恢复、故障注入、并发、浏览器验收 | `v0.12.0` |

---

# 十六、必须增加的测试

## Identity

```text
邮箱大小写唯一性
共享邮箱不能注册
共享邮箱不能重置密码
历史账号可登录但不能重置
验证后的新账号可登录
同一验证码并发只能成功一次
错误五次后 Challenge 锁定
过期 Outbox Replay 不再发信
Registration Ticket 只能消费一次
密码重置撤销全部 Session
Refresh Token 轮换后旧 Token 不可复用
修改邮箱后旧 Session 失效
```

## Category

```text
不能创建三级节点
group 不能发帖
closed board 不能发帖
archived board 不能发帖
不能移动到自己或子节点
并发 Move 只能有一个版本成功
归档影响预览准确
新建子板块不自动继承父级权限
```

## Structured Thread

```text
Thread 写入失败时 Detail 不存在
Detail 写入失败时 Thread 和 Revision 回滚
Audit 失败时全部回滚
Outbox 失败时全部回滚
停用子模块后禁止新建
停用子模块后历史帖子仍可治理
业务状态不能覆盖 Moderation 状态
RichText Article 写入失败时不留下回收站补偿帖子
```

---

# 十七、发布门禁

新增：

```bash
make identity-email-check
make identity-session-check
make category-hierarchy-check
make structured-thread-check
make v12-failure-injection-check
make v12-migration-check
make cold-start-check
make release-check
```

当前发布脚本最后仍硬编码输出：

```text
CampusOS v0.11 release check passed
```

v0.12 应改为读取统一 Version，或输出不带固定版本的文本。

`release-check` 必须新增：

```text
000028-000034 up/down/up 隔离演练
当前二进制从空数据库冷启动
历史 v0.11 数据升级
数据库恢复演练
Identity 浏览器注册与重置 Smoke
Category 树形管理 Smoke
互助和二手交易创建 Smoke
Feature 停用/启用回归
```

---

# 十八、v0.12 明确不包含的内容

为防止版本失控，以下继续排除：

```text
手机号登录
OAuth 登录重构
无限级 Category
父级权限动态继承
支付和订单
担保和资金托管
即时聊天
物流
退款
交易仲裁
External Plugin 事务参与
外部插件自定义 Thread Type
分布式限流
Prometheus/SLO 全面建设
容量和性能专项
```

---

# 十九、最终验收标准

v0.12 只有同时满足以下条件才能发布：

1. 所有新注册用户必须先验证邮箱；
2. 规范化个人邮箱全局一对一；
3. `1904650862@qq.com` 只作为历史占位或 SMTP 发件地址；
4. 历史账号不会因升级突然无法登录；
5. 未验证历史邮箱不能用于密码重置；
6. 密码重置后旧 Session、Refresh Token 和 Access Token 全部失效；
7. SMTP 故障不会生成半注册账号；
8. Category 只有两级，group 不能发帖；
9. Category 写操作、审计和事件原子提交；
10. 普通帖子和 RichText 创建不再依赖补偿回滚；
11. 互助和二手交易使用独立类型化子模块；
12. 子模块 Detail 与 Thread、Revision、Audit、Outbox 原子提交；
13. 子模块停用不会丢失历史数据或绕过内容治理；
14. 空数据库和 v0.11 历史数据库都能升级并启动；
15. `make release-check` 全部通过。

---

# 二十、正式文档目录

```text
docs/项目计划v0.12/
├── 00-v0.12版本计划书.md
├── 01-邮箱身份与历史账号迁移设计.md
├── 02-验证码与邮件投递安全设计.md
├── 03-Session与密码重置设计.md
├── 04-两级板块与权限设计.md
├── 05-结构化帖子事务内核设计.md
├── 06-校园互助子模块设计.md
├── 07-二手交易子模块设计.md
├── 08-v0.12测试与发布验收矩阵.md
└── 09-v0.12风险登记与回滚方案.md
```

每份文档顶部统一包含：

```text
状态
最后更新时间
适用版本
审查代码提交
负责人
关联 ADR
```

---

## 最终结论

优化后的 v0.12 主线应调整为：

```text
邮箱事实收敛
→ Challenge 与可靠邮件
→ Session 和密码恢复
→ 历史账号邮箱治理
→ Category 可靠命令
→ 两级板块
→ 普通帖子和 RichText 原子化
→ Structured Thread Kernel
→ 校园互助
→ 二手交易
→ 完整发布验收
```

其中最关键的变化是：

> 先解决现有 Identity 和 Thread 写入的一致性，再开发新的业务子模块。

否则，互助和二手交易只会把当前注册、会话和 RichText 已存在的非原子问题进一步放大。

该版本可作为正式 v0.12 计划书的替换基线；相较上一版，邮箱事实、Token 失效、邮件模块边界和结构化帖子事务方式已经与当前仓库实现对齐。
