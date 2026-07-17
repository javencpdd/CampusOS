<template>
  <div class="mutual-aid-list" v-loading="loading">
    <section class="page-heading">
      <div>
        <h2>校园互助</h2>
        <p>发布求助、提供帮助、志愿服务或资源共享信息。</p>
      </div>
      <router-link v-if="userStore.isLoggedIn" to="/mutual-aid/create">
        <el-button type="primary">发布互助</el-button>
      </router-link>
    </section>

    <el-alert
      v-if="!enabled"
      type="warning"
      :closable="false"
      show-icon
      title="校园互助功能暂未启用"
      description="管理员启用并重启服务后，可在已授权的板块发布互助信息。"
    />

    <section v-else class="filter-band">
      <el-input v-model="keyword" clearable placeholder="搜索标题、正文或标签" @keyup.enter="load">
        <template #append><el-button @click="load">搜索</el-button></template>
      </el-input>
      <el-select v-model="categoryID" clearable placeholder="全部板块" @change="resetAndLoad">
        <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
      </el-select>
    </section>

    <section v-if="enabled" class="aid-feed">
      <article v-for="item in items" :key="item.thread.id" class="aid-item">
        <div class="aid-item-main">
          <div class="tag-row">
            <el-tag size="small" effect="plain" :type="aidTypeTag(item.detail.aid_type)">{{
              aidTypeLabel(item.detail.aid_type)
            }}</el-tag>
            <el-tag size="small" :type="statusTag(item.detail.aid_status)">{{
              statusLabel(item.detail.aid_status)
            }}</el-tag>
            <el-tag v-for="tag in item.thread.tags || []" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
          </div>
          <router-link class="aid-title" :to="`/mutual-aid/${item.thread.id}`">{{ item.thread.title }}</router-link>
          <p>{{ item.thread.content }}</p>
          <div class="meta-row">
            <span>{{ item.thread.author_name || '校园用户' }}</span>
            <span>{{ formatTime(item.thread.created_at) }}</span>
            <span v-if="item.detail.location_scope">{{ item.detail.location_scope }}</span>
            <span v-if="item.detail.deadline">截止 {{ formatTime(item.detail.deadline) }}</span>
          </div>
        </div>
        <router-link :to="`/mutual-aid/${item.thread.id}`"><el-button text type="primary">查看</el-button></router-link>
      </article>
      <el-empty v-if="!loading && items.length === 0" description="暂无互助信息" />
      <div v-if="total > pageSize" class="pagination">
        <el-pagination
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next"
          @current-change="load"
        />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/modules/identity/store'
import { categoryApi } from '@/modules/community/api'
import { mutualAidApi, type AidStatus, type AidType, type MutualAidResult } from '../api'

const userStore = useUserStore()
const loading = ref(false)
const enabled = ref(true)
const items = ref<MutualAidResult[]>([])
const categories = ref<Array<{ id: string; name: string; is_closed?: boolean; node_kind?: string }>>([])
const keyword = ref('')
const categoryID = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)

const aidTypeLabel = (value: AidType) =>
  ({ request: '求助', offer: '提供帮助', volunteer: '志愿服务', resource_share: '资源共享' })[value] || value
const statusLabel = (value: AidStatus) =>
  ({ open: '开放中', in_progress: '处理中', resolved: '已解决', closed: '已关闭' })[value] || value
const aidTypeTag = (value: AidType) =>
  ({ request: 'warning', offer: 'success', volunteer: 'primary', resource_share: 'info' })[value] as
    'warning' | 'success' | 'primary' | 'info'
const statusTag = (value: AidStatus) =>
  ({ open: 'success', in_progress: 'warning', resolved: 'primary', closed: 'info' })[value] as
    'success' | 'warning' | 'primary' | 'info'
const formatTime = (value?: string) => (value ? new Date(value).toLocaleString() : '')

const load = async () => {
  loading.value = true
  try {
    const response: any = await mutualAidApi.list({
      page: page.value,
      page_size: pageSize,
      keyword: keyword.value || undefined,
      category_id: categoryID.value || undefined,
    })
    items.value = response?.data?.items || []
    total.value = response?.data?.pagination?.total || 0
  } catch (error: any) {
    if (error?.code === 50301) enabled.value = false
    else ElMessage.error(error?.msg || '加载校园互助失败')
  } finally {
    loading.value = false
  }
}

const loadCategories = async () => {
  try {
    const response: any = await categoryApi.list()
    const rows = response?.data?.items || response?.data || []
    categories.value = rows.filter((item: any) => !item.is_closed && (item.node_kind || 'board') === 'board')
  } catch {
    categories.value = []
  }
}

const resetAndLoad = () => {
  page.value = 1
  void load()
}

onMounted(async () => {
  try {
    const response: any = await mutualAidApi.status()
    enabled.value = response?.data?.enabled !== false
  } catch (error: any) {
    enabled.value = error?.code !== 50301
  }
  await Promise.all([loadCategories(), enabled.value ? load() : Promise.resolve()])
})
</script>

<style scoped>
.mutual-aid-list {
  display: grid;
  gap: 16px;
  max-width: 1080px;
  margin: 0 auto;
}
.page-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid #dcdfe6;
}
.page-heading h2,
.page-heading p {
  margin: 0;
}
.page-heading p {
  margin-top: 8px;
  color: var(--campus-muted-color, #606266);
  line-height: 1.6;
}
.filter-band {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(180px, 260px);
  gap: 12px;
}
.aid-feed {
  border-top: 1px solid #ebeef5;
}
.aid-item {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 4px;
  border-bottom: 1px solid #ebeef5;
}
.aid-item-main {
  min-width: 0;
}
.tag-row,
.meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.aid-title {
  display: inline-block;
  margin-top: 10px;
  color: var(--campus-brand-color, #174ea6);
  font-size: 18px;
  font-weight: 650;
  text-decoration: none;
}
.aid-title:hover {
  text-decoration: underline;
}
.aid-item p {
  margin: 8px 0;
  color: var(--campus-text-color, #303133);
  line-height: 1.65;
  white-space: pre-wrap;
}
.meta-row {
  color: var(--campus-muted-color, #909399);
  font-size: 13px;
}
.pagination {
  display: flex;
  justify-content: center;
  padding: 20px 0;
}
@media (max-width: 640px) {
  .page-heading {
    flex-direction: column;
  }
  .filter-band {
    grid-template-columns: 1fr;
  }
  .aid-item {
    gap: 8px;
  }
  .aid-title {
    font-size: 16px;
  }
}
</style>
