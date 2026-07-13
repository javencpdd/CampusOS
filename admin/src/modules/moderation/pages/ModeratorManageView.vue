<template>
  <div class="moderator-page" v-loading="loading">
    <header class="page-header">
      <div>
		<p class="eyebrow">core.moderation</p>
        <h2>板块版主管理</h2>
        <p>版主只在被分配的板块内获得治理能力，所有操作仍由后端校验板块范围。</p>
      </div>
      <div class="header-actions">
		<el-tag type="success">安全核心始终启用</el-tag>
        <el-button :icon="Refresh" circle title="刷新" @click="load" />
      </div>
    </header>

    <el-alert
	  type="info"
      :closable="false"
      show-icon
	  title="板块作用域检查、权限策略、审计和数据完整性校验始终生效；这里只配置版主可执行动作的上限。"
    />

    <section class="config-band">
      <div class="section-heading">
        <div>
          <h3>版主权限上限</h3>
          <p>这里控制所有板块版主最多可以执行哪些操作。保存后立即生效，无需重启 API。</p>
        </div>
        <el-button type="primary" :loading="configSaving" @click="saveConfig">保存权限配置</el-button>
      </div>
      <div class="permission-grid">
        <label v-for="item in permissionOptions" :key="item.key" class="permission-item">
          <el-switch v-model="config[item.key]" />
          <span>
            <strong>{{ item.label }}</strong>
            <small>{{ item.description }}</small>
          </span>
        </label>
      </div>
    </section>

    <section class="moderator-band">
      <div class="section-heading">
        <div>
          <h3>用户与板块范围</h3>
          <p>未选择任何板块表示撤销该用户的版主角色。</p>
        </div>
        <el-input v-model="keyword" clearable placeholder="搜索用户" style="width: 220px" />
      </div>
      <el-table :data="filteredUsers" border stripe>
        <el-table-column prop="username" label="用户名" min-width="150" />
        <el-table-column prop="nickname" label="昵称" min-width="150" />
        <el-table-column label="负责板块" min-width="320">
          <template #default="{ row }">
            <div v-if="assignmentFor(row.id).categories.length" class="category-tags">
              <el-tag v-for="category in assignmentFor(row.id).categories" :key="category.id" effect="plain">
                {{ category.name }}
              </el-tag>
            </div>
            <span v-else class="empty-text">非版主</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="130" align="center">
          <template #default="{ row }">
            <el-button type="primary" plain size="small" @click="openEditor(row)">配置范围</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="dialogVisible" title="配置板块版主" width="560px" destroy-on-close>
      <div v-if="selectedUser" class="scope-dialog">
        <p><strong>{{ selectedUser.username }}</strong>（{{ selectedUser.nickname }}）</p>
        <el-form label-position="top">
          <el-form-item label="负责板块">
            <el-select v-model="selectedCategoryIds" multiple filterable placeholder="选择一个或多个板块" style="width: 100%">
              <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
            </el-select>
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" title="保存空列表会撤销版主角色；跨板块请求始终返回 403。" />
      </div>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="scopeSaving" @click="saveScope">保存范围</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { categoryApi } from '@/modules/community/api'
import { moderationApi, userApi } from '@/modules/identity/api'
import { featureApi } from '@/modules/features/api'

const loading = ref(false)
const users = ref<any[]>([])
const categories = ref<any[]>([])
const assignments = ref<Record<string, any>>({})
const keyword = ref('')
const dialogVisible = ref(false)
const selectedUser = ref<any>(null)
const selectedCategoryIds = ref<string[]>([])
const scopeSaving = ref(false)
const configSaving = ref(false)
const config = reactive<Record<string, boolean>>({
  allow_pin: true,
  allow_lock: true,
  allow_delete_post: true,
})

const permissionOptions = [
  { key: 'allow_pin', label: '置顶主题', description: '置顶或取消置顶所负责板块中的主题。' },
  { key: 'allow_lock', label: '锁定主题', description: '锁定或解锁主题，锁定后普通用户不能继续回复。' },
  { key: 'allow_delete_post', label: '删除回复', description: '删除所负责板块主题下的回复。' },
]

const filteredUsers = computed(() => {
  const value = keyword.value.trim().toLowerCase()
  if (!value) return users.value
  return users.value.filter((user) => `${user.username} ${user.nickname} ${user.email}`.toLowerCase().includes(value))
})

const assignmentFor = (userId: string) => assignments.value[userId] || { category_ids: [], categories: [] }

const load = async () => {
  loading.value = true
  try {
    const [usersResult, categoriesResult, moderatorsResult, statusResult] = await Promise.all([
      userApi.list({ page: 1, page_size: 100 }),
      categoryApi.list(),
      moderationApi.list(),
	  moderationApi.status(),
    ]) as any[]
    users.value = usersResult?.data?.items || []
    categories.value = Array.isArray(categoriesResult?.data) ? categoriesResult.data : []
    const nextAssignments: Record<string, any> = {}
    for (const item of moderatorsResult?.data?.items || []) nextAssignments[item.user_id] = item
    assignments.value = nextAssignments
	const savedConfig = statusResult?.data?.config || {}
    for (const key of Object.keys(config)) config[key] = savedConfig[key] ?? true
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载版主管理信息失败')
  } finally {
    loading.value = false
  }
}

const openEditor = (user: any) => {
  selectedUser.value = user
  selectedCategoryIds.value = [...assignmentFor(user.id).category_ids]
  dialogVisible.value = true
}

const saveScope = async () => {
  if (!selectedUser.value) return
  scopeSaving.value = true
  try {
    await moderationApi.update(selectedUser.value.id, selectedCategoryIds.value)
    ElMessage.success(selectedCategoryIds.value.length ? '版主管理范围已保存' : '版主角色已撤销')
    dialogVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存版主范围失败')
  } finally {
    scopeSaving.value = false
  }
}

const saveConfig = async () => {
  configSaving.value = true
  try {
	await featureApi.updateCompatibilityConfig('category-moderation', { ...config })
    ElMessage.success('权限配置已保存并立即生效')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存权限配置失败')
  } finally {
    configSaving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.moderator-page {
  display: grid;
  gap: 16px;
  max-width: 1400px;
}
.page-header,
.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}
.page-header h2,
.section-heading h3 {
  margin: 0;
}
.page-header p,
.section-heading p {
  margin: 6px 0 0;
  color: #606266;
}
.eyebrow {
  color: #909399 !important;
  font-size: 12px;
  text-transform: uppercase;
}
.header-actions,
.category-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.config-band,
.moderator-band {
  padding: 18px;
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
}
.permission-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}
.permission-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px;
  border: 1px solid #ebeef5;
  border-radius: 6px;
}
.permission-item span {
  display: grid;
  gap: 4px;
}
.permission-item small,
.empty-text {
  color: #909399;
}
.moderator-band .el-table {
  margin-top: 16px;
}
.scope-dialog {
  display: grid;
  gap: 14px;
}
@media (max-width: 800px) {
  .page-header,
  .section-heading {
    flex-direction: column;
  }
  .permission-grid {
    grid-template-columns: 1fr;
  }
}
</style>
