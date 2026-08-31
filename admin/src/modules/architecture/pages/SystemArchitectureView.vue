<template>
  <div class="system-architecture">
    <section class="page-header">
      <div>
        <p class="eyebrow">只读架构视图</p>
        <h2>系统数据架构</h2>
        <p>
          从当前迁移文件和数据目录整理的结构图，用于了解表的逻辑关联、职责和不在
          PostgreSQL 中保存的文件数据。
        </p>
      </div>
      <el-tag type="info" effect="plain">迁移 000001 - 000049</el-tag>
    </section>

    <el-alert
      class="architecture-alert"
      type="info"
      :closable="false"
      show-icon
    >
      PostgreSQL 已对核心身份、管理员准入、社区、个人空间、富文本和 Webhook
      关系启用外键；其余连线表示代码与字段可追踪的逻辑关系。本页不会读取或展示真实业务记录。
    </el-alert>

    <el-tabs v-model="activeTab" class="architecture-tabs">
      <el-tab-pane label="数据库关系" name="database">
        <section class="toolbar-band">
          <div>
            <strong>ER 逻辑关系图</strong>
            <p>点击任意表卡片查看用途、关键字段和迁移来源。</p>
          </div>
          <el-radio-group v-model="domainFilter" aria-label="数据库领域筛选">
            <el-radio-button label="all">全部</el-radio-button>
            <el-radio-button label="identity">身份与社区</el-radio-button>
            <el-radio-button label="space">空间与内容</el-radio-button>
            <el-radio-button label="plugin">插件、集成与系统</el-radio-button>
          </el-radio-group>
        </section>

        <div class="database-layout">
          <section class="relation-board" aria-label="数据库逻辑关系图">
            <article
              v-for="relation in visibleRelations"
              :key="relation.id"
              class="relation-lane"
            >
              <button
                class="table-node"
                :class="`domain-${tableByName(relation.source).domain}`"
                type="button"
                :aria-pressed="selectedTable === relation.source"
                @click="selectedTable = relation.source"
              >
                <span class="node-domain">{{
                  domainLabel(tableByName(relation.source).domain)
                }}</span>
                <strong>{{ relation.source }}</strong>
                <small>{{ tableByName(relation.source).title }}</small>
              </button>
              <div class="relation-link">
                <span>{{ relation.sourceCardinality }}</span>
                <i></i>
                <em>{{ relation.label }}</em>
                <i></i>
                <span>{{ relation.targetCardinality }}</span>
              </div>
              <button
                class="table-node"
                :class="`domain-${tableByName(relation.target).domain}`"
                type="button"
                :aria-pressed="selectedTable === relation.target"
                @click="selectedTable = relation.target"
              >
                <span class="node-domain">{{
                  domainLabel(tableByName(relation.target).domain)
                }}</span>
                <strong>{{ relation.target }}</strong>
                <small>{{ tableByName(relation.target).title }}</small>
              </button>
            </article>
          </section>

          <aside class="table-inspector" aria-live="polite">
            <div class="inspector-heading">
              <div>
                <span class="node-domain">{{
                  domainLabel(currentTable.domain)
                }}</span>
                <h3>{{ currentTable.name }}</h3>
                <p>{{ currentTable.title }}</p>
              </div>
              <el-tag effect="plain">{{ currentTable.migration }}</el-tag>
            </div>
            <p class="table-purpose">{{ currentTable.purpose }}</p>
            <el-descriptions :column="1" border size="small">
              <el-descriptions-item label="关键字段">
                <div class="field-list">
                  <code v-for="field in currentTable.fields" :key="field">{{
                    field
                  }}</code>
                </div>
              </el-descriptions-item>
              <el-descriptions-item label="关联提示">
                {{ currentTable.relationshipNote }}
              </el-descriptions-item>
              <el-descriptions-item label="持久化位置"
                >PostgreSQL</el-descriptions-item
              >
            </el-descriptions>
          </aside>
        </div>

        <section class="catalog-section">
          <div class="section-heading">
            <div>
              <h3>全部数据表</h3>
              <p>按业务域分组；选择后右侧会显示对应说明。</p>
            </div>
            <el-tag type="info" effect="plain"
              >{{ visibleTables.length }} 张表</el-tag
            >
          </div>
          <div class="table-catalog">
            <button
              v-for="table in visibleTables"
              :key="table.name"
              class="catalog-node"
              :class="[
                `domain-${table.domain}`,
                { selected: selectedTable === table.name },
              ]"
              type="button"
              @click="selectedTable = table.name"
            >
              <strong>{{ table.name }}</strong>
              <span>{{ table.title }}</span>
            </button>
          </div>
        </section>
      </el-tab-pane>

      <el-tab-pane label="数据与文件" name="storage">
        <section class="toolbar-band">
          <div>
            <strong>持久化数据全景</strong>
            <p>
              业务元数据进入
              PostgreSQL；文件、插件运行数据和日志按目录归属保存。
            </p>
          </div>
          <el-tag type="warning" effect="plain">本地开发默认布局</el-tag>
        </section>

        <section class="data-flow" aria-label="应用数据流图">
          <article class="flow-node flow-client">
            <el-icon><DataAnalysis /></el-icon>
            <strong>web / admin</strong>
            <span>浏览、管理、上传和配置</span>
          </article>
          <div class="flow-arrow" aria-hidden="true">
            <span>HTTP / JSON</span>
          </div>
          <article class="flow-node flow-api">
            <el-icon><Connection /></el-icon>
            <strong>CampusOS API</strong>
            <span>鉴权、业务规则、文件分类和插件 Runtime</span>
          </article>
          <div class="flow-arrow flow-branch" aria-hidden="true">
            <span>读写</span>
          </div>
          <div class="flow-targets">
            <article class="flow-node flow-postgres">
              <el-icon><Document /></el-icon>
              <strong>PostgreSQL</strong>
              <span>用户、社区、配置、插件元数据、审计和集成记录</span>
            </article>
            <article class="flow-node flow-files">
              <el-icon><FolderOpened /></el-icon>
              <strong>本地 data/</strong>
              <span>个人文件、插件代码、插件数据、风格包和本地配置</span>
            </article>
            <article class="flow-node flow-logs">
              <el-icon><Document /></el-icon>
              <strong>.campusos/logs/</strong>
              <span>API、web、admin 开发期输出，管理端可只读查看</span>
            </article>
          </div>
        </section>

        <section class="storage-grid">
          <el-card
            v-for="store in storageRows"
            :key="store.path"
            shadow="never"
            class="storage-card"
          >
            <template #header>
              <div class="storage-card-heading">
                <code>{{ store.path }}</code>
                <el-tag :type="store.type" size="small" effect="plain">{{
                  store.category
                }}</el-tag>
              </div>
            </template>
            <p>{{ store.purpose }}</p>
            <ul>
              <li v-for="item in store.contents" :key="item">{{ item }}</li>
            </ul>
            <div class="storage-note">{{ store.note }}</div>
          </el-card>
        </section>

        <el-alert
          title="个人空间数据会计入用户本地配额；插件实现目录与插件运行数据目录必须分开。数据库备份不能替代 data/ 与 .env 的文件备份。"
          type="warning"
          :closable="false"
          show-icon
        />
      </el-tab-pane>

      <el-tab-pane label="迁移与维护" name="migrations">
        <section class="toolbar-band">
          <div>
            <strong>Schema 变更来源</strong>
            <p>
              每一行对应仓库中的 migration
              文件；本页不会绕过迁移系统直接修改数据库。
            </p>
          </div>
          <el-tag type="success" effect="plain"
            >schema_migrations 记录执行状态</el-tag
          >
        </section>

        <el-timeline class="migration-timeline">
          <el-timeline-item
            v-for="migration in migrations"
            :key="migration.version"
            :timestamp="migration.file"
            placement="top"
          >
            <el-card shadow="never" class="migration-card">
              <div class="migration-card-heading">
                <div>
                  <code>{{ migration.version }}</code>
                  <strong>{{ migration.title }}</strong>
                </div>
                <el-tag size="small" effect="plain">{{
                  migration.scope
                }}</el-tag>
              </div>
              <p>{{ migration.summary }}</p>
              <div class="migration-tables">
                <el-tag
                  v-for="table in migration.tables"
                  :key="table"
                  size="small"
                  effect="plain"
                  >{{ table }}</el-tag
                >
              </div>
            </el-card>
          </el-timeline-item>
        </el-timeline>

        <section class="maintenance-grid">
          <article>
            <h3>查看迁移状态</h3>
            <code>make migrate-status</code>
            <p>核对数据库已执行的版本与仓库 migration 文件是否一致。</p>
          </article>
          <article>
            <h3>应用迁移</h3>
            <code>make migrate-up</code>
            <p>
              只执行尚未记录在 <code>schema_migrations</code> 中的 up
              migration。
            </p>
          </article>
          <article>
            <h3>备份边界</h3>
            <code>PostgreSQL + data/ + .env</code>
            <p>
              恢复时三者需要使用同一时间点的备份，尤其是用户头像、图文图片和插件文件。
            </p>
          </article>
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  Connection,
  DataAnalysis,
  Document,
  FolderOpened,
} from "@element-plus/icons-vue";

type Domain =
  "identity" | "community" | "space" | "plugin" | "integration" | "system";

interface DbTable {
  name: string;
  title: string;
  domain: Domain;
  purpose: string;
  fields: string[];
  migration: string;
  relationshipNote: string;
}

interface Relation {
  id: string;
  source: string;
  target: string;
  sourceCardinality: string;
  targetCardinality: string;
  label: string;
  domains: Domain[];
}

const activeTab = ref("database");
const domainFilter = ref<"all" | "identity" | "space" | "plugin">("all");
const selectedTable = ref("users");

const databaseTables: DbTable[] = [
  {
    name: "schema_migrations",
    title: "迁移执行记录",
    domain: "system",
    purpose: "迁移脚本创建的系统表，记录每个 migration 是否已执行。",
    fields: ["version", "name", "applied_at"],
    migration: "scripts/migrate.sh / migrate.ps1",
    relationshipNote: "只用于 schema 版本管理，不与业务表建立关系。",
  },
  {
    name: "users",
    title: "用户",
    domain: "identity",
    purpose: "系统中的用户主体，社区、个人空间、权限和上传资源的归属起点。",
    fields: ["id", "username", "email", "status", "auth_version"],
    migration: "000001 + 000016 + 000028",
    relationshipNote:
      "通过受外键约束的 user_id、author_id、created_by、uploader_id 等字段被多个业务表引用。",
  },
  {
    name: "accounts",
    title: "登录凭据",
    domain: "identity",
    purpose: "保存邮箱、手机号或 OAuth 标识及密码哈希等认证凭据；邮箱使用规范化标识和显式验证状态。",
    fields: ["id", "user_id", "type", "identifier_normalized", "verification_state"],
    migration: "000001 + 000016 + 000028",
    relationshipNote: "一个用户可有多个登录账号；邮箱账户以规范化标识全局唯一，user_id 由外键保护。",
  },
  {
    name: "identity_admin_accounts",
    title: "管理员准入账号",
    domain: "identity",
    purpose:
      "独立保存管理平面的准入状态、凭据引用、最近认证和变更原因；管理员角色负责授权，本表负责是否允许进入 Admin。",
    fields: [
      "id",
      "user_id",
      "credential_account_id",
      "status",
      "status_reason",
      "status_changed_by",
      "status_changed_at",
      "last_authenticated_at",
      "version",
    ],
    migration: "000038 + 000039",
    relationshipNote:
      "user_id 与 credential_account_id 均由外键保护；全局 admin 角色变更通过数据库触发器同步 active/revoked，suspended 不会被普通角色刷新静默恢复。",
  },
  {
    name: "identity_legacy_email_placeholders",
    title: "历史邮箱占位标记",
    domain: "identity",
    purpose: "保存历史共享邮箱的迁移标记，不是登录凭据、验证目的地或用户邮箱绑定。",
    fields: ["id", "user_id", "placeholder_email", "migration_source", "resolved_at"],
    migration: "000028",
    relationshipNote: "user_id 由外键指向 users；未解决标记每个用户最多一条，不参与 accounts 登录和找回。",
  },
  {
    name: "identity_reserved_identifiers",
    title: "保留身份标识",
    domain: "identity",
    purpose: "声明不能被新注册、绑定或找回流程使用的规范化身份标识及其原因。",
    fields: ["identifier_type", "identifier_normalized", "reason", "created_at"],
    migration: "000028",
    relationshipNote: "独立策略表；v12 首项用于阻止历史共享邮箱被作为个人邮箱重新注册。",
  },
  {
    name: "identity_email_challenges",
    title: "邮箱验证挑战",
    domain: "identity",
    purpose: "保存验证码的用途、HMAC 重建元数据、尝试次数和一次性 Ticket 摘要；不保存验证码或原始 Ticket。",
    fields: ["id", "public_id", "purpose", "account_id", "ticket_digest", "expires_at"],
    migration: "000029",
    relationshipNote: "account_id 可空外键指向 accounts；Outbox 只保存 challenge_id，邮件正文、验证码和 Secret 不进入该表以外的日志或队列。",
  },
  {
    name: "identity_challenge_rate_limits",
    title: "验证请求限流窗口",
    domain: "identity",
    purpose: "按规范化邮箱或 IP 的 HMAC 摘要保存滑动窗口请求计数，跨进程保持一致。",
    fields: ["scope", "subject_digest", "window_started_at", "request_count", "updated_at"],
    migration: "000029 + 000036",
    relationshipNote: "subject_digest 不是原始邮箱或 IP；通过事务级锁与 Challenge 创建在同一事务内更新，没有业务外键。",
  },
  {
    name: "identity_challenge_policies",
    title: "验证码频率策略",
    domain: "identity",
    purpose: "保存不可关闭、可由管理员热更新的邮箱与 IP 滑动窗口和次数上限。",
    fields: ["id", "email_window_minutes", "email_max_requests", "ip_window_minutes", "ip_max_requests", "version"],
    migration: "000036",
    relationshipNote: "updated_by 可空外键指向 users；策略更新使用版本检查、required audit 和非敏感 Outbox 事件。",
  },
  {
    name: "identity_account_recovery_cases",
    title: "管理员辅助恢复 Case",
    domain: "identity",
    purpose:
      "记录经过线下核验后发起的受控恢复流程；不保存密码、验证码、Ticket 或线下证明原文。",
    fields: ["id", "public_id", "user_id", "account_id", "challenge_id", "status", "expires_at"],
    migration: "000031",
    relationshipNote:
      "关联用户、原账号、邮件 Challenge 和可选创建人；管理端仅显示脱敏目标邮箱，证明材料只保留非敏感编号。",
  },
  {
    name: "sessions",
    title: "刷新会话",
    domain: "identity",
    purpose: "保存不透明 Refresh Token 的摘要、设备、轮换家族、撤销状态与服务端 MFA 认证强度，不保存原始凭据。",
    fields: ["id", "user_id", "refresh_token_digest", "token_family_id", "authentication_strength", "mfa_authenticated_at", "revoked_at", "expires_at"],
    migration: "000001 + 000016 + 000030 + 000040",
    relationshipNote: "一个用户可在多个设备保持会话；user_id 由外键保护，密码恢复、邮箱绑定、管理员暂停或 MFA 关闭会撤销对应会话。",
  },
  {
    name: "identity_mfa_totp_methods",
    title: "TOTP 认证器",
    domain: "identity",
    purpose: "保存用户认证器的加密信封、状态和最近接受时间步；不保存明文 TOTP Secret、二维码或手工密钥。",
    fields: ["id", "user_id", "status", "key_id", "nonce", "ciphertext", "last_accepted_step", "enrollment_expires_at"],
    migration: "000040",
    relationshipNote: "user_id 由外键指向 users；每个用户最多一个 active 和一个 pending 认证器，重放保护依赖 last_accepted_step。",
  },
  {
    name: "identity_mfa_tickets",
    title: "MFA 登录 Ticket",
    domain: "identity",
    purpose: "保存密码成功后的短期单用途 MFA Ticket 摘要、用途、受众与失败次数；不保存原始 Ticket。",
    fields: ["id", "user_id", "audience", "purpose", "ticket_digest", "expires_at", "consumed_at", "attempts"],
    migration: "000040",
    relationshipNote: "user_id 由外键指向 users；digest 全局唯一，消费和失败次数在事务中更新，不能用作长期会话。",
  },
  {
    name: "identity_mfa_recovery_codes",
    title: "MFA 恢复码摘要",
    domain: "identity",
    purpose: "保存一次性恢复码的不可逆摘要和使用时间；页面只在生成时显示原始恢复码一次。",
    fields: ["id", "user_id", "method_id", "code_digest", "used_at", "created_at"],
    migration: "000040",
    relationshipNote: "user_id 与 method_id 分别由外键指向 users 和 identity_mfa_totp_methods；一条摘要只能消费一次。",
  },
  {
    name: "identity_mfa_policies",
    title: "管理员 MFA 策略",
    domain: "identity",
    purpose: "保存管理员 MFA 的 off、注册宽限期或 required 策略、版本和更新人；不保存个人认证器材料。",
    fields: ["id", "mode", "grace_ends_at", "version", "updated_by", "updated_at"],
    migration: "000040",
    relationshipNote: "固定 admin 策略行；updated_by 可空外键指向 users，策略更新使用版本检查和 required audit。",
  },
  {
    name: "roles",
    title: "角色",
    domain: "identity",
    purpose:
      "定义 admin、moderator、member、guest 等系统角色；member 是有效用户的隐式基础角色。",
    fields: ["id", "name", "is_system"],
    migration: "000002",
    relationshipNote:
      "user_roles 保存额外授权；permissions 是兼容期旧授权，role_permissions 绑定 v10 权限目录。",
  },
  {
    name: "user_roles",
    title: "用户角色",
    domain: "identity",
    purpose:
      "用户与额外角色的关联；管理员使用 global 作用域，版主使用一个或多个 category 作用域。",
    fields: ["user_id", "role_id", "scope_type", "scope_id"],
    migration: "000002 + 000014 - 000016",
    relationshipNote:
      "user_id 和 role_id 由外键保护；版主 scope_id 逻辑指向 categories.id，跨版块请求由后端拒绝。",
  },
  {
    name: "permissions",
    title: "角色权限",
    domain: "identity",
    purpose: "为角色声明资源和动作权限；角色管理与各管理域使用独立 action。",
    fields: ["role_id", "resource", "action"],
    migration: "000002 + 000014 - 000017",
    relationshipNote:
      "role_id 由外键保护；版主权限还必须匹配 user_roles 中的 category scope。",
  },
  {
    name: "permission_definitions",
    title: "权限目录",
    domain: "identity",
    purpose:
      "定义稳定 Permission Code、风险等级、允许作用域和审计等级；不以 URL 或数字 ID 表达业务权限。",
    fields: ["id", "code", "domain", "resource", "action", "risk_level"],
    migration: "000025",
    relationshipNote:
      "由 role_permissions 和 route_permission_bindings 关联；旧 permissions 在兼容期仍保留。",
  },
  {
    name: "role_permissions",
    title: "角色权限绑定",
    domain: "identity",
    purpose: "将角色与 v10 权限目录多对多关联，支持软删除的历史保留。",
    fields: ["id", "role_id", "permission_id", "created_by", "deleted_at"],
    migration: "000025",
    relationshipNote:
      "role_id 和 permission_id 都有外键；系统角色矩阵由服务端保护。",
  },
  {
    name: "route_operations",
    title: "路由操作目录",
    domain: "identity",
    purpose: "记录 API 的稳定操作 ID、模块所有者、HTTP 方法、路径与兼容别名。",
    fields: ["id", "operation_code", "module_owner", "method", "path_template"],
    migration: "000025",
    relationshipNote:
      "启动时从 Route Registry 同步；一个操作可绑定一个或多个权限定义。",
  },
  {
    name: "route_permission_bindings",
    title: "路由权限绑定",
    domain: "identity",
    purpose: "连接路由操作和权限定义，避免以路径字符串直接充当权限。",
    fields: ["id", "route_operation_id", "permission_id", "deleted_at"],
    migration: "000025",
    relationshipNote: "两端均有外键；最终资源范围仍由 Service 从真实数据验证。",
  },
  {
    name: "authorization_audits",
    title: "授权记录",
    domain: "identity",
    purpose: "保存授权判定、角色调整与作用域变更的最小结构化审计证据。",
    fields: [
      "actor_id",
      "permission_code",
      "operation_code",
      "scope_type",
      "outcome",
    ],
    migration: "000025",
    relationshipNote:
      "记录 request_id、原因和资源摘要，不保存 Session、Token、JWT 私钥或 Secret。",
  },
  {
    name: "platform_outbox",
    title: "持久事件队列",
    domain: "system",
    purpose: "可靠命令提交后的事务性事件源，Worker 以 lease 领取并处理。",
    fields: ["id", "event_type", "schema_version", "status", "attempts", "lease_generation"],
    migration: "000027",
    relationshipNote: "命令审计、消费凭证和投递尝试通过 event_id 关联；payload 不得保存 Token、Secret 或密码。",
  },
  {
    name: "outbox_consumer_receipts",
    title: "事件消费凭证",
    domain: "system",
    purpose: "保存指定消费者已成功确认的事件，缩小 Worker 崩溃后的重复副作用窗口。",
    fields: ["consumer_name", "event_id", "attempt", "delivered_at"],
    migration: "000027",
    relationshipNote: "event_id 由外键指向 platform_outbox；外部 HTTP 仍是至少一次语义。",
  },
  {
    name: "platform_outbox_attempts",
    title: "事件消费尝试",
    domain: "system",
    purpose: "记录每个 Worker 消费尝试、lease generation、消费者结果和系统最终化证据，供 dead-letter 重放审查。",
    fields: ["event_id", "consumer_name", "worker_id", "attempt", "status"],
    migration: "000027 + 000037",
    relationshipNote: "event_id 由外键指向 platform_outbox；system:outbox-finalize 可记录 failed 状态，但不复制业务 payload。",
  },
  {
    name: "platform_command_audits",
    title: "可靠命令审计",
    domain: "system",
    purpose: "关联 command、操作者、权限、资源、请求和持久事件的最小审计封套。",
    fields: ["command_id", "command_code", "permission_code", "event_id", "created_at"],
    migration: "000027",
    relationshipNote: "event_id 可空外键指向 platform_outbox；业务写入、required audit 和事件在同一事务内提交。",
  },
  {
    name: "platform_worker_leases",
    title: "Worker 心跳",
    domain: "system",
    purpose: "显示可靠 Worker 的最近心跳，用于识别积压和处理器不可用。",
    fields: ["worker_id", "last_heartbeat_at", "updated_at"],
    migration: "000027",
    relationshipNote: "独立运行状态表，不保存用户或事件 payload。",
  },
  {
    name: "platform_operation_runs",
    title: "可恢复操作",
    domain: "system",
    purpose: "保存插件包和资源应用等跨文件系统工作流的 intent、结果和补偿失败证据。",
    fields: ["kind", "subject_type", "subject_id", "status", "idempotency_key"],
    migration: "000027",
    relationshipNote: "不把文件替换、进程或网络调用伪装成数据库事务；失败状态可供管理员检查。",
  },
  {
    name: "platform_compatibility_usage",
    title: "兼容路径遥测",
    domain: "system",
    purpose: "统计遗留资源目录、Manifest 或兼容入口的实际使用量，为删除决策提供证据。",
    fields: ["usage_key", "usage_kind", "first_seen", "last_seen", "usage_count"],
    migration: "000027",
    relationshipNote: "只保存兼容路径摘要，不保存用户 Token、请求正文或 Secret。",
  },
  {
    name: "platform_retention_runs",
    title: "保留策略预演",
    domain: "system",
    purpose: "记录受控 dry-run 的目标、截止时间和候选数量；v11 不执行删除。",
    fields: ["target", "before_at", "eligible_rows", "mode", "status"],
    migration: "000027",
    relationshipNote: "当前只允许 dry-run，正式清理由后续经审批的分批任务实现。",
  },
  {
    name: "categories",
    title: "版块",
    domain: "community",
    purpose: "社区内容分区；支持根级 group、board、默认标签和可审计的归档状态。",
    fields: ["id", "parent_id", "node_kind", "lifecycle_status", "version", "color", "default_tags"],
    migration: "000001 + 000012 + 000016 + 000032",
    relationshipNote:
      "group 只位于根级，board 只能挂到活动 group；parent_id、threads.category_id 和 user_space_contents.category_id 由约束或外键保护。",
  },
  {
    name: "category_thread_type_policies",
    title: "板块帖子类型策略",
    domain: "community",
    purpose:
      "限定一个 board 可以新建的固定结构化帖子类型；策略只影响新建，不删除或隐藏已有内容。",
    fields: ["category_id", "thread_type", "enabled", "updated_at"],
    migration: "000033",
    relationshipNote:
      "category_id 由外键指向 board；数据库触发器拒绝 group、已删除或不存在的板块策略。",
  },
  {
    name: "threads",
    title: "主题 / 文章入口",
    domain: "community",
    purpose:
      "普通帖子和富文本文章共用的顶层内容实体；thread_type 表达业务类型，v10 三维状态决定可见性。",
    fields: [
      "id",
      "author_id",
      "category_id",
      "thread_type",
      "status",
      "publication_status",
      "moderation_status",
      "deletion_status",
    ],
    migration: "000001 + 000016 + 000023 + 000033",
    relationshipNote:
      "作者和版块由外键保护，并关联回复、内容修订、审核记录、个人空间兼容投影和富文本正文。",
  },
  {
    name: "mutual_aid_details",
    title: "校园互助详情",
    domain: "community",
    purpose:
      "互助帖的类型、业务状态、截止时间、位置范围和联系约定；不保存身份、支付或完整住宿敏感信息。",
    fields: ["thread_id", "aid_type", "aid_status", "deadline", "location_scope", "contact_mode", "version", "created_by"],
    migration: "000034",
    relationshipNote:
      "thread_id 一对一关联 mutual_aid 类型主题，created_by 必须匹配主题作者；业务状态不改变 Community 的发布、审核或回收站状态。",
  },
  {
    name: "secondhand_details",
    title: "校园二手详情",
    domain: "community",
    purpose:
      "二手帖的分价金额、CNY 币种、物品成色、交付方式、交易进度和位置范围；不保存支付、订单或仲裁数据。",
    fields: ["thread_id", "price_minor", "currency", "item_condition", "trade_method", "trade_status", "location_scope", "version", "created_by"],
    migration: "000035",
    relationshipNote:
      "thread_id 一对一关联 secondhand 类型主题，created_by 必须匹配主题作者；交易业务状态不改变 Community 的发布、审核或回收站状态。",
  },
  {
    name: "content_revisions",
    title: "内容修订",
    domain: "community",
    purpose: "保存内容状态转换时的标题、正文、格式、标签和版本快照。",
    fields: [
      "id",
      "thread_id",
      "version",
      "content_format",
      "action",
      "created_by",
    ],
    migration: "000023",
    relationshipNote: "thread_id 与 threads 关联；同一主题的 version 唯一。",
  },
  {
    name: "content_moderation_cases",
    title: "内容审核案例",
    domain: "community",
    purpose: "保存下架、整改和审核流程的案例状态、原因与处理人。",
    fields: ["id", "thread_id", "status", "reason", "opened_by", "resolved_by"],
    migration: "000023",
    relationshipNote: "thread_id 指向内容事实；开放案例与审核动作按时间关联。",
  },
  {
    name: "content_moderation_actions",
    title: "内容治理动作",
    domain: "community",
    purpose: "保存下架、重提、审核、恢复、回收站和永久清除的前后状态证据。",
    fields: [
      "id",
      "case_id",
      "thread_id",
      "action",
      "actor_id",
      "before_state",
      "after_state",
    ],
    migration: "000023",
    relationshipNote: "case_id 可为空；thread_id 是治理资源的稳定归属。",
  },
  {
    name: "posts",
    title: "回复",
    domain: "community",
    purpose: "主题下的回复，可通过 parent_id 形成引用/嵌套关系。",
    fields: [
      "id",
      "thread_id",
      "author_id",
      "parent_id",
      "parent_floor_number",
      "floor_number",
    ],
    migration: "000001 + 000016 + 000043",
    relationshipNote:
      "thread_id、author_id 和 parent_id 由外键保护；parent_floor_number 是创建时快照，父回复删除后引用楼层仍稳定。",
  },
  {
    name: "tags",
    title: "标签字典",
    domain: "community",
    purpose: "保留可管理标签元数据；主题当前也以 tags 数组保存标签快照。",
    fields: ["id", "name", "slug", "thread_count"],
    migration: "000001",
    relationshipNote: "当前没有 thread_tags 关联表，主题标签以数组字段为主。",
  },
  {
    name: "likes",
    title: "点赞",
    domain: "community",
    purpose: "记录用户对主题或回复的点赞。",
    fields: ["user_id", "target_type", "target_id"],
    migration: "000001",
    relationshipNote:
      "target_type + target_id 是多态关联，可指向 threads 或 posts。",
  },
  {
    name: "notifications",
    title: "站内通知",
    domain: "community",
    purpose: "保存通知内容、已读状态和跳转地址。",
    fields: ["user_id", "type", "is_read"],
    migration: "000001",
    relationshipNote: "按 user_id 投递给一个用户。",
  },
  {
    name: "audit_logs",
    title: "操作审计",
    domain: "community",
    purpose: "记录操作人、资源、前后数据和 trace id。",
    fields: ["actor_id", "action", "resource", "resource_id"],
    migration: "000001",
    relationshipNote: "actor_id 逻辑指向用户；resource 为多态业务资源。",
  },
  {
    name: "configurations",
    title: "系统配置",
    domain: "community",
    purpose: "保存分组配置及其更新人。",
    fields: ["key", "value", "category", "updated_by"],
    migration: "000001",
    relationshipNote: "updated_by 记录逻辑上的用户归属。",
  },
  {
    name: "api_keys",
    title: "API Key",
    domain: "plugin",
    purpose: "独立管理用户或插件使用的 API Key。",
    fields: ["key", "user_id", "plugin_name", "permissions"],
    migration: "000003",
    relationshipNote: "可关联用户或插件名称，两者属于可选逻辑归属。",
  },
  {
    name: "plugins",
    title: "插件元数据",
    domain: "plugin",
    purpose: "保存插件 manifest、三轴运行状态、配置、checksum 和 UI revision。",
    fields: [
      "name",
      "runtime",
      "status",
      "backend_state",
      "frontend_state",
      "health_state",
      "ui_revision",
      "manifest",
      "config",
    ],
    migration: "000003 + 000011 + 000018",
    relationshipNote:
      "通过 plugin_name 与插件权限、日志和 API Key 形成名称关联。",
  },
  {
    name: "builtin_feature_states",
    title: "内置功能状态",
    domain: "system",
    purpose:
      "保存 Personal Space、RichText、Schedule 和 Appearance 的期望/生效启停状态和权威功能配置；不属于可卸载外部插件。",
    fields: [
      "feature_id",
      "desired_enabled",
      "effective_enabled",
      "pending_restart",
      "config",
      "updated_at",
    ],
    migration: "000019/000020",
    relationshipNote:
      "feature_id 对应 Built-in Feature Registry 的稳定 ID；旧插件状态和配置仅在首次迁移时作为兼容来源。",
  },
  {
    name: "plugin_permissions",
    title: "插件权限",
    domain: "plugin",
    purpose: "保存插件声明或同步后的 Host API 权限。",
    fields: ["plugin_name", "permission_type", "permission_value"],
    migration: "000005",
    relationshipNote: "plugin_name 逻辑指向 plugins.name。",
  },
  {
    name: "plugin_logs",
    title: "插件日志",
    domain: "plugin",
    purpose: "保存插件运行、事件和 Host API 日志。",
    fields: ["plugin_name", "level", "event_type", "trace_id"],
    migration: "000005",
    relationshipNote: "plugin_name 逻辑指向 plugins.name。",
  },
  {
    name: "plugin_records",
    title: "受管插件记录",
    domain: "plugin",
    purpose:
      "Host 托管的 v2 插件结构化记录，按插件、所有者和 collection 隔离。",
    fields: [
      "plugin_name",
      "owner_type",
      "owner_id",
      "collection",
      "record_key",
      "version",
    ],
    migration: "000021",
    relationshipNote:
      "plugin_name 逻辑指向 plugins.name；owner_id 由 Host 注入，system 或 user 所有者均不接受插件伪造。",
  },
  {
    name: "plugin_file_metadata",
    title: "插件用户文件元数据",
    domain: "plugin",
    purpose: "记录用户插件附件的受控文件 ID、类型、配额计数和保留策略。",
    fields: [
      "plugin_name",
      "owner_id",
      "storage_key",
      "size_bytes",
      "retention",
    ],
    migration: "000021",
    relationshipNote:
      "plugin_name 和 owner_id 是逻辑归属；实际文件位于个人空间 plugins 子目录。",
  },
  {
    name: "plugin_user_grants",
    title: "插件用户授权",
    domain: "plugin",
    purpose: "保存用户对 v2 插件个人数据能力的精确版本化授权或撤销状态。",
    fields: ["plugin_name", "user_id", "version", "permissions", "status"],
    migration: "000021",
    relationshipNote:
      "plugin_name 和 user_id 是逻辑关系；每次 Gateway 调用均重新核验。",
  },
  {
    name: "plugin_catalog_entries",
    title: "本地插件目录",
    domain: "plugin",
    purpose:
      "保存已安装 v2 外部插件在本地目录中的可见性、风险摘要、普通用户说明和权限信息。",
    fields: [
      "plugin_name",
      "version",
      "visibility",
      "package_checksum",
      "user_permissions",
      "experience",
    ],
    migration: "000021 + 000022 + 000024",
    relationshipNote:
      "plugin_name 逻辑指向 plugins.name；目录下架不卸载插件或删除用户数据。",
  },
  {
    name: "plugin_install_requests",
    title: "插件安装申请",
    domain: "plugin",
    purpose: "记录用户请求管理员发布或审核本地目录插件的说明和结果。",
    fields: ["plugin_name", "user_id", "status", "reviewed_by"],
    migration: "000021",
    relationshipNote:
      "plugin_name、user_id 和 reviewed_by 为逻辑关系；批准不自动安装宿主代码。",
  },
  {
    name: "plugin_releases",
    title: "插件发布记录",
    domain: "plugin",
    purpose: "保存本地包版本、摘要、签名状态、通道和发布状态的治理记录。",
    fields: [
      "plugin_name",
      "version",
      "checksum",
      "signature_state",
      "rollout_state",
    ],
    migration: "000021",
    relationshipNote:
      "plugin_name 逻辑指向 plugins.name；verified 状态只能来自实际包签名校验。",
  },
  {
    name: "plugin_market_audits",
    title: "插件市场审计",
    domain: "plugin",
    purpose: "记录目录、授权、数据删除和发布治理动作的结果与最小元数据。",
    fields: ["plugin_name", "actor_id", "action", "outcome", "created_at"],
    migration: "000021",
    relationshipNote:
      "plugin_name 和 actor_id 为逻辑关系；审计不会保存用户文件正文或 Secret。",
  },
  {
    name: "ai_call_logs",
    title: "AI 调用日志",
    domain: "integration",
    purpose: "记录模型提供方、用量、耗时和失败原因。",
    fields: ["provider", "model", "source", "status"],
    migration: "000006",
    relationshipNote: "按调用来源记录，不强制绑定用户或帖子。",
  },
  {
    name: "user_spaces",
    title: "个人主页",
    domain: "space",
    purpose: "保存用户公开主页、同步设置、样式和禁用状态。",
    fields: ["user_id", "visibility", "style_name", "sync_enabled"],
    migration: "000007 + 000009 + 000011 + 000016",
    relationshipNote: "一个用户最多一份个人主页配置；user_id 由外键保护。",
  },
  {
    name: "user_storage_quotas",
    title: "用户空间配额授权",
    domain: "space",
    purpose: "只保存管理员对单个用户授予的 User Storage 配额覆盖；没有记录时使用系统默认 50 MB。",
    fields: ["user_id", "quota_bytes", "updated_by", "updated_at"],
    migration: "000042",
    relationshipNote: "user_id 是主键并外键指向 users；updated_by 可空外键记录最近授权管理员。",
  },
  {
    name: "academic_terms",
    title: "学期目录",
    domain: "space",
    purpose: "保存由管理员治理的春/秋学期、第一周、开放状态、默认项与乐观锁版本；不是用户课表正文。",
    fields: [
      "id",
      "year",
      "semester",
      "first_week_start",
      "status",
      "is_default",
      "version",
      "created_by",
      "updated_by",
    ],
    migration: "000044",
    relationshipNote:
      "year + semester 全局唯一；仅 open 学期可为默认；created_by 与 updated_by 是可空外键，关闭状态保留 closed_at 证据。",
  },
  {
    name: "user_storage_accounts",
    title: "对象存储账户账本",
    domain: "space",
    purpose: "保存用户对象存储的已用字节、预留字节与版本，避免并发写入绕过个人空间配额。",
    fields: ["user_id", "used_bytes", "reserved_bytes", "version", "updated_at"],
    migration: "000045",
    relationshipNote: "每个用户至多一行；仅 User Storage Object Core 在事务内更新。",
  },
  {
    name: "storage_objects",
    title: "私有存储对象",
    domain: "space",
    purpose: "登记所有者、用途、大小、hash、状态与乐观锁；不向业务层暴露 provider key 或宿主路径。",
    fields: ["id", "owner_user_id", "namespace", "purpose", "size_bytes", "sha256", "status", "version"],
    migration: "000045",
    relationshipNote: "owner_user_id 外键保护；provider + storage_key 唯一，ready 对象必须有完整 payload。",
  },
  {
    name: "user_storage_reservations",
    title: "对象写入预留",
    domain: "space",
    purpose: "记录 pending 对象的容量预留、有效期和最终状态，用于故障对账而非公开文件索引。",
    fields: ["id", "user_id", "object_id", "reserved_bytes", "status", "expires_at"],
    migration: "000045",
    relationshipNote: "每个对象至多一个 Reservation；过期或异常状态由显式对账任务处理。",
  },
  {
    name: "user_schedule_terms",
    title: "用户课表学期引用",
    domain: "space",
    purpose: "登记用户/受管学期、最近成功的不可变课表对象和复制后的第一周，保护 AcademicTerm 不能在已有课表数据后被删除。",
    fields: ["user_id", "academic_term_id", "current_object_id", "first_week_start", "version"],
    migration: "000046 + 000049",
    relationshipNote: "(user_id, academic_term_id) 联合主键；current_object_id 只指向成功登记的私有对象，旧 JSON 保持兼容读取。",
  },
  {
    name: "user_schedule_preferences",
    title: "用户课表查看偏好",
    domain: "space",
    purpose: "保存用户当前查看的受管学期；历史 index.json 采用后仅写入此受限引用。",
    fields: ["user_id", "academic_term_id", "updated_at"],
    migration: "000049",
    relationshipNote: "每用户一行；academic_term_id 受 RESTRICT 外键保护，不能指向任意 JSON 或文件路径。",
  },
  {
    name: "personal_documents",
    title: "私有个人文档",
    domain: "space",
    purpose: "保存文档元数据、当前不可变版本指针、回收站状态与乐观锁，不保存正文或宿主文件路径。",
    fields: ["id", "owner_user_id", "name", "document_type", "current_version_id", "status", "version"],
    migration: "000047",
    relationshipNote: "用户只能访问自己的文档；回收站是软状态，历史对象和版本不会自动删除。",
  },
  {
    name: "personal_document_versions",
    title: "个人文档不可变版本",
    domain: "space",
    purpose: "每次保存或恢复都追加一条版本，引用私有 storage object 并保留 hash、大小和来源类型。",
    fields: ["id", "document_id", "version_number", "source_object_id", "source_type", "sha256", "restored_from_version_id"],
    migration: "000047",
    relationshipNote: "(document_id, version_number) 唯一；恢复历史版本会新建版本而不回写旧行。",
  },
  {
    name: "personal_document_previews",
    title: "个人文档预览任务",
    domain: "space",
    purpose: "预留隔离 Converter 的异步预览状态与输出对象引用；v0.14-dev 默认不启用 DOCX/PDF Converter。",
    fields: ["id", "document_version_id", "preview_object_id", "status", "error_code", "attempts"],
    migration: "000047",
    relationshipNote: "仅保存有界状态和错误类别，不保存文档内容或下载 Token。",
  },
  {
    name: "user_space_contents",
    title: "主页同步内容",
    domain: "space",
    purpose: "缓存同步到个人主页的主题摘要和展示信息。",
    fields: ["user_id", "thread_id", "category_id", "synced_at"],
    migration: "000008 + 000016",
    relationshipNote:
      "用户、主题和版块均由外键保护；thread_id 在当前表中唯一。",
  },
  {
    name: "user_space_style_snapshots",
    title: "风格快照",
    domain: "space",
    purpose: "在应用风格前保存可回滚的个人主页样式状态。",
    fields: ["user_id", "snapshot_type", "style_name", "style_manifest"],
    migration: "000011",
    relationshipNote: "按 user_id 保存历史快照。",
  },
  {
    name: "richtext_article_contents",
    title: "富文本正文",
    domain: "space",
    purpose: "保存受控富文本文章 HTML/JSON、封面和发布状态。",
    fields: ["thread_id", "created_by", "status", "content_html"],
    migration: "000013 + 000016",
    relationshipNote: "thread_id、created_by 和 updated_by 由外键保护。",
  },
  {
    name: "richtext_article_assets",
    title: "富文本资源",
    domain: "space",
    purpose: "保存图片文件元数据，实际文件位于用户个人空间。",
    fields: ["thread_id", "article_content_id", "uploader_id", "file_url"],
    migration: "000013 + 000016",
    relationshipNote: "主题、富文本正文和上传用户均由外键保护。",
  },
  {
    name: "webhook_endpoints",
    title: "Webhook 端点",
    domain: "integration",
    purpose: "保存外部订阅地址、签名密钥、事件和重试设置。",
    fields: ["name", "url", "events", "enabled", "max_concurrent", "rate_limit_per_minute"],
    migration: "000011 + 000027",
    relationshipNote: "一个端点可产生多条投递记录；生产默认拒绝私网和未复核重定向。",
  },
  {
    name: "webhook_deliveries",
    title: "Webhook 投递",
    domain: "integration",
    purpose: "记录投递状态、重试次数、响应状态和错误信息。",
    fields: ["endpoint_id", "event_type", "status", "attempts", "outbox_event_id", "delivery_key"],
    migration: "000011 + 000016 + 000027",
    relationshipNote: "endpoint_id 和 outbox_event_id 由外键保护；delivery_key 约束同一端点和事件的记录更新。",
  },
  {
    name: "mcp_audit_logs",
    title: "MCP 调用审计",
    domain: "integration",
    purpose: "记录内部 MCP-like 工具调用、参数和结果。",
    fields: ["user_id", "tool", "arguments", "success"],
    migration: "000011",
    relationshipNote: "user_id 为调用主体标识，当前以字符串保存。",
  },
  {
    name: "message_bindings",
    title: "消息账号绑定",
    domain: "integration",
    purpose: "绑定 CampusOS 用户与外部平台账号。",
    fields: ["user_id", "platform", "external_user_id"],
    migration: "000011",
    relationshipNote: "user_id 为 CampusOS 用户标识，当前以字符串保存。",
  },
  {
    name: "message_logs",
    title: "消息日志",
    domain: "integration",
    purpose: "记录 local adapter 或未来外部适配器的消息方向和原始负载。",
    fields: ["platform", "conversation_id", "sender_id", "direction"],
    migration: "000011",
    relationshipNote: "按平台和会话索引，不直接强制关联用户。",
  },
];

const relations: Relation[] = [
  {
    id: "users-admin-accounts",
    source: "users",
    target: "identity_admin_accounts",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "accounts-admin-accounts",
    source: "accounts",
    target: "identity_admin_accounts",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> credential_account_id",
    domains: ["identity"],
  },
  {
    id: "users-mfa-totp-methods",
    source: "users",
    target: "identity_mfa_totp_methods",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "users-mfa-tickets",
    source: "users",
    target: "identity_mfa_tickets",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "users-mfa-recovery-codes",
    source: "users",
    target: "identity_mfa_recovery_codes",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "mfa-methods-recovery-codes",
    source: "identity_mfa_totp_methods",
    target: "identity_mfa_recovery_codes",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> method_id",
    domains: ["identity"],
  },
  {
    id: "users-mfa-policies",
    source: "users",
    target: "identity_mfa_policies",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> updated_by (optional)",
    domains: ["identity"],
  },
  {
    id: "users-accounts",
    source: "users",
    target: "accounts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "users-legacy-email-placeholders",
    source: "users",
    target: "identity_legacy_email_placeholders",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "accounts-email-challenges",
    source: "accounts",
    target: "identity_email_challenges",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> account_id (optional)",
    domains: ["identity"],
  },
  {
    id: "users-challenge-policies",
    source: "users",
    target: "identity_challenge_policies",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> updated_by (optional)",
    domains: ["identity"],
  },
  {
    id: "users-academic-terms",
    source: "users",
    target: "academic_terms",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> created_by / updated_by (optional)",
    domains: ["identity", "space"],
  },
  {
    id: "users-storage-accounts",
    source: "users",
    target: "user_storage_accounts",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "users-storage-objects",
    source: "users",
    target: "storage_objects",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> owner_user_id",
    domains: ["identity", "space"],
  },
  {
    id: "storage-objects-reservations",
    source: "storage_objects",
    target: "user_storage_reservations",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> object_id",
    domains: ["space"],
  },
  {
    id: "users-schedule-terms",
    source: "users",
    target: "user_schedule_terms",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "academic-terms-schedule-terms",
    source: "academic_terms",
    target: "user_schedule_terms",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> academic_term_id",
    domains: ["space"],
  },
  {
    id: "storage-objects-schedule-terms",
    source: "storage_objects",
    target: "user_schedule_terms",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> current_object_id (optional)",
    domains: ["space"],
  },
  {
    id: "users-schedule-preferences",
    source: "users",
    target: "user_schedule_preferences",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "academic-terms-schedule-preferences",
    source: "academic_terms",
    target: "user_schedule_preferences",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> academic_term_id",
    domains: ["space"],
  },
  {
    id: "users-personal-documents",
    source: "users",
    target: "personal_documents",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> owner_user_id",
    domains: ["identity", "space"],
  },
  {
    id: "documents-document-versions",
    source: "personal_documents",
    target: "personal_document_versions",
    sourceCardinality: "1",
    targetCardinality: "1..N",
    label: "id -> document_id / current_version_id",
    domains: ["space"],
  },
  {
    id: "storage-objects-document-versions",
    source: "storage_objects",
    target: "personal_document_versions",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> source_object_id",
    domains: ["space"],
  },
  {
    id: "document-versions-previews",
    source: "personal_document_versions",
    target: "personal_document_previews",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> document_version_id",
    domains: ["space"],
  },
  {
    id: "storage-objects-document-previews",
    source: "storage_objects",
    target: "personal_document_previews",
    sourceCardinality: "1",
    targetCardinality: "0..N",
    label: "id -> preview_object_id (optional)",
    domains: ["space"],
  },
  {
    id: "users-recovery-cases",
    source: "users",
    target: "identity_account_recovery_cases",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id / created_by",
    domains: ["identity"],
  },
  {
    id: "accounts-recovery-cases",
    source: "accounts",
    target: "identity_account_recovery_cases",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> account_id",
    domains: ["identity"],
  },
  {
    id: "challenges-recovery-cases",
    source: "identity_email_challenges",
    target: "identity_account_recovery_cases",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> challenge_id",
    domains: ["identity"],
  },
  {
    id: "users-sessions",
    source: "users",
    target: "sessions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "users-user-roles",
    source: "users",
    target: "user_roles",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity"],
  },
  {
    id: "roles-user-roles",
    source: "roles",
    target: "user_roles",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> role_id",
    domains: ["identity"],
  },
  {
    id: "categories-user-roles",
    source: "categories",
    target: "user_roles",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> category scope_id",
    domains: ["identity", "community"],
  },
  {
    id: "roles-permissions",
    source: "roles",
    target: "permissions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> role_id",
    domains: ["identity"],
  },
  {
    id: "roles-role-permissions",
    source: "roles",
    target: "role_permissions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> role_id",
    domains: ["identity"],
  },
  {
    id: "role-permissions-definitions",
    source: "role_permissions",
    target: "permission_definitions",
    sourceCardinality: "N",
    targetCardinality: "1",
    label: "permission_id -> id",
    domains: ["identity"],
  },
  {
    id: "route-operations-bindings",
    source: "route_operations",
    target: "route_permission_bindings",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> route_operation_id",
    domains: ["identity"],
  },
  {
    id: "route-bindings-definitions",
    source: "route_permission_bindings",
    target: "permission_definitions",
    sourceCardinality: "N",
    targetCardinality: "1",
    label: "permission_id -> id",
    domains: ["identity"],
  },
  {
    id: "users-authorization-audits",
    source: "users",
    target: "authorization_audits",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> actor_id",
    domains: ["identity"],
  },
  {
    id: "outbox-receipts",
    source: "platform_outbox",
    target: "outbox_consumer_receipts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> event_id",
    domains: ["system"],
  },
  {
    id: "outbox-attempts",
    source: "platform_outbox",
    target: "platform_outbox_attempts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> event_id",
    domains: ["system"],
  },
  {
    id: "outbox-command-audits",
    source: "platform_outbox",
    target: "platform_command_audits",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> event_id",
    domains: ["system"],
  },
  {
    id: "categories-parent",
    source: "categories",
    target: "categories",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "group.id -> board.parent_id",
    domains: ["community"],
  },
  {
    id: "categories-threads",
    source: "categories",
    target: "threads",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> category_id",
    domains: ["community"],
  },
  {
    id: "categories-thread-type-policies",
    source: "categories",
    target: "category_thread_type_policies",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> category_id (board)",
    domains: ["community"],
  },
  {
    id: "users-threads",
    source: "users",
    target: "threads",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> author_id",
    domains: ["identity", "community"],
  },
  {
    id: "threads-content-revisions",
    source: "threads",
    target: "content_revisions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "threads-moderation-cases",
    source: "threads",
    target: "content_moderation_cases",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "threads-moderation-actions",
    source: "threads",
    target: "content_moderation_actions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "moderation-case-actions",
    source: "content_moderation_cases",
    target: "content_moderation_actions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> case_id",
    domains: ["community"],
  },
  {
    id: "threads-posts",
    source: "threads",
    target: "posts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "posts-parent",
    source: "posts",
    target: "posts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> parent_id",
    domains: ["community"],
  },
  {
    id: "users-posts",
    source: "users",
    target: "posts",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> author_id",
    domains: ["identity", "community"],
  },
  {
    id: "users-likes",
    source: "users",
    target: "likes",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "community"],
  },
  {
    id: "users-notifications",
    source: "users",
    target: "notifications",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "community"],
  },
  {
    id: "users-audit",
    source: "users",
    target: "audit_logs",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> actor_id",
    domains: ["identity", "community"],
  },
  {
    id: "users-spaces",
    source: "users",
    target: "user_spaces",
    sourceCardinality: "1",
    targetCardinality: "1",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "users-storage-quotas",
    source: "users",
    target: "user_storage_quotas",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "threads-space-contents",
    source: "threads",
    target: "user_space_contents",
    sourceCardinality: "1",
    targetCardinality: "1",
    label: "id -> thread_id",
    domains: ["community", "space"],
  },
  {
    id: "users-space-contents",
    source: "users",
    target: "user_space_contents",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "categories-space-contents",
    source: "categories",
    target: "user_space_contents",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> category_id",
    domains: ["community", "space"],
  },
  {
    id: "users-space-snapshots",
    source: "users",
    target: "user_space_style_snapshots",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "space"],
  },
  {
    id: "threads-richtext",
    source: "threads",
    target: "richtext_article_contents",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> thread_id",
    domains: ["community", "space"],
  },
  {
    id: "threads-mutual-aid",
    source: "threads",
    target: "mutual_aid_details",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "users-mutual-aid",
    source: "users",
    target: "mutual_aid_details",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> created_by",
    domains: ["identity", "community"],
  },
  {
    id: "threads-secondhand",
    source: "threads",
    target: "secondhand_details",
    sourceCardinality: "1",
    targetCardinality: "0..1",
    label: "id -> thread_id",
    domains: ["community"],
  },
  {
    id: "users-secondhand",
    source: "users",
    target: "secondhand_details",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> created_by",
    domains: ["identity", "community"],
  },
  {
    id: "users-richtext",
    source: "users",
    target: "richtext_article_contents",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> created_by",
    domains: ["identity", "space"],
  },
  {
    id: "richtext-assets",
    source: "richtext_article_contents",
    target: "richtext_article_assets",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> article_content_id",
    domains: ["space"],
  },
  {
    id: "threads-richtext-assets",
    source: "threads",
    target: "richtext_article_assets",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> thread_id",
    domains: ["community", "space"],
  },
  {
    id: "users-richtext-assets",
    source: "users",
    target: "richtext_article_assets",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> uploader_id",
    domains: ["identity", "space"],
  },
  {
    id: "plugins-permissions",
    source: "plugins",
    target: "plugin_permissions",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-logs",
    source: "plugins",
    target: "plugin_logs",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-records",
    source: "plugins",
    target: "plugin_records",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-files",
    source: "plugins",
    target: "plugin_file_metadata",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-grants",
    source: "plugins",
    target: "plugin_user_grants",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "users-grants",
    source: "users",
    target: "plugin_user_grants",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical id -> user_id",
    domains: ["identity", "plugin"],
  },
  {
    id: "plugins-catalog",
    source: "plugins",
    target: "plugin_catalog_entries",
    sourceCardinality: "1",
    targetCardinality: "1",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-releases",
    source: "plugins",
    target: "plugin_releases",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "plugins-market-audits",
    source: "plugins",
    target: "plugin_market_audits",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "logical name -> plugin_name",
    domains: ["plugin"],
  },
  {
    id: "webhook-deliveries",
    source: "webhook_endpoints",
    target: "webhook_deliveries",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> endpoint_id",
    domains: ["integration"],
  },
  {
    id: "outbox-webhook-deliveries",
    source: "platform_outbox",
    target: "webhook_deliveries",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> outbox_event_id",
    domains: ["system", "integration"],
  },
  {
    id: "users-bindings",
    source: "users",
    target: "message_bindings",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "integration"],
  },
  {
    id: "users-api-keys",
    source: "users",
    target: "api_keys",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "id -> user_id",
    domains: ["identity", "plugin"],
  },
  {
    id: "plugins-api-keys",
    source: "plugins",
    target: "api_keys",
    sourceCardinality: "1",
    targetCardinality: "N",
    label: "name -> plugin_name",
    domains: ["plugin"],
  },
];

const storageRows = [
  {
    path: "PostgreSQL",
    category: "关系数据",
    type: "primary",
    purpose: "系统主数据和需要查询、筛选、审计的元数据。",
    contents: [
      "用户、登录凭据、管理员准入账号、会话、角色与权限",
      "版块、主题、回复、标签、通知和审计",
      "插件元数据、Webhook、Message、AI 调用与样式快照",
    ],
    note: "由 migrations/ 和 schema_migrations 管理版本。",
  },
  {
    path: "data/personal-space/<user_id>/",
    category: "用户文件",
    type: "success",
    purpose: "每个用户拥有的本地文件空间，默认 50 MB，可由管理员按用户授权。",
    contents: [
      "img/avatars/：头像源文件，默认保留最近 3 个并可切换，只有新上传才按 FIFO 清理",
      "img/content/：普通帖子、校园互助和二手正文图片；仅当前上传者可在“我的文档 → 已上传资源”中查看清单，文件仍可能被公开帖子引用",
      "img/richtext/：图文文章图片；JPEG/PNG 优化后计入配额",
      "file/schedule/terms/<year>-<semester>.json：每学期课表",
      "plugins/<plugin>/：v2 插件受控附件",
      "file/、excel/、word/、pdf/：按用途/后缀分类的文件",
    ],
    note: "数据库只保存 URL 或元数据；“已上传资源”是只读库存，不会把兼容图片迁入私有文档版本，也不会提供删除，以免破坏已发布内容；恢复时必须与数据库同时恢复。",
  },
  {
    path: "modules/ + internal/modules/",
    category: "编译期模块",
    type: "primary",
    purpose: "Core/Built-in Feature 描述符与 Go 实现；不进入插件安装流程。",
    contents: [
      "modules/core、modules/features：campusos.module/v1 描述符",
      "internal/modules/core、internal/modules/features：编译期实现",
      "internal/platform/observability：有界指标、Admin 摘要与可选 loopback Prometheus 导出",
    ],
    note: "模块随主程序构建；Core 不可停用，Built-in Feature 由 /features 管理。",
  },
  {
    path: "data/plugins/<plugin>/",
    category: "外部插件实现",
    type: "warning",
    purpose: "可独立安装的 External Plugin manifest、运行入口和实现代码。",
    contents: [
      "plugin.yaml",
      "Wasm/受管进程 runtime 文件",
      "插件 README 与随代码部署的静态输入",
    ],
    note: "禁止放入 Built-in Feature、模块数据或风格包；不要把运行数据写入此目录。",
  },
  {
    path: "data/plugin_data/<plugin>/",
    category: "外部插件数据",
    type: "warning",
    purpose: "External Plugin 的 v1 KV、私有运行数据和版本快照。",
    contents: [
      "SQLite-backed v1 插件 KV",
      "version-snapshots/",
      "插件私有缓存和可恢复运行状态",
    ],
    note: "v2 结构化记录进入 PostgreSQL，v2 用户附件进入个人空间；本目录仍应与 data/plugins 分开备份。",
  },
  {
    path: "data/module_data/<feature>/",
    category: "内置功能数据",
    type: "primary",
    purpose: "Built-in Feature 拥有的本地可变数据。",
    contents: [
      "personal-space/styles/：内置个人主页 JSON 风格",
      "后续 Feature 的本地索引或可恢复状态",
    ],
    note: "不由 Plugin Manager 打包或删除；功能停用必须保留数据。",
  },
  {
    path: "data/resources/<kind>/",
    category: "资源包",
    type: "success",
    purpose:
      "无业务 Runtime 的主题、首页包、个人主页风格包、Skills、Prompt、Persona 与知识元数据。",
    contents: [
      "themes/、homepage-packs/、space-style-packs/",
      "skills/、prompts/、personas/、knowledge-metadata/",
      "校验后的资源与 resource.json",
    ],
    note: "资源包不能包含 plugin.yaml、go.mod、后台进程或数据库迁移；入口、路径与 checksum 必须通过校验。",
  },
  {
    path: "data/images/、data/config/、data/dist/、data/skills/",
    category: "系统本地数据",
    type: "info",
    purpose:
      "全局非用户图片、本地配置、构建/发布产物和本地 runtime skills 的预留边界。",
    contents: [
      "images/：非个人空间全局图片",
      "config/：本地配置，不提交密钥",
      "dist/：本地发布或构建产物",
      "skills/：本地 runtime/imported skills",
    ],
    note: "实际启用路径可能受 .env 中的目录变量覆盖。",
  },
  {
    path: ".campusos/logs/",
    category: "开发期日志",
    type: "info",
    purpose: "原生和 Docker 开发启动脚本写入 API、Web、Admin 和 Docs 的本地输出。",
    contents: ["api.log", "web.log", "admin.log", "docs.log"],
    note: "Docker stdout 通过 tee 同步到固定来源供管理端实时 follow；这不是集中日志系统。",
  },
];

const migrations = [
  {
    version: "000001",
    file: "000001_init_schema.up.sql",
    title: "核心身份、社区与系统表",
    scope: "核心",
    summary: "建立用户、认证、社区内容、通知、审计和配置基础。",
    tables: [
      "users",
      "accounts",
      "sessions",
      "categories",
      "threads",
      "posts",
      "tags",
      "likes",
      "audit_logs",
      "notifications",
      "configurations",
    ],
  },
  {
    version: "000002",
    file: "000002_add_roles.up.sql",
    title: "RBAC 角色权限",
    scope: "身份",
    summary: "建立角色、用户角色关联和角色权限，并写入系统角色种子。",
    tables: ["roles", "user_roles", "permissions"],
  },
  {
    version: "000003 / 000005",
    file: "add_plugins + schema_alignment",
    title: "插件持久化与日志",
    scope: "插件",
    summary: "建立插件元数据、API Key、权限声明和插件运行日志。",
    tables: ["plugins", "api_keys", "plugin_permissions", "plugin_logs"],
  },
  {
    version: "000004",
    file: "000004_seed_admin.up.sql",
    title: "默认管理员与版块种子",
    scope: "种子数据",
    summary: "写入默认管理员账号、管理员角色和默认版块，不新增数据表。",
    tables: ["users", "accounts", "user_roles", "categories"],
  },
  {
    version: "000006",
    file: "000006_add_ai_call_logs.up.sql",
    title: "AI Gateway 调用日志",
    scope: "集成",
    summary: "记录 provider、模型、token、耗时和错误。",
    tables: ["ai_call_logs"],
  },
  {
    version: "000007 - 000009",
    file: "add_user_spaces + contents + styles",
    title: "个人主页与同步内容",
    scope: "个人空间",
    summary: "建立个人主页配置、主题同步内容，并给主页增加风格状态。",
    tables: ["user_spaces", "user_space_contents"],
  },
  {
    version: "000010",
    file: "000010_fix_admin_seed_password.up.sql",
    title: "默认管理员密码修正",
    scope: "种子数据",
    summary: "修正默认管理员密码哈希，不新增数据表。",
    tables: ["accounts"],
  },
  {
    version: "000011",
    file: "000011_v05_operational_features.up.sql",
    title: "运营化与低风险集成",
    scope: "v0.5",
    summary: "增加主页风格快照、Webhook、MCP 审计、消息绑定和消息日志。",
    tables: [
      "user_space_style_snapshots",
      "webhook_endpoints",
      "webhook_deliveries",
      "mcp_audit_logs",
      "message_bindings",
      "message_logs",
    ],
  },
  {
    version: "000012",
    file: "000012_category_default_tags.up.sql",
    title: "版块默认标签",
    scope: "社区",
    summary: "为 categories 增加 default_tags 数组字段和索引。",
    tables: ["categories"],
  },
  {
    version: "000013",
    file: "000013_controlled_richtext_article.up.sql",
    title: "受控富文本图文文章",
    scope: "内容",
    summary: "将富文本正文和图片元数据关联到既有 threads。",
    tables: ["richtext_article_contents", "richtext_article_assets"],
  },
  {
    version: "000014",
    file: "000014_role_assignment_permissions.up.sql",
    title: "角色分配修复与细粒度权限",
    scope: "身份",
    summary:
      "修复 user_roles 全局角色唯一性，分离角色读取、分配和撤销权限，不新增数据表。",
    tables: ["user_roles", "permissions"],
  },
  {
    version: "000015",
    file: "000015_category_moderation_scope.up.sql",
    title: "版块版主作用域",
    scope: "身份与治理",
    summary:
      "停用历史全局版主授权，约束 global/category 作用域形状，并补充主题锁定权限。",
    tables: ["user_roles", "permissions"],
  },
  {
    version: "000016",
    file: "000016_v06_core_integrity.up.sql",
    title: "核心数据完整性",
    scope: "数据库",
    summary: "在数据预检后增加核心状态与计数检查、稳定关系外键和关键查询索引。",
    tables: [
      "users",
      "accounts",
      "sessions",
      "categories",
      "threads",
      "posts",
      "user_roles",
      "permissions",
      "user_spaces",
      "user_space_contents",
      "richtext_article_contents",
      "richtext_article_assets",
      "webhook_deliveries",
    ],
  },
  {
    version: "000017",
    file: "000017_v06_admin_permission_split.up.sql",
    title: "管理权限细分",
    scope: "身份与治理",
    summary:
      "将插件、富文本、集成、空间、日志和首页管理从粗粒度角色权限拆分为资源动作权限。",
    tables: ["permissions"],
  },
  {
    version: "000018",
    file: "000018_plugin_ui_runtime.up.sql",
    title: "插件 UI Runtime 状态",
    scope: "插件",
    summary:
      "持久化 BackendState、FrontendState、Health 和 UI revision，并用 CHECK 约束状态集合。",
    tables: ["plugins"],
  },
  {
    version: "000019",
    file: "000019_builtin_feature_state.up.sql",
    title: "内置功能状态",
    scope: "模块化单体",
    summary:
      "建立 Built-in Feature 独立状态表，并从历史 builtin plugin 状态一次性初始化。",
    tables: ["builtin_feature_states"],
  },
  {
    version: "000020",
    file: "000020_builtin_feature_config.up.sql",
    title: "内置功能配置",
    scope: "模块化单体",
    summary:
      "为 Built-in Feature 状态表增加权威 JSON 配置，旧插件配置只在首次缺失时导入。",
    tables: ["builtin_feature_states"],
  },
  {
    version: "000021",
    file: "000021_v09_plugin_market.up.sql",
    title: "v9 受管插件市场数据",
    scope: "插件平台",
    summary:
      "建立受管记录、文件元数据、用户 Grant、本地目录、申请、发布记录和市场审计。",
    tables: [
      "plugin_records",
      "plugin_file_metadata",
      "plugin_user_grants",
      "plugin_catalog_entries",
      "plugin_install_requests",
      "plugin_releases",
      "plugin_market_audits",
    ],
  },
  {
    version: "000022",
    file: "000022_v09_plugin_catalog_permissions.up.sql",
    title: "目录用户权限说明",
    scope: "插件平台",
    summary: "为本地目录增加用户权限的用途、风险和可撤销声明。",
    tables: ["plugin_catalog_entries"],
  },
  {
    version: "000023",
    file: "000023_v10_content_governance.up.sql",
    title: "v10 内容治理状态机",
    scope: "内容治理",
    summary:
      "为 threads 追加发布、治理和删除维度，并建立内容修订、审核案例和治理动作记录。",
    tables: [
      "threads",
      "content_revisions",
      "content_moderation_cases",
      "content_moderation_actions",
      "richtext_article_contents",
    ],
  },
  {
    version: "000024",
    file: "000024_v10_plugin_catalog_experience.up.sql",
    title: "v10 插件用户体验元数据",
    scope: "插件平台",
    summary:
      "为本地插件目录增加普通用户可读的用途、数据、风险、关闭后行为和维护者信息。",
    tables: ["plugin_catalog_entries"],
  },
  {
    version: "000025",
    file: "000025_v10_authorization_catalog.up.sql",
    title: "v10 稳定权限与路由目录",
    scope: "身份与治理",
    summary:
      "建立权限定义、角色权限、路由操作、路由权限绑定和结构化授权审计，保留旧 permissions 兼容。",
    tables: [
      "permission_definitions",
      "role_permissions",
      "route_operations",
      "route_permission_bindings",
      "authorization_audits",
    ],
  },
  {
    version: "000026",
    file: "000026_v10_module_plugin_separation.up.sql",
    title: "v10 模块、插件与资源分离",
    scope: "模块化单体与插件平台",
    summary:
      "将历史 Built-in 状态和配置迁入 Feature Store，合并 Appearance，软删除外部插件目录中的历史 Built-in 活跃行，并补充 Feature 权限。",
    tables: [
      "builtin_feature_states",
      "plugins",
      "permissions",
      "permission_definitions",
      "role_permissions",
    ],
  },
  {
    version: "000027",
    file: "000027_v11_reliable_commands_and_outbox.up.sql",
    title: "v11 可靠命令与持久事件",
    scope: "平台可靠性与 Webhook",
    summary:
      "建立事务性 Outbox、消费凭证、尝试记录、Worker 心跳、可恢复操作、兼容遥测和 dry-run 保留记录，并收紧 Webhook 投递元数据。",
    tables: [
      "platform_outbox",
      "outbox_consumer_receipts",
      "platform_outbox_attempts",
      "platform_command_audits",
      "platform_worker_leases",
      "platform_operation_runs",
      "platform_compatibility_usage",
      "platform_retention_runs",
      "webhook_endpoints",
      "webhook_deliveries",
      "authorization_audits",
      "permission_definitions",
      "role_permissions",
    ],
  },
  {
    version: "000028",
    file: "000028_v12_identity_account_state.up.sql",
    title: "v12 邮箱身份事实与历史账号状态",
    scope: "身份与账号安全",
    summary:
      "将邮箱登录事实收敛到 accounts 的规范化标识和验证状态，保留 users.email 兼容投影，并建立历史共享邮箱占位标记和保留标识策略。",
    tables: [
      "users",
      "accounts",
      "identity_legacy_email_placeholders",
      "identity_reserved_identifiers",
    ],
  },
  {
    version: "000029",
    file: "000029_v12_identity_challenges.up.sql",
    title: "v12 邮箱 Challenge、Ticket 与持久限流",
    scope: "身份与账号安全",
    summary:
      "建立 HMAC 验证码重建元数据、一次性 Ticket 摘要和基于 keyed digest 的持久限流窗口；验证码和原始 Ticket 不入库。",
    tables: ["identity_email_challenges", "identity_challenge_rate_limits"],
  },
  {
    version: "000030",
    file: "000030_v12_identity_sessions.up.sql",
    title: "v12 会话权威与 Refresh 轮换",
    scope: "身份与账号安全",
    summary:
      "清除历史原始 Refresh Token，新增摘要、家族、轮换、撤销和 IP 摘要字段；旧会话显式失效。",
    tables: ["sessions"],
  },
  {
    version: "000031",
    file: "000031_v12_identity_recovery_cases.up.sql",
    title: "v12 账号恢复 Case 与细粒度权限",
    scope: "身份与账号安全",
    summary:
      "建立管理员辅助恢复工作流、关联约束和恢复/会话/邮件投递权限；敏感凭据不入库。",
    tables: ["identity_account_recovery_cases", "permission_definitions", "role_permissions"],
  },
  {
    version: "000032",
    file: "000032_v12_category_hierarchy.up.sql",
    title: "v12 两级板块与可靠管理",
    scope: "社区与权限",
    summary:
      "在既有 categories 表追加 group/board、active/archived、版本与颜色约束，并以触发器保护两级层级和活动父级规则；同时追加细粒度板块管理权限。",
    tables: ["categories", "permission_definitions", "role_permissions"],
  },
  {
    version: "000033",
    file: "000033_v12_structured_threads.up.sql",
    title: "v12 结构化帖子类型与板块策略",
    scope: "社区与内容",
    summary:
      "为 threads 追加固定业务类型，回填历史 RichText article，并建立 board 类型策略、约束、触发器与配置权限。",
    tables: ["threads", "category_thread_type_policies", "richtext_article_contents", "permission_definitions", "role_permissions"],
  },
  {
    version: "000034",
    file: "000034_v12_mutual_aid.up.sql",
    title: "v12 校园互助结构化详情",
    scope: "社区与内容",
    summary:
      "建立校园互助业务详情、状态/联系方式/位置约束、作者一致性触发器和查询索引；Community 仍拥有主题治理状态。",
    tables: ["mutual_aid_details", "threads", "users"],
  },
  {
    version: "000035",
    file: "000035_v12_secondhand.up.sql",
    title: "v12 校园二手结构化详情",
    scope: "社区与内容",
    summary:
      "建立校园二手的 CNY 分价、物品状态、交付方式、交易状态约束、作者一致性触发器和查询索引；Community 仍拥有主题治理状态。",
    tables: ["secondhand_details", "threads", "users"],
  },
  {
    version: "000036",
    file: "000036_v12_identity_challenge_policy.up.sql",
    title: "v12 验证码频率策略",
    scope: "身份与账号安全",
    summary:
      "将邮箱和 IP 请求限制改为有边界、可审计、可热更新的滑动窗口策略，同时保留旧计数 Scope 兼容读取。",
    tables: ["identity_challenge_policies", "identity_challenge_rate_limits", "permission_definitions", "role_permissions"],
  },
  {
    version: "000037",
    file: "000037_v12_outbox_worker_convergence.up.sql",
    title: "v12 可靠事件 Worker 收敛",
    scope: "平台可靠性",
    summary:
      "为系统最终化阶段增加 failed 尝试证据；Worker 与 Store 同步收紧最大领取次数，并让耗尽的非终态事件收敛到 dead。",
    tables: ["platform_outbox", "platform_outbox_attempts", "outbox_consumer_receipts"],
  },
  {
    version: "000038",
    file: "000038_v12_admin_accounts.up.sql",
    title: "v12 管理员账号与管理平面准入",
    scope: "身份与后台安全",
    summary:
      "建立独立管理员准入账号表，将管理平面准入与普通用户主体、登录凭据及 RBAC 授权分层，并以触发器同步全局 admin 角色生命周期。",
    tables: ["identity_admin_accounts", "users", "accounts", "user_roles", "roles"],
  },
  {
    version: "000039",
    file: "000039_v13_admin_admission_operations.up.sql",
    title: "v13 管理员准入运营操作",
    scope: "身份与后台安全",
    summary:
      "为独立管理员准入追加状态原因、变更操作者和时间、状态索引及暂停/恢复权限；角色同步保持 suspended 状态不会被静默恢复。",
    tables: ["identity_admin_accounts", "permission_definitions", "role_permissions"],
  },
  {
    version: "000040",
    file: "000040_v13_identity_mfa.up.sql",
    title: "v13 TOTP MFA 与受控恢复",
    scope: "身份与后台安全",
    summary:
      "为 Session 追加服务端 MFA 强度，并建立加密 TOTP 信封、单用途 Ticket 摘要、恢复码摘要和管理员 MFA 策略；不存储明文认证器材料。",
    tables: ["sessions", "identity_mfa_totp_methods", "identity_mfa_tickets", "identity_mfa_recovery_codes", "identity_mfa_policies", "permission_definitions", "role_permissions"],
  },
  {
    version: "000041",
    file: "000041_v13_schema_index_hygiene.up.sql",
    title: "v13 Schema 索引收敛",
    scope: "数据库维护",
    summary:
      "保留全部历史迁移和业务数据，通过前向迁移删除九个已被同谓词复合 B-tree 严格左前缀覆盖的窄索引，并把重复索引、重复约束和冗余前缀检测接入数据库门禁。",
    tables: ["notifications", "plugin_permissions", "plugin_records", "posts", "role_permissions", "sessions", "threads", "user_roles", "webhook_deliveries"],
  },
  {
    version: "000042",
    file: "000042_v13_user_storage_quotas.up.sql",
    title: "v13 用户空间配额授权",
    scope: "个人空间与存储",
    summary:
      "建立按用户覆盖的 User Storage 配额记录，保留 50 MB 系统默认值，并记录最近授权管理员和时间；文件仍位于 data/personal-space。",
    tables: ["user_storage_quotas", "users"],
  },
  {
    version: "000043",
    file: "000043_v13_post_parent_floor.up.sql",
    title: "v13 回复父楼层快照",
    scope: "社区回复",
    summary:
      "为 posts 增加 parent_floor_number 创建时快照并从父回复回填存量数据，父回复删除或跨分页时引用楼层显示保持稳定。",
    tables: ["posts"],
  },
  {
    version: "000044",
    file: "000044_v14_academic_terms.up.sql",
    title: "v14 管理员治理学期目录",
    scope: "个人课表与系统目录",
    summary:
      "建立 spring/fall 学期、第一周星期一约束、open/closed/default 生命周期、乐观锁与管理员管理权限；课表对象绑定将在后续 migration 追加。",
    tables: ["academic_terms", "permission_definitions", "role_permissions", "users"],
  },
  {
    version: "000045",
    file: "000045_v14_storage_objects.up.sql",
    title: "v14 私有对象存储账本",
    scope: "个人空间与文件一致性",
    summary: "建立对象元数据、账户已用/预留字节和 Reservation；Local Provider 以 staging + 原子 rename 写入。",
    tables: ["user_storage_accounts", "storage_objects", "user_storage_reservations", "users"],
  },
  {
    version: "000046",
    file: "000046_v14_schedule_term_references.up.sql",
    title: "v14 课表学期引用保护",
    scope: "个人课表",
    summary: "登记用户课表已使用的 AcademicTerm，使用 RESTRICT 外键阻止错误删除有课表数据的学期。",
    tables: ["user_schedule_terms", "academic_terms", "users"],
  },
  {
    version: "000047",
    file: "000047_v14_personal_documents.up.sql",
    title: "v14 私有文档与不可变版本",
    scope: "个人空间",
    summary: "建立个人文档、不可变版本与预览状态；源文件只通过私有 storage object 访问。",
    tables: ["personal_documents", "personal_document_versions", "personal_document_previews", "storage_objects", "users"],
  },
  {
    version: "000048",
    file: "000048_v14_storage_constraint_names.up.sql",
    title: "v14 对象账本约束名称兼容",
    scope: "数据库合同",
    summary: "仅无损统一早期开发库自动生成的对象账本 CHECK 约束名称；新空库保持 no-op，生产不执行 down。",
    tables: ["user_storage_accounts", "storage_objects", "user_storage_reservations"],
  },
  {
    version: "000049",
    file: "000049_v14_schedule_object_bindings.up.sql",
    title: "v14 课表对象绑定与查看偏好",
    scope: "个人课表与对象兼容",
    summary: "为用户课表学期引用补充当前 Object、第一周快照、版本与查看偏好；旧 JSON 保持只读兼容并由显式采用命令登记。",
    tables: ["user_schedule_terms", "user_schedule_preferences", "storage_objects", "academic_terms", "users"],
  },
];

const tableByName = (name: string) =>
  databaseTables.find((table) => table.name === name) || databaseTables[0];
const currentTable = computed(() => tableByName(selectedTable.value));

const visibleTables = computed(() => {
  if (domainFilter.value === "all") return databaseTables;
  if (domainFilter.value === "identity")
    return databaseTables.filter(
      (table) => table.domain === "identity" || table.domain === "community",
    );
  if (domainFilter.value === "space")
    return databaseTables.filter((table) => table.domain === "space");
  return databaseTables.filter(
    (table) =>
      table.domain === "plugin" ||
      table.domain === "integration" ||
      table.domain === "system",
  );
});

const visibleRelations = computed(() => {
  if (domainFilter.value === "all") return relations;
  if (domainFilter.value === "identity")
    return relations.filter((relation) =>
      relation.domains.some(
        (domain) => domain === "identity" || domain === "community",
      ),
    );
  if (domainFilter.value === "space")
    return relations.filter((relation) => relation.domains.includes("space"));
  return relations.filter((relation) =>
    relation.domains.some(
      (domain) =>
        domain === "plugin" || domain === "integration" || domain === "system",
    ),
  );
});

const domainLabel = (domain: Domain) =>
  ({
    identity: "身份",
    community: "社区",
    space: "空间与内容",
    plugin: "插件",
    integration: "集成",
    system: "系统",
  })[domain];

watch(domainFilter, () => {
  if (
    !visibleTables.value.some((table) => table.name === selectedTable.value)
  ) {
    selectedTable.value = visibleTables.value[0]?.name || "users";
  }
});
</script>

<style scoped>
.system-architecture {
  max-width: 1520px;
}
.page-header,
.toolbar-band,
.section-heading,
.inspector-heading,
.storage-card-heading,
.migration-card-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.page-header {
  margin-bottom: 16px;
  padding: 22px 24px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fff;
}
.page-header h2 {
  margin: 4px 0 8px;
  font-size: 24px;
  color: #1f2937;
}
.page-header p,
.toolbar-band p,
.section-heading p,
.inspector-heading p,
.migration-card p {
  margin: 0;
  line-height: 1.6;
  color: #606266;
}
.eyebrow {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  color: #2563eb;
}
.architecture-alert {
  margin-bottom: 16px;
}
.architecture-tabs {
  padding: 0 18px 18px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #fff;
}
.toolbar-band {
  flex-wrap: wrap;
  padding: 16px 0;
  border-bottom: 1px solid #ebeef5;
}
.toolbar-band strong {
  display: block;
  margin-bottom: 4px;
  color: #303133;
}
.database-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.7fr) minmax(300px, 0.8fr);
  gap: 16px;
  margin-top: 16px;
}
.relation-board {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-content: start;
  max-height: 720px;
  overflow: auto;
  padding: 4px;
}
.relation-lane {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) minmax(96px, 0.8fr) minmax(
      100px,
      1fr
    );
  align-items: center;
  gap: 8px;
  min-height: 114px;
  padding: 10px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fbfcfe;
}
.table-node,
.catalog-node {
  min-width: 0;
  border: 1px solid #dce5f0;
  border-radius: 6px;
  background: #fff;
  color: #1f2937;
  text-align: left;
  cursor: pointer;
  transition:
    border-color 0.18s ease,
    box-shadow 0.18s ease;
}
.table-node {
  min-height: 84px;
  padding: 10px;
}
.table-node:hover,
.table-node[aria-pressed="true"],
.catalog-node:hover,
.catalog-node.selected {
  border-color: #409eff;
  box-shadow: 0 0 0 2px rgba(64, 158, 255, 0.12);
}
.table-node strong,
.catalog-node strong {
  display: block;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.table-node small,
.catalog-node span {
  display: block;
  margin-top: 5px;
  overflow: hidden;
  color: #606266;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-domain {
  display: inline-block;
  margin-bottom: 6px;
  color: #64748b;
  font-size: 11px;
  line-height: 1;
}
.relation-link {
  display: grid;
  grid-template-columns: auto minmax(12px, 1fr) auto minmax(12px, 1fr) auto;
  align-items: center;
  gap: 4px;
  color: #64748b;
  font-size: 11px;
  text-align: center;
}
.relation-link i {
  height: 1px;
  background: #94a3b8;
}
.relation-link em {
  max-width: 78px;
  overflow: hidden;
  color: #475569;
  font-style: normal;
  line-height: 1.25;
  text-overflow: ellipsis;
}
.table-inspector {
  position: sticky;
  top: 12px;
  align-self: start;
  padding: 18px;
  border: 1px solid #dbe4ee;
  border-radius: 8px;
  background: #f8fafc;
}
.inspector-heading {
  align-items: flex-start;
}
.inspector-heading h3 {
  margin: 0 0 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 17px;
}
.table-purpose {
  margin: 14px 0;
  line-height: 1.7;
  color: #475569;
}
.field-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.field-list code,
.maintenance-grid code {
  padding: 3px 5px;
  border-radius: 4px;
  background: #eef2f7;
  color: #334155;
  font-size: 12px;
}
.catalog-section {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid #ebeef5;
}
.section-heading h3 {
  margin: 0 0 4px;
  font-size: 16px;
}
.table-catalog {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(172px, 1fr));
  gap: 10px;
  margin-top: 14px;
}
.catalog-node {
  padding: 11px;
}
.domain-identity {
  border-left: 3px solid #2563eb;
}
.domain-community {
  border-left: 3px solid #059669;
}
.domain-space {
  border-left: 3px solid #d97706;
}
.domain-plugin {
  border-left: 3px solid #7c3aed;
}
.domain-integration {
  border-left: 3px solid #db2777;
}
.domain-system {
  border-left: 3px solid #475569;
}
.data-flow {
  display: grid;
  grid-template-columns: minmax(150px, 0.8fr) minmax(80px, 0.25fr) minmax(
      180px,
      1fr
    ) minmax(80px, 0.25fr) minmax(230px, 1.4fr);
  align-items: center;
  gap: 12px;
  padding: 28px 0;
}
.flow-node {
  display: grid;
  justify-items: start;
  gap: 7px;
  min-height: 146px;
  padding: 18px;
  border: 1px solid #dbe4ee;
  border-radius: 8px;
  background: #fff;
}
.flow-node :deep(.el-icon) {
  font-size: 22px;
}
.flow-node strong {
  color: #1e293b;
}
.flow-node span {
  color: #64748b;
  font-size: 13px;
  line-height: 1.55;
}
.flow-client {
  border-top: 3px solid #2563eb;
}
.flow-api {
  border-top: 3px solid #059669;
}
.flow-postgres {
  border-left: 3px solid #7c3aed;
}
.flow-files {
  border-left: 3px solid #d97706;
}
.flow-logs {
  border-left: 3px solid #64748b;
}
.flow-arrow {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 4px;
  color: #64748b;
  font-size: 11px;
}
.flow-arrow::before {
  height: 1px;
  background: #94a3b8;
  content: "";
}
.flow-arrow::after {
  border-top: 5px solid transparent;
  border-bottom: 5px solid transparent;
  border-left: 7px solid #94a3b8;
  content: "";
}
.flow-targets {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.flow-targets .flow-node {
  min-height: 146px;
}
.flow-branch {
  align-self: stretch;
}
.flow-branch::before {
  background: repeating-linear-gradient(
    90deg,
    #94a3b8 0 8px,
    transparent 8px 12px
  );
}
.storage-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin: 10px 0 16px;
}
.storage-card {
  min-height: 238px;
  border-radius: 8px;
}
.storage-card-heading code {
  overflow: hidden;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.storage-card p {
  margin: 0;
  color: #475569;
  line-height: 1.6;
}
.storage-card ul {
  min-height: 84px;
  margin: 12px 0;
  padding-left: 18px;
  color: #606266;
  font-size: 13px;
  line-height: 1.7;
}
.storage-note {
  padding-top: 10px;
  border-top: 1px solid #ebeef5;
  color: #909399;
  font-size: 12px;
  line-height: 1.5;
}
.migration-timeline {
  margin: 22px 0 0 4px;
}
.migration-card-heading {
  align-items: flex-start;
}
.migration-card-heading code {
  margin-right: 10px;
  color: #2563eb;
}
.migration-card-heading strong {
  color: #1f2937;
}
.migration-card p {
  margin: 10px 0;
}
.migration-tables {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.maintenance-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin-top: 18px;
}
.maintenance-grid article {
  padding: 16px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #f8fafc;
}
.maintenance-grid h3 {
  margin: 0 0 10px;
  font-size: 15px;
}
.maintenance-grid p {
  margin: 12px 0 0;
  color: #606266;
  line-height: 1.6;
  font-size: 13px;
}
@media (max-width: 1200px) {
  .database-layout {
    grid-template-columns: 1fr;
  }
  .table-inspector {
    position: static;
  }
  .storage-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .data-flow {
    grid-template-columns: 1fr;
  }
  .flow-arrow {
    min-height: 34px;
    grid-template-columns: 1fr;
    grid-template-rows: 1fr auto;
    justify-items: center;
  }
  .flow-arrow::before {
    width: 1px;
    height: 18px;
  }
  .flow-arrow::after {
    border-top: 7px solid #94a3b8;
    border-right: 5px solid transparent;
    border-left: 5px solid transparent;
    border-bottom: 0;
  }
  .flow-targets {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .flow-branch {
    display: none;
  }
}
@media (max-width: 820px) {
  .page-header,
  .toolbar-band {
    align-items: flex-start;
    flex-direction: column;
  }
  .relation-board {
    grid-template-columns: 1fr;
    max-height: none;
  }
  .storage-grid,
  .maintenance-grid,
  .flow-targets {
    grid-template-columns: 1fr;
  }
  .relation-lane {
    grid-template-columns: minmax(94px, 1fr) minmax(84px, 0.7fr) minmax(
        94px,
        1fr
      );
  }
  .architecture-tabs {
    padding: 0 12px 12px;
  }
}
</style>
