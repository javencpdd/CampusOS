<template>
  <div class="developer-docs">
    <section class="page-heading">
      <div>
        <p class="eyebrow">开发者上手</p>
        <h1>CampusOS 说明文档</h1>
        <p>
          这页给刚接触 CampusOS 的开发者使用。先看目录、插件是什么、怎么写、怎么导入、怎么排查，再去改代码会省很多时间。
        </p>
      </div>
      <div class="heading-actions">
        <el-button type="primary" @click="$router.push({ name: 'Plugins' })">
          <el-icon><Connection /></el-icon>
          打开插件管理
        </el-button>
        <el-button @click="$router.push({ name: 'PlatformLogs' })">
          <el-icon><Monitor /></el-icon>
          查看平台日志
        </el-button>
      </div>
    </section>

    <el-row :gutter="16" class="guide-grid">
      <el-col v-for="item in quickCards" :key="item.title" :xs="24" :sm="12" :lg="6">
        <el-card shadow="never" class="guide-card">
          <div class="guide-index">{{ item.index }}</div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
        </el-card>
      </el-col>
    </el-row>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Guide /></el-icon>
        <h2>1. 新人先按这个顺序看</h2>
      </div>
      <div class="plain-steps">
        <div v-for="step in startSteps" :key="step.title" class="plain-step">
          <span>{{ step.index }}</span>
          <div>
            <strong>{{ step.title }}</strong>
            <p>{{ step.description }}</p>
          </div>
        </div>
      </div>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><FolderOpened /></el-icon>
        <h2>2. 先记住这些目录</h2>
      </div>
      <el-table :data="directoryRows" border stripe>
        <el-table-column prop="path" label="目录或文件" min-width="280">
          <template #default="{ row }">
            <code>{{ row.path }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="purpose" label="用途" min-width="300" />
        <el-table-column prop="tip" label="怎么理解" min-width="320" />
      </el-table>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Connection /></el-icon>
        <h2>3. 插件到底是什么</h2>
      </div>
      <div class="explain-block">
        <p>
          可以把插件理解成“带说明书的小功能包”。CampusOS 启动时会扫描
          <code>data/plugins</code>，读取每个插件目录里的 <code>plugin.yaml</code>，
          然后根据插件声明的运行时、权限、配置和订阅事件来加载它。
        </p>
        <p>
          插件代码放在 <code>data/plugins/&lt;plugin&gt;/</code>，插件运行时产生的数据放在
          <code>data/plugin_data/&lt;plugin&gt;/</code>。这两个目录不要混用：前者像“程序安装目录”，后者像“程序数据目录”。
        </p>
      </div>
      <el-row :gutter="16" class="runtime-grid">
        <el-col v-for="runtime in runtimeRows" :key="runtime.name" :xs="24" :md="8">
          <el-card shadow="never" class="runtime-card">
            <el-tag :type="runtime.type" effect="plain">{{ runtime.name }}</el-tag>
            <h3>{{ runtime.title }}</h3>
            <p>{{ runtime.description }}</p>
            <p class="muted">{{ runtime.when }}</p>
          </el-card>
        </el-col>
      </el-row>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><EditPen /></el-icon>
        <h2>4. 写一个插件的最小流程</h2>
      </div>
      <div class="process-list">
        <div v-for="step in pluginSteps" :key="step.title" class="process-item">
          <div class="process-number">{{ step.index }}</div>
          <div>
            <strong>{{ step.title }}</strong>
            <p>{{ step.description }}</p>
            <code v-if="step.command">{{ step.command }}</code>
          </div>
        </div>
      </div>
      <div class="code-panel">
        <div class="code-title">一个容易看懂的 plugin.yaml 例子</div>
        <pre><code>{{ pluginYamlExample }}</code></pre>
      </div>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Setting /></el-icon>
        <h2>5. plugin.yaml 里最常改的内容</h2>
      </div>
      <el-table :data="manifestRows" border stripe>
        <el-table-column prop="field" label="字段" width="180">
          <template #default="{ row }">
            <code>{{ row.field }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="meaning" label="意思" min-width="260" />
        <el-table-column prop="advice" label="建议" min-width="360" />
      </el-table>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Operation /></el-icon>
        <h2>6. 在管理端怎么管理插件</h2>
      </div>
      <div class="management-flow">
        <div v-for="step in managementSteps" :key="step.title" class="management-step">
          <el-tag type="info" effect="plain">{{ step.index }}</el-tag>
          <strong>{{ step.title }}</strong>
          <p>{{ step.description }}</p>
        </div>
      </div>
      <el-alert
        title="插件启用/禁用只改变插件状态，不会自动删除插件目录。卸载才是删除已安装插件记录和文件的高风险操作。"
        type="warning"
        show-icon
        :closable="false"
      />
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Key /></el-icon>
        <h2>7. 权限、配置和数据怎么想</h2>
      </div>
      <div class="three-column">
        <div v-for="item in boundaryRows" :key="item.title" class="note-box">
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
          <ul>
            <li v-for="point in item.points" :key="point">{{ point }}</li>
          </ul>
        </div>
      </div>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Tools /></el-icon>
        <h2>8. 常用命令</h2>
      </div>
      <div class="command-grid">
        <div v-for="command in commandRows" :key="command.command" class="command-row">
          <code>{{ command.command }}</code>
          <span>{{ command.description }}</span>
        </div>
      </div>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><Warning /></el-icon>
        <h2>9. 常见问题排查</h2>
      </div>
      <el-collapse>
        <el-collapse-item v-for="item in troubleshootingRows" :key="item.title" :title="item.title">
          <p>{{ item.answer }}</p>
          <code v-if="item.command">{{ item.command }}</code>
        </el-collapse-item>
      </el-collapse>
    </section>

    <section class="doc-section">
      <div class="section-title">
        <el-icon><DocumentChecked /></el-icon>
        <h2>10. 提交前检查清单</h2>
      </div>
      <div class="checklist">
        <label v-for="item in checklistRows" :key="item">
          <input type="checkbox" disabled />
          <span>{{ item }}</span>
        </label>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import {
  Connection,
  DocumentChecked,
  EditPen,
  FolderOpened,
  Guide,
  Key,
  Monitor,
  Operation,
  Setting,
  Tools,
  Warning,
} from '@element-plus/icons-vue'

const quickCards = [
  {
    index: '01',
    title: '先跑起来',
    description: '用 make dev-all 启动依赖、后端、用户端和管理端，先看到页面再改代码。',
  },
  {
    index: '02',
    title: '先看目录',
    description: '插件代码、插件数据、上传文件和后台源码分开保存，路径清楚后不容易改错。',
  },
  {
    index: '03',
    title: '先改最小例子',
    description: '从 hello-wasm 或 built-in 插件 README 入手，不要一开始就写复杂插件。',
  },
  {
    index: '04',
    title: '先看日志',
    description: '插件失败时先看插件日志和平台日志，再去查数据库或前端。',
  },
]

const startSteps = [
  {
    index: '1',
    title: '确认服务能启动',
    description: '先运行 STOP_EXISTING=true make dev-all，确认 API、web 和 admin 都能打开。',
  },
  {
    index: '2',
    title: '打开插件管理',
    description: '进入管理端的“插件管理”，看当前有哪些插件、状态是什么、能不能打开配置。',
  },
  {
    index: '3',
    title: '看一个简单插件',
    description: '优先看 data/plugins/hello-wasm，再看 personal-space 和 homepage-customizer 这两个内置插件。',
  },
  {
    index: '4',
    title: '改一处小配置',
    description: '例如修改 homepage-customizer 的标题或背景图，观察前台首页是否变化。',
  },
  {
    index: '5',
    title: '再写自己的插件',
    description: '确定运行时、权限、配置字段和数据目录后，再创建新插件目录和 plugin.yaml。',
  },
]

const directoryRows = [
  {
    path: 'data/plugins/<plugin>/',
    purpose: '插件实现目录，保存 plugin.yaml、运行入口、Wasm 文件或内置插件 README。',
    tip: '像安装目录。插件代码和说明放这里。',
  },
  {
    path: 'data/plugin_data/<plugin>/',
    purpose: '插件运行数据目录，保存 KV 数据、源码风格包、后续生成文件。',
    tip: '像数据目录。插件运行后产生或可编辑的数据放这里。',
  },
  {
    path: 'data/plugin_data/<plugin>/style-packs/',
    purpose: '页面拓展风格包源码目录。',
    tip: '风格包不是插件实现代码，所以放 plugin_data，不放 data/plugins。',
  },
  {
    path: 'internal/plugin/',
    purpose: '后端插件管理器、运行时、导入导出和权限检查的核心源码。',
    tip: '要改插件系统本身时才进这里。',
  },
  {
    path: 'cmd/campusosctl/',
    purpose: '插件 CLI，支持 inspect、pack、install 等命令。',
    tip: '要打包、检查、安装插件时用它。',
  },
  {
    path: 'admin/src/views/PluginManageView.vue',
    purpose: '管理端插件管理页面。',
    tip: '要改导入、配置、日志、启停按钮时看这里。',
  },
]

const runtimeRows = [
  {
    name: 'builtin',
    type: 'info' as const,
    title: '内置插件',
    description: '功能仍由后端核心模块实现，但用插件目录保存 manifest、默认配置和说明。',
    when: '适合 personal-space、homepage-customizer 这类系统默认能力。',
  },
  {
    name: 'wasm',
    type: 'warning' as const,
    title: 'Wasm 插件',
    description: '插件被编译成 .wasm，由 CampusOS 用 wazero 加载，隔离性更强。',
    when: '适合小型逻辑、事件处理和希望更安全隔离的插件。',
  },
  {
    name: 'grpc',
    type: 'success' as const,
    title: 'gRPC 插件',
    description: '插件作为独立进程运行，通过 gRPC 和主系统通信。',
    when: '适合复杂服务、长任务或需要独立依赖的插件。',
  },
]

const pluginSteps = [
  {
    index: '1',
    title: '选运行时',
    description: '先决定用 builtin、wasm 还是 grpc。新人通常先从 wasm 或现有 builtin 示例学。',
    command: '',
  },
  {
    index: '2',
    title: '创建目录',
    description: '每个插件一个目录，目录名建议和插件 name 一致。目录中至少要有 plugin.yaml。',
    command: 'mkdir -p data/plugins/my-plugin',
  },
  {
    index: '3',
    title: '写 plugin.yaml',
    description: '声明插件名称、版本、运行时、模块文件、权限、配置字段和订阅事件。',
    command: 'data/plugins/my-plugin/plugin.yaml',
  },
  {
    index: '4',
    title: '写代码或放入口文件',
    description: 'Wasm 插件放 plugin.wasm；gRPC 插件配置启动方式；builtin 插件通常对应后端已有服务。',
    command: '',
  },
  {
    index: '5',
    title: '本地检查和打包',
    description: '先 inspect 看 manifest 是否能解析，再 pack 成可导入包。',
    command: 'go run ./cmd/campusosctl plugin inspect data/plugins/my-plugin',
  },
  {
    index: '6',
    title: '到管理端导入和启用',
    description: '打开“插件管理”，导入包，确认预检信息，再启用、配置、看日志。',
    command: '',
  },
]

const pluginYamlExample = `name: my-plugin
display_name: "My Plugin"
version: "0.1.0"
description: "A small CampusOS plugin example."
runtime: wasm
events:
  subscribe:
    - "thread.created"
permissions:
  api:
    - resource: "log"
      actions: ["write"]
storage:
  type: none
config:
  module: "plugin.wasm"
  entrypoint: "handle_event"
  greeting: "Hello CampusOS"
config_schema:
  fields:
    - key: "greeting"
      label: "Greeting"
      type: "string"
      default: "Hello CampusOS"
      description: "Text shown by this plugin."`

const manifestRows = [
  {
    field: 'name',
    meaning: '插件的唯一名称。',
    advice: '用小写字母、数字和中划线，例如 my-plugin。后续导入、启停、日志都会用它。',
  },
  {
    field: 'runtime',
    meaning: '插件怎么运行。',
    advice: '当前常见值是 builtin、wasm、grpc。选错运行时会导致插件加载失败。',
  },
  {
    field: 'config.module',
    meaning: 'Wasm 插件模块文件。',
    advice: 'Wasm 插件通常写 plugin.wasm；gRPC 和 builtin 插件会有自己的启动或内置读取方式。',
  },
  {
    field: 'permissions.api',
    meaning: '插件希望调用哪些 Host API。',
    advice: '只申请必要权限。权限越多，审核和排查成本越高。',
  },
  {
    field: 'events.subscribe',
    meaning: '插件订阅哪些系统事件。',
    advice: '例如帖子创建、用户注册等。没有事件订阅的插件也可以只提供配置或被手动调用。',
  },
  {
    field: 'config_schema',
    meaning: '告诉管理端配置表单应该怎么画。',
    advice: '要让管理员能在页面上改配置，就写清楚 key、label、type、default 和 description。',
  },
]

const managementSteps = [
  {
    index: '导入',
    title: '上传插件包',
    description: '导入前会做预检，检查 manifest、包大小、checksum、权限和同名插件风险。',
  },
  {
    index: '启用',
    title: '让插件开始工作',
    description: '启用后插件进入运行状态。禁用插件会让它停止对用户侧或事件侧产生影响。',
  },
  {
    index: '配置',
    title: '修改插件参数',
    description: '如果插件写了 config_schema，管理端会自动生成配置表单。',
  },
  {
    index: '日志',
    title: '查看运行情况',
    description: '插件报错、事件处理失败、Host API 调用异常，优先在这里看。',
  },
  {
    index: '导出',
    title: '备份或迁移插件',
    description: '导出当前插件包，方便在另一套环境安装或做版本留档。',
  },
  {
    index: '卸载',
    title: '移除插件',
    description: '卸载是高风险操作。先确认没有用户功能依赖它，再执行。',
  },
]

const boundaryRows = [
  {
    title: '权限',
    description: '权限是插件能做什么的边界。',
    points: [
      '只申请当前功能必须用到的 Host API。',
      '导入前检查权限列表，避免插件拿到过大的系统能力。',
      '权限问题通常会出现在插件日志或后端日志里。',
    ],
  },
  {
    title: '配置',
    description: '配置是管理员能调整什么的边界。',
    points: [
      '默认值写在 plugin.yaml 的 config 中。',
      '表单声明写在 config_schema 中。',
      '保存配置后，插件或内置服务读取最新配置。',
    ],
  },
  {
    title: '数据',
    description: '数据是插件运行时产生什么的边界。',
    points: [
      '插件代码放 data/plugins。',
      '运行数据放 data/plugin_data。',
      '上传图片和个人空间文件放 data/images。',
    ],
  },
]

const commandRows = [
  {
    command: 'STOP_EXISTING=true make dev-all',
    description: '重新启动完整开发环境，端口被占用时也能先停旧进程。',
  },
  {
    command: 'go run ./cmd/campusosctl plugin inspect data/plugins/hello-wasm',
    description: '检查一个插件目录的 manifest 是否能解析。',
  },
  {
    command: 'go run ./cmd/campusosctl plugin pack data/plugins/hello-wasm',
    description: '把插件目录打包成可导入的插件包。',
  },
  {
    command: 'GOCACHE=/tmp/campusos-go-cache go test ./...',
    description: '运行后端 Go 测试，改插件后端逻辑时必须跑。',
  },
  {
    command: 'cd admin && pnpm build',
    description: '构建管理端，改管理端页面后必须跑。',
  },
  {
    command: 'cd web && pnpm build',
    description: '构建用户端，改用户前台或首页配置渲染时要跑。',
  },
]

const troubleshootingRows = [
  {
    title: '插件管理里看不到新插件',
    answer: '先确认插件目录在 data/plugins 下，目录里有 plugin.yaml；如果通过环境变量改过 PLUGINS_DIR，就以环境变量为准。',
    command: 'go run ./cmd/campusosctl plugin inspect data/plugins/<plugin>',
  },
  {
    title: '配置弹窗里没有表单',
    answer: '通常是 plugin.yaml 没有写 config_schema，或者字段类型不受支持。先看 manifest 校验是否通过。',
    command: '',
  },
  {
    title: '启用插件后用户侧没变化',
    answer: '先确认插件状态是 running，再看插件是否真的接入了对应功能。builtin 插件还要检查后端服务是否读取了插件配置或状态。',
    command: '',
  },
  {
    title: '导入插件提示同名插件已存在',
    answer: '这是正常保护。确认要覆盖时打开“覆盖”开关；不想覆盖就修改插件 name 或版本策略。',
    command: '',
  },
  {
    title: '插件报错但页面没有明显提示',
    answer: '先看“插件管理 -> 日志”，再看“平台日志”。前端报错看浏览器控制台，后端报错看 api.log。',
    command: '',
  },
  {
    title: '风格包应该放在哪里',
    answer: '页面风格包源码放 data/plugin_data/<plugin>/style-packs/<pack>/。data/plugins 只放插件实现代码和 manifest。',
    command: '',
  },
]

const checklistRows = [
  'plugin.yaml 能被 inspect 解析。',
  '权限只申请当前功能需要的最小集合。',
  'config_schema 字段能在管理端正常显示和保存。',
  '插件启用、禁用、配置、日志都在管理端走过一遍。',
  '插件数据没有写进 data/plugins，而是写进 data/plugin_data 或 data/images。',
  '后端测试和相关前端 build 已通过。',
  'README 或帮助文档已经说明新插件怎么使用。',
]
</script>

<style scoped>
.developer-docs {
  max-width: 1400px;
}

.page-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
  padding: 24px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
}

.page-heading h1 {
  margin: 4px 0 10px;
  font-size: 28px;
  line-height: 1.25;
  color: #1f2937;
}

.page-heading p {
  max-width: 760px;
  margin: 0;
  line-height: 1.8;
  color: #4b5563;
}

.eyebrow {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  color: #2563eb;
}

.heading-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.guide-grid {
  margin-bottom: 18px;
}

.guide-card {
  min-height: 160px;
  border-radius: 8px;
}

.guide-index {
  width: 40px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 6px;
  background: #eef2ff;
  color: #3730a3;
  font-weight: 700;
  margin-bottom: 14px;
}

.guide-card h3,
.runtime-card h3,
.note-box h3 {
  margin: 0 0 8px;
  font-size: 16px;
  color: #1f2937;
}

.guide-card p,
.runtime-card p,
.note-box p,
.plain-step p,
.process-item p,
.management-step p,
.explain-block p {
  margin: 0;
  line-height: 1.7;
  color: #4b5563;
}

.doc-section {
  margin-bottom: 18px;
  padding: 22px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid #e5e7eb;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  color: #2563eb;
}

.section-title h2 {
  margin: 0;
  font-size: 20px;
  color: #1f2937;
}

.plain-steps,
.process-list,
.management-flow,
.command-grid,
.checklist {
  display: grid;
  gap: 12px;
}

.plain-step,
.process-item,
.management-step,
.command-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 14px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #f9fafb;
}

.plain-step span,
.process-number {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border-radius: 999px;
  background: #dbeafe;
  color: #1d4ed8;
  font-weight: 700;
}

.explain-block {
  display: grid;
  gap: 10px;
  margin-bottom: 16px;
}

.runtime-grid {
  margin-top: 6px;
}

.runtime-card {
  min-height: 190px;
  border-radius: 8px;
}

.muted {
  margin-top: 10px !important;
  color: #6b7280 !important;
}

.code-panel {
  margin-top: 16px;
  border: 1px solid #d1d5db;
  border-radius: 8px;
  overflow: hidden;
}

.code-title {
  padding: 10px 14px;
  background: #f3f4f6;
  font-weight: 700;
  color: #374151;
}

pre {
  margin: 0;
  padding: 16px;
  overflow-x: auto;
  background: #111827;
  color: #f9fafb;
  line-height: 1.65;
}

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
}

.three-column {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 14px;
}

.note-box {
  padding: 16px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #f9fafb;
}

.note-box ul {
  margin: 12px 0 0;
  padding-left: 18px;
  color: #4b5563;
  line-height: 1.8;
}

.command-row {
  align-items: center;
}

.command-row code {
  flex: 0 0 360px;
  max-width: 100%;
  padding: 8px 10px;
  border-radius: 6px;
  background: #eef2ff;
  color: #3730a3;
  white-space: normal;
  word-break: break-word;
}

.command-row span {
  color: #4b5563;
  line-height: 1.7;
}

.checklist {
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
}

.checklist label {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 12px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  color: #374151;
  background: #f9fafb;
  line-height: 1.6;
}

@media (max-width: 860px) {
  .page-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .heading-actions {
    width: 100%;
  }

  .heading-actions .el-button {
    flex: 1;
  }

  .command-row {
    flex-direction: column;
    align-items: stretch;
  }

  .command-row code {
    flex-basis: auto;
  }
}
</style>
