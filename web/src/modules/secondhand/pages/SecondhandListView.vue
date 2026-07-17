<template>
  <div class="secondhand-list" v-loading="loading">
    <section class="page-heading">
      <div>
        <h2>校园二手</h2>
        <p>浏览校内闲置物品，线下交易前请自行确认物品和联系信息。</p>
      </div>
      <router-link v-if="userStore.isLoggedIn" to="/secondhand/create">
        <el-button type="primary">发布二手信息</el-button>
      </router-link>
    </section>

    <el-alert
      v-if="!enabled"
      type="warning"
      :closable="false"
      show-icon
      title="校园二手功能暂未启用"
      description="当前不能浏览或发布二手信息。"
    />

    <template v-else>
      <section class="filter-band">
        <el-input v-model="keyword" clearable placeholder="搜索标题、正文或标签" @keyup.enter="resetAndLoad">
          <template #append><el-button @click="resetAndLoad">搜索</el-button></template>
        </el-input>
        <el-select v-model="categoryID" clearable placeholder="全部板块" @change="resetAndLoad">
          <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
        </el-select>
      </section>

      <section class="listing-feed">
        <article v-for="item in items" :key="item.thread.id" class="listing-item">
          <div class="listing-main">
            <div class="tag-row">
              <el-tag :type="conditionTag(item.detail.item_condition)" effect="plain">{{
                conditionLabel(item.detail.item_condition)
              }}</el-tag>
              <el-tag :type="statusTag(item.detail.trade_status)">{{ statusLabel(item.detail.trade_status) }}</el-tag>
            </div>
            <router-link class="listing-title" :to="'/secondhand/' + item.thread.id">{{
              item.thread.title
            }}</router-link>
            <p>{{ item.thread.content }}</p>
            <div class="meta-row">
              <span>{{ item.thread.author_name || '校园用户' }}</span>
              <span>{{ item.detail.location_scope || '未说明地点' }}</span>
              <span>{{ formatTime(item.thread.created_at) }}</span>
            </div>
          </div>
          <strong class="price">{{ formatPrice(item.detail.price_minor) }}</strong>
        </article>
        <el-empty v-if="!loading && items.length === 0" description="暂时没有匹配的二手信息" />
      </section>

      <el-pagination
        v-if="total > pageSize"
        class="pagination"
        background
        layout="prev, pager, next"
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        @current-change="changePage"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/modules/identity/store'
import { categoryApi } from '@/modules/community/api'
import { secondhandApi, type ItemCondition, type SecondhandResult, type TradeStatus } from '../api'

const userStore = useUserStore()
const loading = ref(false)
const enabled = ref(true)
const items = ref<SecondhandResult[]>([])
const categories = ref<Array<{ id: string; name: string; is_closed?: boolean; node_kind?: string }>>([])
const keyword = ref('')
const categoryID = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)

const conditionLabel = (value: ItemCondition) =>
  ({ new: '全新', like_new: '近新', good: '良好', fair: '一般' })[value] || value
const statusLabel = (value: TradeStatus) =>
  ({ available: '在售', reserved: '已预留', sold: '已售出', closed: '已关闭' })[value] || value
const conditionTag = (value: ItemCondition) =>
  ({ new: 'success', like_new: 'primary', good: 'info', fair: 'warning' })[value] as
    'success' | 'primary' | 'info' | 'warning'
const statusTag = (value: TradeStatus) =>
  ({ available: 'success', reserved: 'warning', sold: 'primary', closed: 'info' })[value] as
    'success' | 'warning' | 'primary' | 'info'
const formatTime = (value?: string) => (value ? new Date(value).toLocaleString() : '')
const formatPrice = (minor: number) =>
  new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY', minimumFractionDigits: 2 }).format(
    (minor || 0) / 100,
  )

const load = async () => {
  loading.value = true
  try {
    const response: any = await secondhandApi.list({
      page: page.value,
      page_size: pageSize,
      keyword: keyword.value || undefined,
      category_id: categoryID.value || undefined,
    })
    items.value = response?.data?.items || []
    total.value = response?.data?.pagination?.total || 0
  } catch (error: any) {
    if (error?.code === 50301) enabled.value = false
    else ElMessage.error(error?.msg || '加载校园二手失败')
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

const changePage = (value: number) => {
  page.value = value
  void load()
}

onMounted(async () => {
  try {
    const response: any = await secondhandApi.status()
    enabled.value = response?.data?.enabled !== false
  } catch (error: any) {
    enabled.value = error?.code !== 50301
  }
  await Promise.all([loadCategories(), enabled.value ? load() : Promise.resolve()])
})
</script>

<style scoped>
.secondhand-list {
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
.listing-feed {
  border-top: 1px solid #ebeef5;
}
.listing-item {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 4px;
  border-bottom: 1px solid #ebeef5;
}
.listing-main {
  min-width: 0;
}
.tag-row,
.meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.listing-title {
  display: inline-block;
  margin-top: 10px;
  color: var(--campus-brand-color, #174ea6);
  font-size: 18px;
  font-weight: 650;
  text-decoration: none;
}
.listing-title:hover {
  text-decoration: underline;
}
.listing-item p {
  margin: 8px 0;
  color: var(--campus-text-color, #303133);
  line-height: 1.65;
  white-space: pre-wrap;
}
.meta-row {
  color: var(--campus-muted-color, #909399);
  font-size: 13px;
}
.price {
  color: var(--campus-brand-color, #174ea6);
  font-size: 18px;
  white-space: nowrap;
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
  .listing-item {
    gap: 8px;
  }
  .listing-title {
    font-size: 16px;
  }
  .price {
    font-size: 16px;
  }
}
</style>
