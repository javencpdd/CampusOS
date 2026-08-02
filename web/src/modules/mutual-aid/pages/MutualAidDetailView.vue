<template>
  <div class="aid-detail" v-loading="loading">
    <section v-if="result" class="detail-heading">
      <div>
        <div class="tag-row">
          <el-tag :type="aidTypeTag(detail.aid_type)" effect="plain">{{ aidTypeLabel(detail.aid_type) }}</el-tag>
          <el-tag :type="statusTag(detail.aid_status)">{{ statusLabel(detail.aid_status) }}</el-tag>
          <el-tag v-if="thread.is_pinned" type="danger">置顶</el-tag>
        </div>
        <h2>{{ thread.title }}</h2>
        <div class="meta-row">
          <span>{{ thread.author_name || '校园用户' }}</span>
          <span>{{ formatTime(thread.created_at) }}</span>
          <span>{{ thread.view_count || 0 }} 浏览</span>
          <span>{{ thread.reply_count || 0 }} 回复</span>
        </div>
        <ThreadTaxonomy :category-id="thread.category_id" :tags="thread.tags" />
      </div>
      <div class="heading-actions">
        <router-link v-if="isOwner" :to="`/mutual-aid/${thread.id}/edit`"><el-button>编辑</el-button></router-link>
        <router-link to="/mutual-aid"><el-button>返回列表</el-button></router-link>
      </div>
    </section>

    <section v-if="result" class="detail-grid">
      <article class="content-panel">
        <h3>互助说明</h3>
        <article
          v-if="thread.content_format === 'safe_html'"
          class="content content-rich"
          v-html="thread.content"
        ></article>
        <p v-else class="content">{{ thread.content }}</p>
      </article>
      <aside class="information-panel">
        <h3>信息状态</h3>
        <dl>
          <div>
            <dt>当前状态</dt>
            <dd>{{ statusLabel(detail.aid_status) }}</dd>
          </div>
          <div>
            <dt>大致位置</dt>
            <dd>{{ detail.location_scope || '未说明' }}</dd>
          </div>
          <div>
            <dt>截止时间</dt>
            <dd>{{ detail.deadline ? formatTime(detail.deadline) : '未设置' }}</dd>
          </div>
          <div>
            <dt>联系方式</dt>
            <dd>{{ contactLabel(detail.contact_mode) }}</dd>
          </div>
        </dl>
        <div v-if="isOwner && nextStatuses.length" class="status-actions">
          <span>更新状态</span>
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
        <h3>交流与跟进</h3>
        <p>通过帖子回复补充进展或联系约定。状态变更只由发布者操作，版主仍可按板块规则治理帖子。</p>
      </div>
      <router-link :to="`/threads/${thread.id}`"><el-button type="primary">查看回复</el-button></router-link>
    </section>

    <el-empty v-if="!loading && !result" description="互助信息不存在或暂不可用" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import ThreadTaxonomy from '@/modules/community/components/ThreadTaxonomy.vue'
import { useUserStore } from '@/modules/identity/store'
import { mutualAidApi, type AidStatus, type AidType, type ContactMode, type MutualAidResult } from '../api'

const route = useRoute()
const userStore = useUserStore()
const loading = ref(false)
const statusSaving = ref(false)
const result = ref<MutualAidResult | null>(null)
const thread = computed(() => result.value?.thread || {})
const detail = computed(() => result.value?.detail || ({} as MutualAidResult['detail']))
const isOwner = computed(() => Boolean(result.value && userStore.user?.id === thread.value.author_id))
const nextStatuses = computed<AidStatus[]>(() => {
  const current = detail.value.aid_status
  if (current === 'open') return ['in_progress', 'resolved', 'closed']
  if (current === 'in_progress') return ['open', 'resolved', 'closed']
  if (current === 'resolved') return ['closed']
  return []
})

const aidTypeLabel = (value: AidType) =>
  ({ request: '求助', offer: '提供帮助', volunteer: '志愿服务', resource_share: '资源共享' })[value] || value
const statusLabel = (value: AidStatus) =>
  ({ open: '开放中', in_progress: '处理中', resolved: '已解决', closed: '已关闭' })[value] || value
const contactLabel = (value: ContactMode) =>
  ({ comment: '评论区联系', in_app: '站内联系', email: '邮箱联系', other: '其他约定方式' })[value] || value
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
    const id = String(route.params.id)
    const response: any = userStore.isLoggedIn
      ? await mutualAidApi.getMine(id).catch(() => mutualAidApi.get(id))
      : await mutualAidApi.get(id)
    result.value = response?.data || null
  } catch (error: any) {
    result.value = null
    ElMessage.error(error?.msg || '加载互助信息失败')
  } finally {
    loading.value = false
  }
}

const updateStatus = async (status: AidStatus) => {
  if (!result.value) return
  statusSaving.value = true
  try {
    const response: any = await mutualAidApi.updateStatus(String(route.params.id), {
      aid_status: status,
      version: result.value.detail.version,
    })
    result.value = response?.data || result.value
    ElMessage.success('互助状态已更新')
  } catch (error: any) {
    ElMessage.error(error?.msg || '更新互助状态失败，请刷新后重试')
  } finally {
    statusSaving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.aid-detail {
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
