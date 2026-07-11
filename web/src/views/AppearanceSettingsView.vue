<template>
  <div class="appearance-settings" v-loading="themeStore.loading">
    <header class="appearance-header">
      <div>
        <h2>界面风格</h2>
        <p>从管理员提供并通过筛查的系统风格包中选择。当前选择只保存在本机的当前用户下。</p>
      </div>
      <el-button :icon="RefreshLeft" :disabled="!themeStore.catalog.enabled" @click="restoreDefault">恢复默认</el-button>
    </header>

    <el-alert
      v-if="themeStore.error"
      :title="themeStore.error"
      type="error"
      show-icon
      :closable="false"
    />
    <el-empty v-else-if="!themeStore.catalog.enabled" description="系统风格插件当前未运行" />
    <el-empty v-else-if="themeStore.catalog.items.length === 0" description="管理员尚未提供可用风格包" />

    <section v-else class="theme-list" aria-label="系统风格包">
      <article
        v-for="item in themeStore.catalog.items"
        :key="item.name"
        class="theme-item"
        :class="{ active: item.name === themeStore.activeName }"
      >
        <img v-if="item.preview_url" :src="item.preview_url" :alt="`${item.display_name} 预览`" />
        <div v-else class="theme-preview-placeholder"><el-icon><Picture /></el-icon></div>
        <div class="theme-content">
          <div class="theme-title-row">
            <div>
              <h3>{{ item.display_name }}</h3>
              <code>{{ item.name }}@{{ item.version }}</code>
            </div>
            <el-tag v-if="item.name === themeStore.activeName" type="success" effect="plain">正在使用</el-tag>
          </div>
          <p>{{ item.description || '管理员提供的 CampusOS 系统风格包。' }}</p>
          <div class="token-row">
            <span
              v-for="token in colorTokens(item)"
              :key="token.key"
              class="color-token"
              :title="`${token.key}: ${token.value}`"
              :style="{ backgroundColor: token.value }"
            />
          </div>
          <div v-if="item.capabilities?.length" class="capability-list">
            <el-tag
              v-for="capability in item.capabilities"
              :key="capability"
              size="small"
              :type="capability === 'schedule.me.read' ? 'warning' : 'info'"
              effect="plain"
            >
              {{ capabilityLabel(capability) }}
            </el-tag>
          </div>
          <el-button
            type="primary"
            :disabled="item.name === themeStore.activeName || !themeStore.catalog.allow_user_switch"
            @click="selectTheme(item)"
          >
            应用风格
          </el-button>
        </div>
      </article>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { Picture, RefreshLeft } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import { useWebThemeStore, type WebThemeItem } from '@/stores/webTheme'

const userStore = useUserStore()
const themeStore = useWebThemeStore()

const userID = () => userStore.user?.id
const capabilityLabel = (capability: string) => ({
  'community.threads.read': '读取公开帖子摘要',
  'categories.read': '读取公开版块',
  'schedule.me.read': '读取我的课表（需授权）',
}[capability] || capability)

const colorTokens = (item: WebThemeItem) => Object.entries(item.tokens || {})
  .filter(([key, value]) => key.startsWith('color.') && /^#[0-9a-f]{6}$/i.test(value))
  .slice(0, 5)
  .map(([key, value]) => ({ key, value }))

const selectTheme = async (item: WebThemeItem) => {
  const grants: string[] = []
  if (item.capabilities?.includes('schedule.me.read')) {
    await ElMessageBox.confirm(
      '该风格包申请读取当前登录用户自己的课表。主应用会代为调用接口并裁剪数据，风格包不会获得登录令牌。是否授权？',
      '私有数据能力授权',
      { confirmButtonText: '授权并应用', cancelButtonText: '取消', type: 'warning' },
    )
    grants.push('schedule.me.read')
  }
  await themeStore.select(item.name, userID(), grants)
  ElMessage.success(`已应用 ${item.display_name}`)
}

const restoreDefault = async () => {
  await themeStore.restoreDefault(userID())
  ElMessage.success('已恢复管理员设置的默认风格')
}
</script>

<style scoped>
.appearance-settings {
  display: grid;
  gap: 16px;
  max-width: 1040px;
  margin: 0 auto;
}

.appearance-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding-bottom: 18px;
  border-bottom: 1px solid #dcdfe6;
}

.appearance-header h2,
.appearance-header p {
  margin: 0;
}

.appearance-header p {
  margin-top: 8px;
  color: #606266;
  line-height: 1.7;
}

.theme-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.theme-item {
  display: grid;
  grid-template-rows: 190px minmax(0, 1fr);
  overflow: hidden;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #ffffff;
}

.theme-item.active {
  border-color: #16a36a;
  box-shadow: 0 0 0 1px #16a36a;
}

.theme-item img,
.theme-preview-placeholder {
  width: 100%;
  height: 190px;
  object-fit: cover;
  background: #eef2f0;
}

.theme-preview-placeholder {
  display: grid;
  place-items: center;
  color: #87958e;
  font-size: 30px;
}

.theme-content {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 12px;
  padding: 18px;
}

.theme-title-row {
  display: flex;
  width: 100%;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.theme-title-row h3 {
  margin: 0 0 5px;
  font-size: 18px;
}

.theme-title-row code {
  color: #909399;
  font-size: 12px;
}

.theme-content p {
  margin: 0;
  color: #606266;
  line-height: 1.65;
}

.token-row,
.capability-list {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.color-token {
  width: 24px;
  height: 24px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 4px;
}

.theme-content .el-button {
  margin-top: auto;
}

@media (max-width: 760px) {
  .appearance-header {
    flex-direction: column;
  }

  .theme-list {
    grid-template-columns: 1fr;
  }
}
</style>
