<template>
  <div class="listing-detail" v-loading="loading">
    <section v-if="result" class="detail-heading">
      <div>
        <div class="tag-row">
          <el-tag :type="conditionTag(detail.item_condition)" effect="plain">{{
            conditionLabel(detail.item_condition)
          }}</el-tag>
          <el-tag :type="statusTag(detail.trade_status)">{{ statusLabel(detail.trade_status) }}</el-tag>
          <el-tag v-if="thread.is_pinned" type="danger">置顶</el-tag>
        </div>
        <h2>{{ thread.title }}</h2>
        <div class="meta-row">
          <span>{{ thread.author_name || '校园用户' }}</span>
          <span>{{ formatTime(thread.created_at) }}</span>
          <span>{{ thread.view_count || 0 }} 浏览</span>
          <span>{{ thread.reply_count || 0 }} 回复</span>
        </div>
      </div>
      <div class="heading-actions">
        <router-link v-if="isOwner" :to="'/secondhand/' + thread.id + '/edit'"><el-button>编辑</el-button></router-link>
        <router-link to="/secondhand"><el-button>返回列表</el-button></router-link>
      </div>
    </section>

    <section v-if="result" class="detail-grid">
      <article class="content-panel">
        <h3>物品说明</h3>
        <article
          v-if="thread.content_format === 'safe_html'"
          class="content content-rich"
          v-html="thread.content"
        ></article>
        <p v-else class="content">{{ thread.content }}</p>
        <div v-if="thread.tags?.length" class="tag-row">
          <el-tag v-for="tag in thread.tags" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
        </div>
      </article>
      <aside class="information-panel">
        <h3>交易信息</h3>
        <strong class="price">{{ formatPrice(detail.price_minor) }}</strong>
        <dl>
          <div>
            <dt>当前状态</dt>
            <dd>{{ statusLabel(detail.trade_status) }}</dd>
          </div>
          <div>
            <dt>物品成色</dt>
            <dd>{{ conditionLabel(detail.item_condition) }}</dd>
          </div>
          <div>
            <dt>交易方式</dt>
            <dd>{{ methodLabel(detail.trade_method) }}</dd>
          </div>
          <div>
            <dt>大致位置</dt>
            <dd>{{ detail.location_scope || '未说明' }}</dd>
          </div>
        </dl>
        <div v-if="isOwner && nextStatuses.length" class="status-actions">
          <span>更新交易状态</span>
          <el-button
            v-for="status in nextStatuses"
            :key="status"
            size="small"
            :loading="statusSaving"
            @click="updateStatus(status)"
          >
            {{ statusLabel(status) }}
          </el-button>
        </div>
      </aside>
    </section>

    <section v-if="result" class="discussion-band">
      <div>
        <h3>联系与交易约定</h3>
        <p>通过帖子回复补充物品信息或约定交易。CampusOS 不处理付款、担保、订单或争议仲裁。</p>
      </div>
      <router-link :to="'/threads/' + thread.id"><el-button type="primary">查看回复</el-button></router-link>
    </section>

    <el-empty v-if="!loading && !result" description="二手信息不存在或暂不可用" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/modules/identity/store'
import { secondhandApi, type ItemCondition, type SecondhandResult, type TradeMethod, type TradeStatus } from '../api'

const route = useRoute()
const userStore = useUserStore()
const loading = ref(false)
const statusSaving = ref(false)
const result = ref<SecondhandResult | null>(null)
const thread = computed(() => result.value?.thread || {})
const detail = computed(() => result.value?.detail || ({} as SecondhandResult['detail']))
const isOwner = computed(() => Boolean(result.value && userStore.user?.id === thread.value.author_id))
const nextStatuses = computed<TradeStatus[]>(() => {
  const current = detail.value.trade_status
  if (current === 'available') return ['reserved', 'sold', 'closed']
  if (current === 'reserved') return ['available', 'sold', 'closed']
  return []
})

const conditionLabel = (value: ItemCondition) =>
  ({ new: '全新', like_new: '近新', good: '良好', fair: '一般' })[value] || value
const methodLabel = (value: TradeMethod) =>
  ({ in_person: '当面交易', campus_dropoff: '校内送达', other: '其他自行约定' })[value] || value
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
    const id = String(route.params.id)
    const response: any = userStore.isLoggedIn
      ? await secondhandApi.getMine(id).catch(() => secondhandApi.get(id))
      : await secondhandApi.get(id)
    result.value = response?.data || null
  } catch (error: any) {
    result.value = null
    ElMessage.error(error?.msg || '加载二手信息失败')
  } finally {
    loading.value = false
  }
}

const updateStatus = async (status: TradeStatus) => {
  if (!result.value) return
  statusSaving.value = true
  try {
    const response: any = await secondhandApi.updateStatus(String(route.params.id), {
      trade_status: status,
      version: result.value.detail.version,
    })
    result.value = response?.data || result.value
    ElMessage.success('交易状态已更新')
  } catch (error: any) {
    ElMessage.error(error?.msg || '更新交易状态失败，请刷新后重试')
  } finally {
    statusSaving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.listing-detail {
  max-width: 1080px;
  margin: 0 auto;
  display: grid;
  gap: 18px;
}
.detail-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 18px;
  padding-bottom: 16px;
  border-bottom: 1px solid #dcdfe6;
}
.detail-heading h2 {
  margin: 10px 0 8px;
  font-size: 26px;
}
.heading-actions,
.tag-row,
.meta-row,
.status-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.meta-row {
  color: var(--campus-muted-color, #909399);
  font-size: 13px;
}
.detail-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(250px, 330px);
  gap: 18px;
}
.content-panel,
.information-panel,
.discussion-band {
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: var(--campus-surface-background, #fff);
}
.content-panel,
.information-panel {
  padding: 20px;
}
.content-panel h3,
.information-panel h3,
.discussion-band h3,
.discussion-band p {
  margin: 0;
}
.content {
  min-height: 160px;
  white-space: pre-wrap;
  line-height: 1.75;
  color: var(--campus-text-color, #303133);
}
.content-rich {
  white-space: normal;
  overflow-wrap: anywhere;
}
.content-rich :deep(img) {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 16px auto;
  border-radius: 6px;
}
.content-rich :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  border-left: 4px solid var(--el-border-color);
  background: var(--el-fill-color-light);
}
.content + .tag-row {
  margin-top: 16px;
}
.price {
  display: block;
  margin: 16px 0 4px;
  color: var(--campus-brand-color, #174ea6);
  font-size: 25px;
}
dl {
  margin: 16px 0;
  display: grid;
  gap: 12px;
}
dl div {
  display: grid;
  grid-template-columns: 82px minmax(0, 1fr);
  gap: 8px;
}
dt {
  color: var(--campus-muted-color, #909399);
}
dd {
  margin: 0;
  word-break: break-word;
}
.status-actions {
  padding-top: 14px;
  border-top: 1px solid #ebeef5;
}
.status-actions span {
  width: 100%;
  color: var(--campus-muted-color, #606266);
  font-size: 13px;
}
.discussion-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px;
}
.discussion-band p {
  margin-top: 8px;
  color: var(--campus-muted-color, #606266);
  line-height: 1.6;
}
@media (max-width: 720px) {
  .detail-heading,
  .discussion-band {
    flex-direction: column;
  }
  .detail-grid {
    grid-template-columns: 1fr;
  }
  .detail-heading h2 {
    font-size: 22px;
  }
}
</style>
