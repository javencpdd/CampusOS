<template>
  <div class="resources-page">
    <header class="page-header">
      <p class="eyebrow">Developer resources</p>
      <h2>相关资料</h2>
      <p>项目教程和参考文档在独立文档站维护，管理端只保留稳定入口。</p>
    </header>

    <section class="resource-panel" aria-labelledby="resource-heading">
      <div class="section-heading">
        <div>
          <h3 id="resource-heading">开发者入口</h3>
          <p>链接会在新标签页打开。</p>
        </div>
      </div>

      <div class="resource-list">
        <article v-for="resource in resources" :key="resource.title" class="resource-item">
          <div class="resource-icon" :class="resource.tone">
            <el-icon><component :is="resource.icon" /></el-icon>
          </div>
          <div class="resource-copy">
            <strong>{{ resource.title }}</strong>
            <p>{{ resource.description }}</p>
            <code>{{ resource.url }}</code>
          </div>
          <el-button
            type="primary"
            plain
            :icon="Promotion"
            :aria-label="`打开${resource.title}`"
            @click="openExternal(resource.url)"
          >
            打开
          </el-button>
        </article>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { Document, Link, Promotion } from '@element-plus/icons-vue'

const docsUrl = (import.meta.env.VITE_DOCS_URL || 'http://localhost:3002').replace(/\/$/, '')
const githubUrl = import.meta.env.VITE_GITHUB_URL || 'https://github.com/javencpdd/CampusOS'

const resources = [
  {
    title: 'CampusOS 官方文档',
    description: '项目介绍、开发部署、HTTP API、插件编写、打包导入和生命周期说明。',
    url: docsUrl,
    icon: Document,
    tone: 'tone-docs',
  },
  {
    title: '完整入门路径',
    description: '从环境准备、启动验证和模块分类，一直走到 Admin 入口、测试与提交前检查。',
    url: `${docsUrl}/guide/getting-started`,
    icon: Document,
    tone: 'tone-guide',
  },
  {
    title: '权限配置入门',
    description: '逐步理解角色、Permission Code、板块范围和审计，并完成自定义角色与版主配置。',
    url: `${docsUrl}/guide/permission-configuration`,
    icon: Document,
    tone: 'tone-permission',
  },
  {
    title: '课表插件完整教程',
    description: '以课表为例区分 Built-in Feature 与 External Plugin，并演示受管数据、打包、发布和授权。',
    url: `${docsUrl}/plugins/schedule-plugin-tutorial`,
    icon: Document,
    tone: 'tone-plugin',
  },
  {
    title: 'GitHub 仓库',
    description: '查看源码、版本历史、Issue、Pull Request 和项目许可证。',
    url: githubUrl,
    icon: Link,
    tone: 'tone-source',
  },
]

const openExternal = (url: string) => {
  window.open(url, '_blank', 'noopener,noreferrer')
}
</script>

<style scoped>
.resources-page {
  display: grid;
  gap: 16px;
  max-width: 1080px;
}

.page-header,
.resource-panel {
  padding: 20px;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  background: #fff;
}

.page-header h2,
.section-heading h3 {
  margin: 0;
}

.page-header > p:last-child,
.section-heading p {
  margin: 7px 0 0;
  color: #606266;
  line-height: 1.65;
}

.eyebrow {
  margin: 0 0 6px;
  color: #15803d;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.resource-list {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.resource-item {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  align-items: center;
  gap: 14px;
  padding: 15px;
  border: 1px solid #ebeef5;
  border-radius: 6px;
}

.resource-icon {
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  border-radius: 6px;
  font-size: 21px;
}

.tone-docs {
  color: #166534;
  background: #ecfdf3;
}

.tone-source {
  color: #1d4ed8;
  background: #eff6ff;
}

.tone-guide {
  color: #b45309;
  background: #fffbeb;
}

.tone-plugin {
  color: #7c3aed;
  background: #f5f3ff;
}

.tone-permission {
  color: #b42318;
  background: #fff1f0;
}

.resource-copy {
  min-width: 0;
}

.resource-copy strong,
.resource-copy p,
.resource-copy code {
  display: block;
}

.resource-copy p {
  margin: 5px 0 8px;
  color: #606266;
  line-height: 1.55;
}

.resource-copy code {
  overflow: hidden;
  color: #909399;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 720px) {
  .resource-item {
    grid-template-columns: 40px minmax(0, 1fr);
  }

  .resource-item .el-button {
    grid-column: 2;
    justify-self: start;
  }
}
</style>
