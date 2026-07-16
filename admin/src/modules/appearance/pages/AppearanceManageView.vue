<template>
  <div class="appearance-page" v-loading="loading">
    <header class="page-header">
      <div>
        <p class="eyebrow">Appearance & Resource Packages</p>
        <h2>外观与风格包</h2>
        <p>管理可用资源和系统首页外观。系统主题与个人主页风格仍由各自的用户在对应前台页面选择。</p>
      </div>
      <el-button
        :icon="Refresh"
        circle
        title="刷新风格包目录"
        aria-label="刷新风格包目录"
        @click="loadCatalog"
      />
    </header>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="风格包是 Resource Package，不是可卸载业务插件"
      description="data/plugins 只保存切换、校验和导入导出的实现；主题、模板、图片和 CSS 保存在 data/resources 或 data/plugin_data 的兼容资源目录。"
    />

    <section class="ownership-grid" aria-label="风格包选择边界">
      <article v-for="item in ownership" :key="item.title">
        <strong>{{ item.title }}</strong>
        <el-tag size="small" effect="plain" :type="item.type">{{ item.actor }}</el-tag>
        <p>{{ item.description }}</p>
      </article>
    </section>

    <HomepagePackManager />

    <section class="theme-band" aria-labelledby="system-theme-heading">
      <div class="section-heading">
        <div>
          <p class="eyebrow theme-eyebrow">Web Theme Catalog</p>
          <h3 id="system-theme-heading">系统主题目录</h3>
          <p>管理员负责提供和审核主题；每位用户在用户前台选择自己的系统主题和可配置参数。</p>
        </div>
        <el-button type="primary" plain :icon="Promotion" @click="openWebAppearance">打开用户端切换页</el-button>
      </div>

      <div class="catalog-state">
        <el-tag :type="catalog.enabled ? 'success' : 'info'">{{ catalog.enabled ? '目录已启用' : '目录已停用' }}</el-tag>
        <el-tag effect="plain">{{ catalog.allow_user_switch ? '允许用户切换' : '使用管理员默认主题' }}</el-tag>
        <span>默认主题：<code>{{ catalog.default_style_pack || '未设置' }}</code></span>
      </div>

      <div v-if="catalog.items.length" class="theme-grid">
        <article v-for="theme in catalog.items" :key="theme.name" class="theme-item">
          <img v-if="theme.preview_url" :src="theme.preview_url" :alt="`${theme.display_name || theme.name} 预览`" />
          <div v-else class="preview-empty">暂无预览</div>
          <div class="theme-copy">
            <div class="theme-title">
              <strong>{{ theme.display_name || theme.name }}</strong>
              <el-tag v-if="theme.name === catalog.default_style_pack" type="success" size="small">默认</el-tag>
            </div>
            <code>{{ theme.name }} · v{{ theme.version || '-' }}</code>
            <p>{{ theme.description || '该主题没有填写说明。' }}</p>
          </div>
        </article>
      </div>
      <el-empty v-else description="管理员尚未提供通过筛查的系统主题" />
    </section>

    <section class="space-style-band" aria-labelledby="space-style-heading">
      <div>
        <p class="eyebrow space-eyebrow">Personal Space Style</p>
        <h3 id="space-style-heading">个人主页风格包</h3>
        <p>管理员提供候选包，主页所有者在自己的头像菜单进入个人主页设置并选择。Admin 不代替用户修改个人主页，也不能读取用户私有课表。</p>
      </div>
      <div class="path-list">
        <code>data/resources/space-style-packs/</code>
        <code>data/plugin_data/personal-space/style-packs/（兼容来源）</code>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Promotion, Refresh } from '@element-plus/icons-vue'
import HomepagePackManager from '@/modules/appearance/components/HomepagePackManager.vue'
import { webThemeCatalogApi } from '@/modules/appearance/api'

interface ThemeItem {
  name: string
  display_name?: string
  version?: string
  description?: string
  preview_url?: string
}

interface ThemeCatalog {
  enabled: boolean
  allow_user_switch: boolean
  default_style_pack?: string
  items: ThemeItem[]
}

const loading = ref(false)
const catalog = ref<ThemeCatalog>({ enabled: false, allow_user_switch: false, items: [] })
const webUrl = (import.meta.env.VITE_WEB_URL || 'http://localhost:3000').replace(/\/$/, '')

const ownership = [
  { title: '首页风格包', actor: '管理员统一切换', type: 'warning', description: '作用于用户前台首页，切换后所有访问者看到同一首页方案。' },
  { title: '系统主题', actor: '用户自主切换', type: 'success', description: '管理员提供候选目录，用户在本机为自己的账号选择主题。' },
  { title: '个人主页风格', actor: '主页所有者选择', type: 'info', description: '访问用户 A 的主页时，所有人看到用户 A 保存的主页风格。' },
] as const

const unwrap = (payload: any) => payload?.data || payload

const loadCatalog = async () => {
  loading.value = true
  try {
    const value = unwrap(await webThemeCatalogApi.catalog()) || {}
    catalog.value = {
      enabled: Boolean(value.enabled),
      allow_user_switch: Boolean(value.allow_user_switch),
      default_style_pack: value.default_style_pack || '',
      items: Array.isArray(value.items) ? value.items : [],
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载系统主题目录失败')
  } finally {
    loading.value = false
  }
}

const openWebAppearance = () => {
  window.open(`${webUrl}/appearance`, '_blank', 'noopener,noreferrer')
}

onMounted(loadCatalog)
</script>

<style scoped>
.appearance-page {
  display: grid;
  gap: 16px;
  max-width: 1440px;
}

.page-header,
.theme-band,
.space-style-band {
  padding: 20px;
  border: 1px solid #e4e7ed;
  background: #fff;
}

.page-header,
.section-heading,
.space-style-band,
.theme-title {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.page-header h2,
.section-heading h3,
.space-style-band h3,
.page-header p,
.section-heading p,
.space-style-band p {
  margin: 0;
}

.page-header > div > p:last-child,
.section-heading > div > p:last-child,
.space-style-band > div > p:last-child {
  margin-top: 7px;
  color: #606266;
  line-height: 1.6;
}

.eyebrow {
  margin-bottom: 5px !important;
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}

.theme-eyebrow {
  color: #15803d;
}

.space-eyebrow {
  color: #b45309;
}

.ownership-grid,
.theme-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.ownership-grid article,
.theme-item {
  border: 1px solid #e4e7ed;
  background: #fff;
}

.ownership-grid article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 8px;
  padding: 15px;
}

.ownership-grid p {
  grid-column: 1 / -1;
  margin: 0;
  color: #606266;
  font-size: 13px;
  line-height: 1.55;
}

.theme-band {
  display: grid;
  gap: 16px;
}

.catalog-state,
.path-list {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 9px;
}

.catalog-state span {
  color: #606266;
  font-size: 13px;
}

.theme-item {
  min-width: 0;
  overflow: hidden;
}

.theme-item img,
.preview-empty {
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  background: #f5f7fa;
}

.preview-empty {
  display: grid;
  place-items: center;
  color: #909399;
  font-size: 13px;
}

.theme-copy {
  display: grid;
  gap: 7px;
  padding: 14px;
}

.theme-copy code {
  color: #909399;
  font-size: 12px;
}

.theme-copy p {
  margin: 0;
  color: #606266;
  font-size: 13px;
  line-height: 1.55;
}

.space-style-band {
  align-items: center;
}

.path-list {
  justify-content: flex-end;
}

.path-list code {
  padding: 7px 9px;
  border: 1px solid #dcdfe6;
  background: #f8fafc;
  overflow-wrap: anywhere;
}

@media (max-width: 900px) {
  .ownership-grid,
  .theme-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .page-header,
  .section-heading,
  .space-style-band {
    align-items: stretch;
    flex-direction: column;
  }

  .section-heading .el-button,
  .path-list {
    width: 100%;
  }

  .path-list {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
