<template>
  <div class="system-architecture">
    <section class="page-header">
      <div>
        <p class="eyebrow">只读架构视图</p>
        <h2>系统数据架构</h2>
        <p>从当前迁移文件和数据目录整理的结构图，用于了解表的逻辑关联、职责和不在 PostgreSQL 中保存的文件数据。</p>
      </div>
      <el-tag type="info" effect="plain">迁移 000001 - 000018</el-tag>
    </section>

    <el-alert class="architecture-alert" type="info" :closable="false" show-icon>
      PostgreSQL 已对核心身份、社区、个人空间、富文本和 Webhook 关系启用外键；其余连线表示代码与字段可追踪的逻辑关系。本页不会读取或展示真实业务记录。
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
            <article v-for="relation in visibleRelations" :key="relation.id" class="relation-lane">
              <button
                class="table-node"
                :class="`domain-${tableByName(relation.source).domain}`"
                type="button"
                :aria-pressed="selectedTable === relation.source"
                @click="selectedTable = relation.source"
              >
                <span class="node-domain">{{ domainLabel(tableByName(relation.source).domain) }}</span>
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
                <span class="node-domain">{{ domainLabel(tableByName(relation.target).domain) }}</span>
                <strong>{{ relation.target }}</strong>
                <small>{{ tableByName(relation.target).title }}</small>
              </button>
            </article>
          </section>

          <aside class="table-inspector" aria-live="polite">
            <div class="inspector-heading">
              <div>
                <span class="node-domain">{{ domainLabel(currentTable.domain) }}</span>
                <h3>{{ currentTable.name }}</h3>
                <p>{{ currentTable.title }}</p>
              </div>
              <el-tag effect="plain">{{ currentTable.migration }}</el-tag>
            </div>
            <p class="table-purpose">{{ currentTable.purpose }}</p>
            <el-descriptions :column="1" border size="small">
              <el-descriptions-item label="关键字段">
                <div class="field-list">
                  <code v-for="field in currentTable.fields" :key="field">{{ field }}</code>
                </div>
              </el-descriptions-item>
              <el-descriptions-item label="关联提示">
                {{ currentTable.relationshipNote }}
              </el-descriptions-item>
              <el-descriptions-item label="持久化位置">PostgreSQL</el-descriptions-item>
            </el-descriptions>
          </aside>
        </div>

        <section class="catalog-section">
          <div class="section-heading">
            <div>
              <h3>全部数据表</h3>
              <p>按业务域分组；选择后右侧会显示对应说明。</p>
            </div>
            <el-tag type="info" effect="plain">{{ visibleTables.length }} 张表</el-tag>
          </div>
          <div class="table-catalog">
            <button
              v-for="table in visibleTables"
              :key="table.name"
              class="catalog-node"
              :class="[`domain-${table.domain}`, { selected: selectedTable === table.name }]"
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
            <p>业务元数据进入 PostgreSQL；文件、插件运行数据和日志按目录归属保存。</p>
          </div>
          <el-tag type="warning" effect="plain">本地开发默认布局</el-tag>
        </section>

        <section class="data-flow" aria-label="应用数据流图">
          <article class="flow-node flow-client">
            <el-icon><DataAnalysis /></el-icon>
            <strong>web / admin</strong>
            <span>浏览、管理、上传和配置</span>
          </article>
          <div class="flow-arrow" aria-hidden="true"><span>HTTP / JSON</span></div>
          <article class="flow-node flow-api">
            <el-icon><Connection /></el-icon>
            <strong>CampusOS API</strong>
            <span>鉴权、业务规则、文件分类和插件 Runtime</span>
          </article>
          <div class="flow-arrow flow-branch" aria-hidden="true"><span>读写</span></div>
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
          <el-card v-for="store in storageRows" :key="store.path" shadow="never" class="storage-card">
            <template #header>
              <div class="storage-card-heading">
                <code>{{ store.path }}</code>
                <el-tag :type="store.type" size="small" effect="plain">{{ store.category }}</el-tag>
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
            <p>每一行对应仓库中的 migration 文件；本页不会绕过迁移系统直接修改数据库。</p>
          </div>
          <el-tag type="success" effect="plain">schema_migrations 记录执行状态</el-tag>
        </section>

        <el-timeline class="migration-timeline">
          <el-timeline-item v-for="migration in migrations" :key="migration.version" :timestamp="migration.file" placement="top">
            <el-card shadow="never" class="migration-card">
              <div class="migration-card-heading">
                <div>
                  <code>{{ migration.version }}</code>
                  <strong>{{ migration.title }}</strong>
                </div>
                <el-tag size="small" effect="plain">{{ migration.scope }}</el-tag>
              </div>
              <p>{{ migration.summary }}</p>
              <div class="migration-tables">
                <el-tag v-for="table in migration.tables" :key="table" size="small" effect="plain">{{ table }}</el-tag>
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
            <p>只执行尚未记录在 <code>schema_migrations</code> 中的 up migration。</p>
          </article>
          <article>
            <h3>备份边界</h3>
            <code>PostgreSQL + data/ + .env</code>
            <p>恢复时三者需要使用同一时间点的备份，尤其是用户头像、图文图片和插件文件。</p>
          </article>
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Connection, DataAnalysis, Document, FolderOpened } from '@element-plus/icons-vue'

type Domain = 'identity' | 'community' | 'space' | 'plugin' | 'integration' | 'system'

interface DbTable {
  name: string
  title: string
  domain: Domain
  purpose: string
  fields: string[]
  migration: string
  relationshipNote: string
}

interface Relation {
  id: string
  source: string
  target: string
  sourceCardinality: string
  targetCardinality: string
  label: string
  domains: Domain[]
}

const activeTab = ref('database')
const domainFilter = ref<'all' | 'identity' | 'space' | 'plugin'>('all')
const selectedTable = ref('users')

const databaseTables: DbTable[] = [
  { name: 'schema_migrations', title: '迁移执行记录', domain: 'system', purpose: '迁移脚本创建的系统表，记录每个 migration 是否已执行。', fields: ['version', 'name', 'applied_at'], migration: 'scripts/migrate.sh / migrate.ps1', relationshipNote: '只用于 schema 版本管理，不与业务表建立关系。' },
  { name: 'users', title: '用户', domain: 'identity', purpose: '系统中的用户主体，社区、个人空间、权限和上传资源的归属起点。', fields: ['id', 'username', 'email', 'status'], migration: '000001 + 000016', relationshipNote: '通过受外键约束的 user_id、author_id、created_by、uploader_id 等字段被多个业务表引用。' },
  { name: 'accounts', title: '登录凭据', domain: 'identity', purpose: '保存邮箱、手机号或 OAuth 标识及密码哈希等认证凭据。', fields: ['id', 'user_id', 'type', 'identifier'], migration: '000001 + 000016', relationshipNote: '一个用户可有多个登录账号；user_id 由外键保护。' },
  { name: 'sessions', title: '刷新会话', domain: 'identity', purpose: '保存 refresh token、设备和过期时间。', fields: ['id', 'user_id', 'refresh_token', 'expires_at'], migration: '000001 + 000016', relationshipNote: '一个用户可在多个设备保持会话；user_id 由外键保护。' },
  { name: 'roles', title: '角色', domain: 'identity', purpose: '定义 admin、moderator、member、guest 等系统角色；member 是有效用户的隐式基础角色。', fields: ['id', 'name', 'is_system'], migration: '000002', relationshipNote: 'user_roles 只保存额外授权；permissions 约束角色可以执行的操作。' },
  { name: 'user_roles', title: '用户角色', domain: 'identity', purpose: '用户与额外角色的关联；管理员使用 global 作用域，版主使用一个或多个 category 作用域。', fields: ['user_id', 'role_id', 'scope_type', 'scope_id'], migration: '000002 + 000014 - 000016', relationshipNote: 'user_id 和 role_id 由外键保护；版主 scope_id 逻辑指向 categories.id，跨版块请求由后端拒绝。' },
  { name: 'permissions', title: '角色权限', domain: 'identity', purpose: '为角色声明资源和动作权限；角色管理与各管理域使用独立 action。', fields: ['role_id', 'resource', 'action'], migration: '000002 + 000014 - 000017', relationshipNote: 'role_id 由外键保护；版主权限还必须匹配 user_roles 中的 category scope。' },
  { name: 'categories', title: '版块', domain: 'community', purpose: '社区内容分区，支持父子版块和默认标签。', fields: ['id', 'parent_id', 'name', 'default_tags'], migration: '000001 + 000012 + 000016', relationshipNote: 'parent_id、threads.category_id 和 user_space_contents.category_id 由外键保护。' },
  { name: 'threads', title: '主题 / 文章入口', domain: 'community', purpose: '普通帖子和富文本文章共用的顶层内容实体。', fields: ['id', 'author_id', 'category_id', 'status'], migration: '000001 + 000016', relationshipNote: '作者和版块由外键保护，并关联回复、个人空间同步内容和富文本正文。' },
  { name: 'posts', title: '回复', domain: 'community', purpose: '主题下的回复，可通过 parent_id 形成引用/嵌套关系。', fields: ['id', 'thread_id', 'author_id', 'parent_id', 'floor_number'], migration: '000001 + 000016', relationshipNote: 'thread_id、author_id 和 parent_id 由外键保护。' },
  { name: 'tags', title: '标签字典', domain: 'community', purpose: '保留可管理标签元数据；主题当前也以 tags 数组保存标签快照。', fields: ['id', 'name', 'slug', 'thread_count'], migration: '000001', relationshipNote: '当前没有 thread_tags 关联表，主题标签以数组字段为主。' },
  { name: 'likes', title: '点赞', domain: 'community', purpose: '记录用户对主题或回复的点赞。', fields: ['user_id', 'target_type', 'target_id'], migration: '000001', relationshipNote: 'target_type + target_id 是多态关联，可指向 threads 或 posts。' },
  { name: 'notifications', title: '站内通知', domain: 'community', purpose: '保存通知内容、已读状态和跳转地址。', fields: ['user_id', 'type', 'is_read'], migration: '000001', relationshipNote: '按 user_id 投递给一个用户。' },
  { name: 'audit_logs', title: '操作审计', domain: 'community', purpose: '记录操作人、资源、前后数据和 trace id。', fields: ['actor_id', 'action', 'resource', 'resource_id'], migration: '000001', relationshipNote: 'actor_id 逻辑指向用户；resource 为多态业务资源。' },
  { name: 'configurations', title: '系统配置', domain: 'community', purpose: '保存分组配置及其更新人。', fields: ['key', 'value', 'category', 'updated_by'], migration: '000001', relationshipNote: 'updated_by 记录逻辑上的用户归属。' },
  { name: 'api_keys', title: 'API Key', domain: 'plugin', purpose: '独立管理用户或插件使用的 API Key。', fields: ['key', 'user_id', 'plugin_name', 'permissions'], migration: '000003', relationshipNote: '可关联用户或插件名称，两者属于可选逻辑归属。' },
  { name: 'plugins', title: '插件元数据', domain: 'plugin', purpose: '保存插件 manifest、三轴运行状态、配置、checksum 和 UI revision。', fields: ['name', 'runtime', 'status', 'backend_state', 'frontend_state', 'health_state', 'ui_revision', 'manifest', 'config'], migration: '000003 + 000011 + 000018', relationshipNote: '通过 plugin_name 与插件权限、日志和 API Key 形成名称关联。' },
  { name: 'plugin_permissions', title: '插件权限', domain: 'plugin', purpose: '保存插件声明或同步后的 Host API 权限。', fields: ['plugin_name', 'permission_type', 'permission_value'], migration: '000005', relationshipNote: 'plugin_name 逻辑指向 plugins.name。' },
  { name: 'plugin_logs', title: '插件日志', domain: 'plugin', purpose: '保存插件运行、事件和 Host API 日志。', fields: ['plugin_name', 'level', 'event_type', 'trace_id'], migration: '000005', relationshipNote: 'plugin_name 逻辑指向 plugins.name。' },
  { name: 'ai_call_logs', title: 'AI 调用日志', domain: 'integration', purpose: '记录模型提供方、用量、耗时和失败原因。', fields: ['provider', 'model', 'source', 'status'], migration: '000006', relationshipNote: '按调用来源记录，不强制绑定用户或帖子。' },
  { name: 'user_spaces', title: '个人主页', domain: 'space', purpose: '保存用户公开主页、同步设置、样式和禁用状态。', fields: ['user_id', 'visibility', 'style_name', 'sync_enabled'], migration: '000007 + 000009 + 000011 + 000016', relationshipNote: '一个用户最多一份个人主页配置；user_id 由外键保护。' },
  { name: 'user_space_contents', title: '主页同步内容', domain: 'space', purpose: '缓存同步到个人主页的主题摘要和展示信息。', fields: ['user_id', 'thread_id', 'category_id', 'synced_at'], migration: '000008 + 000016', relationshipNote: '用户、主题和版块均由外键保护；thread_id 在当前表中唯一。' },
  { name: 'user_space_style_snapshots', title: '风格快照', domain: 'space', purpose: '在应用风格前保存可回滚的个人主页样式状态。', fields: ['user_id', 'snapshot_type', 'style_name', 'style_manifest'], migration: '000011', relationshipNote: '按 user_id 保存历史快照。' },
  { name: 'richtext_article_contents', title: '富文本正文', domain: 'space', purpose: '保存受控富文本文章 HTML/JSON、封面和发布状态。', fields: ['thread_id', 'created_by', 'status', 'content_html'], migration: '000013 + 000016', relationshipNote: 'thread_id、created_by 和 updated_by 由外键保护。' },
  { name: 'richtext_article_assets', title: '富文本资源', domain: 'space', purpose: '保存图片文件元数据，实际文件位于用户个人空间。', fields: ['thread_id', 'article_content_id', 'uploader_id', 'file_url'], migration: '000013 + 000016', relationshipNote: '主题、富文本正文和上传用户均由外键保护。' },
  { name: 'webhook_endpoints', title: 'Webhook 端点', domain: 'integration', purpose: '保存外部订阅地址、签名密钥、事件和重试设置。', fields: ['name', 'url', 'events', 'enabled'], migration: '000011', relationshipNote: '一个端点可产生多条投递记录。' },
  { name: 'webhook_deliveries', title: 'Webhook 投递', domain: 'integration', purpose: '记录投递状态、重试次数、响应状态和错误信息。', fields: ['endpoint_id', 'event_type', 'status', 'attempts'], migration: '000011 + 000016', relationshipNote: 'endpoint_id 由指向 webhook_endpoints.id 的外键保护。' },
  { name: 'mcp_audit_logs', title: 'MCP 调用审计', domain: 'integration', purpose: '记录内部 MCP-like 工具调用、参数和结果。', fields: ['user_id', 'tool', 'arguments', 'success'], migration: '000011', relationshipNote: 'user_id 为调用主体标识，当前以字符串保存。' },
  { name: 'message_bindings', title: '消息账号绑定', domain: 'integration', purpose: '绑定 CampusOS 用户与外部平台账号。', fields: ['user_id', 'platform', 'external_user_id'], migration: '000011', relationshipNote: 'user_id 为 CampusOS 用户标识，当前以字符串保存。' },
  { name: 'message_logs', title: '消息日志', domain: 'integration', purpose: '记录 local adapter 或未来外部适配器的消息方向和原始负载。', fields: ['platform', 'conversation_id', 'sender_id', 'direction'], migration: '000011', relationshipNote: '按平台和会话索引，不直接强制关联用户。' },
]

const relations: Relation[] = [
  { id: 'users-accounts', source: 'users', target: 'accounts', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity'] },
  { id: 'users-sessions', source: 'users', target: 'sessions', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity'] },
  { id: 'users-user-roles', source: 'users', target: 'user_roles', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity'] },
  { id: 'roles-user-roles', source: 'roles', target: 'user_roles', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> role_id', domains: ['identity'] },
  { id: 'categories-user-roles', source: 'categories', target: 'user_roles', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> category scope_id', domains: ['identity', 'community'] },
  { id: 'roles-permissions', source: 'roles', target: 'permissions', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> role_id', domains: ['identity'] },
  { id: 'categories-parent', source: 'categories', target: 'categories', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> parent_id', domains: ['community'] },
  { id: 'categories-threads', source: 'categories', target: 'threads', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> category_id', domains: ['community'] },
  { id: 'users-threads', source: 'users', target: 'threads', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> author_id', domains: ['identity', 'community'] },
  { id: 'threads-posts', source: 'threads', target: 'posts', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> thread_id', domains: ['community'] },
  { id: 'posts-parent', source: 'posts', target: 'posts', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> parent_id', domains: ['community'] },
  { id: 'users-posts', source: 'users', target: 'posts', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> author_id', domains: ['identity', 'community'] },
  { id: 'users-likes', source: 'users', target: 'likes', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'community'] },
  { id: 'users-notifications', source: 'users', target: 'notifications', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'community'] },
  { id: 'users-audit', source: 'users', target: 'audit_logs', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> actor_id', domains: ['identity', 'community'] },
  { id: 'users-spaces', source: 'users', target: 'user_spaces', sourceCardinality: '1', targetCardinality: '1', label: 'id -> user_id', domains: ['identity', 'space'] },
  { id: 'threads-space-contents', source: 'threads', target: 'user_space_contents', sourceCardinality: '1', targetCardinality: '1', label: 'id -> thread_id', domains: ['community', 'space'] },
  { id: 'users-space-contents', source: 'users', target: 'user_space_contents', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'space'] },
  { id: 'categories-space-contents', source: 'categories', target: 'user_space_contents', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> category_id', domains: ['community', 'space'] },
  { id: 'users-space-snapshots', source: 'users', target: 'user_space_style_snapshots', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'space'] },
  { id: 'threads-richtext', source: 'threads', target: 'richtext_article_contents', sourceCardinality: '1', targetCardinality: '0..1', label: 'id -> thread_id', domains: ['community', 'space'] },
  { id: 'users-richtext', source: 'users', target: 'richtext_article_contents', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> created_by', domains: ['identity', 'space'] },
  { id: 'richtext-assets', source: 'richtext_article_contents', target: 'richtext_article_assets', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> article_content_id', domains: ['space'] },
  { id: 'threads-richtext-assets', source: 'threads', target: 'richtext_article_assets', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> thread_id', domains: ['community', 'space'] },
  { id: 'users-richtext-assets', source: 'users', target: 'richtext_article_assets', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> uploader_id', domains: ['identity', 'space'] },
  { id: 'plugins-permissions', source: 'plugins', target: 'plugin_permissions', sourceCardinality: '1', targetCardinality: 'N', label: 'name -> plugin_name', domains: ['plugin'] },
  { id: 'plugins-logs', source: 'plugins', target: 'plugin_logs', sourceCardinality: '1', targetCardinality: 'N', label: 'name -> plugin_name', domains: ['plugin'] },
  { id: 'webhook-deliveries', source: 'webhook_endpoints', target: 'webhook_deliveries', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> endpoint_id', domains: ['integration'] },
  { id: 'users-bindings', source: 'users', target: 'message_bindings', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'integration'] },
  { id: 'users-api-keys', source: 'users', target: 'api_keys', sourceCardinality: '1', targetCardinality: 'N', label: 'id -> user_id', domains: ['identity', 'plugin'] },
  { id: 'plugins-api-keys', source: 'plugins', target: 'api_keys', sourceCardinality: '1', targetCardinality: 'N', label: 'name -> plugin_name', domains: ['plugin'] },
]

const storageRows = [
  { path: 'PostgreSQL', category: '关系数据', type: 'primary', purpose: '系统主数据和需要查询、筛选、审计的元数据。', contents: ['用户、账号、会话、角色与权限', '版块、主题、回复、标签、通知和审计', '插件元数据、Webhook、Message、AI 调用与样式快照'], note: '由 migrations/ 和 schema_migrations 管理版本。' },
  { path: 'data/personal-space/<user_id>/', category: '用户文件', type: 'success', purpose: '每个用户拥有的本地文件空间，默认受 personal-space 配额管理。', contents: ['img/avatars/：头像源文件，默认保留最近 3 个', 'img/richtext/：图文文章图片', 'file/schedule/terms/<year>-<semester>.json：每学期课表', 'file/、excel/、word/、pdf/：按用途/后缀分类的文件'], note: '数据库只保存 URL 或元数据；恢复时必须与数据库同时恢复。' },
  { path: 'data/plugins/<plugin>/', category: '插件实现', type: 'warning', purpose: '内置或已安装插件的 manifest、运行入口和实现代码。', contents: ['plugin.yaml', 'Wasm/gRPC runtime 文件', '插件 README 与内置静态示例'], note: '系统级插件随服务部署；不要把运行数据写入此目录。' },
  { path: 'data/plugin_data/<plugin>/', category: '插件数据', type: 'warning', purpose: '插件运行产生的 KV、可编辑风格包源码和后续生成文件。', contents: ['SQLite-backed 插件 KV', 'personal-space 与 homepage 的 style-packs', '插件私有运行数据'], note: '应与 data/plugins 分开备份和权限管理。' },
  { path: 'data/images/、data/config/、data/dist/、data/skills/', category: '系统本地数据', type: 'info', purpose: '全局非用户图片、本地配置、构建/发布产物和本地 runtime skills 的预留边界。', contents: ['images/：非个人空间全局图片', 'config/：本地配置，不提交密钥', 'dist/：本地发布或构建产物', 'skills/：本地 runtime/imported skills'], note: '实际启用路径可能受 .env 中的目录变量覆盖。' },
  { path: '.campusos/logs/', category: '开发期日志', type: 'info', purpose: '启动脚本写入 API、web 和 admin 的本地输出。', contents: ['api.log', 'web.log', 'admin.log'], note: '管理端平台日志页只读取固定来源；这不是集中日志系统。' },
]

const migrations = [
  { version: '000001', file: '000001_init_schema.up.sql', title: '核心身份、社区与系统表', scope: '核心', summary: '建立用户、认证、社区内容、通知、审计和配置基础。', tables: ['users', 'accounts', 'sessions', 'categories', 'threads', 'posts', 'tags', 'likes', 'audit_logs', 'notifications', 'configurations'] },
  { version: '000002', file: '000002_add_roles.up.sql', title: 'RBAC 角色权限', scope: '身份', summary: '建立角色、用户角色关联和角色权限，并写入系统角色种子。', tables: ['roles', 'user_roles', 'permissions'] },
  { version: '000003 / 000005', file: 'add_plugins + schema_alignment', title: '插件持久化与日志', scope: '插件', summary: '建立插件元数据、API Key、权限声明和插件运行日志。', tables: ['plugins', 'api_keys', 'plugin_permissions', 'plugin_logs'] },
  { version: '000004', file: '000004_seed_admin.up.sql', title: '默认管理员与版块种子', scope: '种子数据', summary: '写入默认管理员账号、管理员角色和默认版块，不新增数据表。', tables: ['users', 'accounts', 'user_roles', 'categories'] },
  { version: '000006', file: '000006_add_ai_call_logs.up.sql', title: 'AI Gateway 调用日志', scope: '集成', summary: '记录 provider、模型、token、耗时和错误。', tables: ['ai_call_logs'] },
  { version: '000007 - 000009', file: 'add_user_spaces + contents + styles', title: '个人主页与同步内容', scope: '个人空间', summary: '建立个人主页配置、主题同步内容，并给主页增加风格状态。', tables: ['user_spaces', 'user_space_contents'] },
  { version: '000010', file: '000010_fix_admin_seed_password.up.sql', title: '默认管理员密码修正', scope: '种子数据', summary: '修正默认管理员密码哈希，不新增数据表。', tables: ['accounts'] },
  { version: '000011', file: '000011_v05_operational_features.up.sql', title: '运营化与低风险集成', scope: 'v0.5', summary: '增加主页风格快照、Webhook、MCP 审计、消息绑定和消息日志。', tables: ['user_space_style_snapshots', 'webhook_endpoints', 'webhook_deliveries', 'mcp_audit_logs', 'message_bindings', 'message_logs'] },
  { version: '000012', file: '000012_category_default_tags.up.sql', title: '版块默认标签', scope: '社区', summary: '为 categories 增加 default_tags 数组字段和索引。', tables: ['categories'] },
  { version: '000013', file: '000013_controlled_richtext_article.up.sql', title: '受控富文本图文文章', scope: '内容', summary: '将富文本正文和图片元数据关联到既有 threads。', tables: ['richtext_article_contents', 'richtext_article_assets'] },
  { version: '000014', file: '000014_role_assignment_permissions.up.sql', title: '角色分配修复与细粒度权限', scope: '身份', summary: '修复 user_roles 全局角色唯一性，分离角色读取、分配和撤销权限，不新增数据表。', tables: ['user_roles', 'permissions'] },
  { version: '000015', file: '000015_category_moderation_scope.up.sql', title: '版块版主作用域', scope: '身份与治理', summary: '停用历史全局版主授权，约束 global/category 作用域形状，并补充主题锁定权限。', tables: ['user_roles', 'permissions'] },
  { version: '000016', file: '000016_v06_core_integrity.up.sql', title: '核心数据完整性', scope: '数据库', summary: '在数据预检后增加核心状态与计数检查、稳定关系外键和关键查询索引。', tables: ['users', 'accounts', 'sessions', 'categories', 'threads', 'posts', 'user_roles', 'permissions', 'user_spaces', 'user_space_contents', 'richtext_article_contents', 'richtext_article_assets', 'webhook_deliveries'] },
  { version: '000017', file: '000017_v06_admin_permission_split.up.sql', title: '管理权限细分', scope: '身份与治理', summary: '将插件、富文本、集成、空间、日志和首页管理从粗粒度角色权限拆分为资源动作权限。', tables: ['permissions'] },
  { version: '000018', file: '000018_plugin_ui_runtime.up.sql', title: '插件 UI Runtime 状态', scope: '插件', summary: '持久化 BackendState、FrontendState、Health 和 UI revision，并用 CHECK 约束状态集合。', tables: ['plugins'] },
]

const tableByName = (name: string) => databaseTables.find((table) => table.name === name) || databaseTables[0]
const currentTable = computed(() => tableByName(selectedTable.value))

const visibleTables = computed(() => {
  if (domainFilter.value === 'all') return databaseTables
  if (domainFilter.value === 'identity') return databaseTables.filter((table) => table.domain === 'identity' || table.domain === 'community')
  if (domainFilter.value === 'space') return databaseTables.filter((table) => table.domain === 'space')
  return databaseTables.filter((table) => table.domain === 'plugin' || table.domain === 'integration' || table.domain === 'system')
})

const visibleRelations = computed(() => {
  if (domainFilter.value === 'all') return relations
  if (domainFilter.value === 'identity') return relations.filter((relation) => relation.domains.some((domain) => domain === 'identity' || domain === 'community'))
  if (domainFilter.value === 'space') return relations.filter((relation) => relation.domains.includes('space'))
  return relations.filter((relation) => relation.domains.some((domain) => domain === 'plugin' || domain === 'integration' || domain === 'system'))
})

const domainLabel = (domain: Domain) => ({ identity: '身份', community: '社区', space: '空间与内容', plugin: '插件', integration: '集成', system: '系统' })[domain]

watch(domainFilter, () => {
  if (!visibleTables.value.some((table) => table.name === selectedTable.value)) {
    selectedTable.value = visibleTables.value[0]?.name || 'users'
  }
})
</script>

<style scoped>
.system-architecture { max-width: 1520px; }
.page-header, .toolbar-band, .section-heading, .inspector-heading, .storage-card-heading, .migration-card-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.page-header { margin-bottom: 16px; padding: 22px 24px; border: 1px solid #e5e7eb; border-radius: 8px; background: #fff; }
.page-header h2 { margin: 4px 0 8px; font-size: 24px; color: #1f2937; }
.page-header p, .toolbar-band p, .section-heading p, .inspector-heading p, .migration-card p { margin: 0; line-height: 1.6; color: #606266; }
.eyebrow { margin: 0; font-size: 13px; font-weight: 700; color: #2563eb; }
.architecture-alert { margin-bottom: 16px; }
.architecture-tabs { padding: 0 18px 18px; border: 1px solid #e4e7ed; border-radius: 8px; background: #fff; }
.toolbar-band { flex-wrap: wrap; padding: 16px 0; border-bottom: 1px solid #ebeef5; }
.toolbar-band strong { display: block; margin-bottom: 4px; color: #303133; }
.database-layout { display: grid; grid-template-columns: minmax(0, 1.7fr) minmax(300px, .8fr); gap: 16px; margin-top: 16px; }
.relation-board { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-content: start; max-height: 720px; overflow: auto; padding: 4px; }
.relation-lane { display: grid; grid-template-columns: minmax(100px, 1fr) minmax(96px, .8fr) minmax(100px, 1fr); align-items: center; gap: 8px; min-height: 114px; padding: 10px; border: 1px solid #e5e7eb; border-radius: 8px; background: #fbfcfe; }
.table-node, .catalog-node { min-width: 0; border: 1px solid #dce5f0; border-radius: 6px; background: #fff; color: #1f2937; text-align: left; cursor: pointer; transition: border-color .18s ease, box-shadow .18s ease; }
.table-node { min-height: 84px; padding: 10px; }
.table-node:hover, .table-node[aria-pressed="true"], .catalog-node:hover, .catalog-node.selected { border-color: #409eff; box-shadow: 0 0 0 2px rgba(64, 158, 255, .12); }
.table-node strong, .catalog-node strong { display: block; overflow: hidden; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.table-node small, .catalog-node span { display: block; margin-top: 5px; overflow: hidden; color: #606266; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.node-domain { display: inline-block; margin-bottom: 6px; color: #64748b; font-size: 11px; line-height: 1; }
.relation-link { display: grid; grid-template-columns: auto minmax(12px, 1fr) auto minmax(12px, 1fr) auto; align-items: center; gap: 4px; color: #64748b; font-size: 11px; text-align: center; }
.relation-link i { height: 1px; background: #94a3b8; }
.relation-link em { max-width: 78px; overflow: hidden; color: #475569; font-style: normal; line-height: 1.25; text-overflow: ellipsis; }
.table-inspector { position: sticky; top: 12px; align-self: start; padding: 18px; border: 1px solid #dbe4ee; border-radius: 8px; background: #f8fafc; }
.inspector-heading { align-items: flex-start; }
.inspector-heading h3 { margin: 0 0 4px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 17px; }
.table-purpose { margin: 14px 0; line-height: 1.7; color: #475569; }
.field-list { display: flex; flex-wrap: wrap; gap: 6px; }
.field-list code, .maintenance-grid code { padding: 3px 5px; border-radius: 4px; background: #eef2f7; color: #334155; font-size: 12px; }
.catalog-section { margin-top: 20px; padding-top: 16px; border-top: 1px solid #ebeef5; }
.section-heading h3 { margin: 0 0 4px; font-size: 16px; }
.table-catalog { display: grid; grid-template-columns: repeat(auto-fill, minmax(172px, 1fr)); gap: 10px; margin-top: 14px; }
.catalog-node { padding: 11px; }
.domain-identity { border-left: 3px solid #2563eb; }
.domain-community { border-left: 3px solid #059669; }
.domain-space { border-left: 3px solid #d97706; }
.domain-plugin { border-left: 3px solid #7c3aed; }
.domain-integration { border-left: 3px solid #db2777; }
.domain-system { border-left: 3px solid #475569; }
.data-flow { display: grid; grid-template-columns: minmax(150px, .8fr) minmax(80px, .25fr) minmax(180px, 1fr) minmax(80px, .25fr) minmax(230px, 1.4fr); align-items: center; gap: 12px; padding: 28px 0; }
.flow-node { display: grid; justify-items: start; gap: 7px; min-height: 146px; padding: 18px; border: 1px solid #dbe4ee; border-radius: 8px; background: #fff; }
.flow-node :deep(.el-icon) { font-size: 22px; }
.flow-node strong { color: #1e293b; }
.flow-node span { color: #64748b; font-size: 13px; line-height: 1.55; }
.flow-client { border-top: 3px solid #2563eb; }.flow-api { border-top: 3px solid #059669; }.flow-postgres { border-left: 3px solid #7c3aed; }.flow-files { border-left: 3px solid #d97706; }.flow-logs { border-left: 3px solid #64748b; }
.flow-arrow { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: 4px; color: #64748b; font-size: 11px; }
.flow-arrow::before { height: 1px; background: #94a3b8; content: ''; }.flow-arrow::after { border-top: 5px solid transparent; border-bottom: 5px solid transparent; border-left: 7px solid #94a3b8; content: ''; }
.flow-targets { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.flow-targets .flow-node { min-height: 146px; }.flow-branch { align-self: stretch; }.flow-branch::before { background: repeating-linear-gradient(90deg, #94a3b8 0 8px, transparent 8px 12px); }
.storage-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin: 10px 0 16px; }
.storage-card { min-height: 238px; border-radius: 8px; }.storage-card-heading code { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.storage-card p { margin: 0; color: #475569; line-height: 1.6; }.storage-card ul { min-height: 84px; margin: 12px 0; padding-left: 18px; color: #606266; font-size: 13px; line-height: 1.7; }.storage-note { padding-top: 10px; border-top: 1px solid #ebeef5; color: #909399; font-size: 12px; line-height: 1.5; }
.migration-timeline { margin: 22px 0 0 4px; }.migration-card-heading { align-items: flex-start; }.migration-card-heading code { margin-right: 10px; color: #2563eb; }.migration-card-heading strong { color: #1f2937; }.migration-card p { margin: 10px 0; }.migration-tables { display: flex; flex-wrap: wrap; gap: 6px; }
.maintenance-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-top: 18px; }.maintenance-grid article { padding: 16px; border: 1px solid #e4e7ed; border-radius: 8px; background: #f8fafc; }.maintenance-grid h3 { margin: 0 0 10px; font-size: 15px; }.maintenance-grid p { margin: 12px 0 0; color: #606266; line-height: 1.6; font-size: 13px; }
@media (max-width: 1200px) { .database-layout { grid-template-columns: 1fr; }.table-inspector { position: static; }.storage-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }.data-flow { grid-template-columns: 1fr; }.flow-arrow { min-height: 34px; grid-template-columns: 1fr; grid-template-rows: 1fr auto; justify-items: center; }.flow-arrow::before { width: 1px; height: 18px; }.flow-arrow::after { border-top: 7px solid #94a3b8; border-right: 5px solid transparent; border-left: 5px solid transparent; border-bottom: 0; }.flow-targets { grid-template-columns: repeat(3, minmax(0, 1fr)); }.flow-branch { display: none; } }
@media (max-width: 820px) { .page-header, .toolbar-band { align-items: flex-start; flex-direction: column; }.relation-board { grid-template-columns: 1fr; max-height: none; }.storage-grid, .maintenance-grid, .flow-targets { grid-template-columns: 1fr; }.relation-lane { grid-template-columns: minmax(94px, 1fr) minmax(84px, .7fr) minmax(94px, 1fr); }.architecture-tabs { padding: 0 12px 12px; } }
</style>
