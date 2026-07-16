<template>
  <div class="admin-reviews">
    <header class="page-header">
      <div>
        <p class="eyebrow">Review queue</p>
        <h2>内容审核</h2>
        <p>审核只处理待审核内容。拒绝后作者需要整改并重新提交，不能通过编辑直接恢复公开。</p>
      </div>
      <el-button :icon="Refresh" circle title="刷新审核队列" aria-label="刷新审核队列" @click="loadThreads" />
    </header>

    <section class="review-panel">
      <el-tabs v-model="activeTab" @tab-change="loadThreads">
        <el-tab-pane label="待审核" name="pending">
          <template #label>待审核 <el-tag size="small" type="warning">{{ total }}</el-tag></template>
        </el-tab-pane>
        <el-tab-pane label="已拒绝" name="rejected" />
        <el-tab-pane label="已下架" name="taken_down" />
      </el-tabs>

      <div class="table-wrap">
        <el-table :data="threads" v-loading="loading" stripe border class="review-table">
          <el-table-column prop="id" label="ID" width="160" show-overflow-tooltip />
          <el-table-column prop="title" label="标题" min-width="260" show-overflow-tooltip />
          <el-table-column prop="author_name" label="作者" width="120" />
          <el-table-column prop="category_id" label="版块" width="120" />
          <el-table-column label="内容类型" width="100">
            <template #default="{ row }"><el-tag size="small" effect="plain">{{ row.content_format === 'richtext_article' ? '图文' : '普通' }}</el-tag></template>
          </el-table-column>
          <el-table-column label="原因" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">{{ row.moderation_reason || '-' }}</template>
          </el-table-column>
          <el-table-column label="提交时间" width="180"><template #default="{ row }">{{ formatDate(row.updated_at || row.created_at) }}</template></el-table-column>
          <el-table-column label="操作" width="220" fixed="right">
            <template #default="{ row }">
              <div class="row-actions">
                <el-button type="info" plain size="small" :icon="View" @click="viewDetail(row)">查看</el-button>
                <template v-if="activeTab === 'pending'">
                  <el-button type="success" plain size="small" :icon="Check" @click="openDecision(row, 'approve')">通过</el-button>
                  <el-button type="danger" plain size="small" :icon="Close" @click="openDecision(row, 'reject')">拒绝</el-button>
                </template>
                <el-button v-else type="success" plain size="small" @click="openDecision(row, 'restore')">直接恢复</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <el-empty v-if="!loading && threads.length === 0" description="当前队列没有内容" />
      <div class="pagination"><el-pagination v-model:current-page="page" :page-size="30" :total="total" layout="total, prev, pager, next" @current-change="loadThreads" /></div>
    </section>

    <el-dialog v-model="detailVisible" title="审核内容" width="min(760px, calc(100vw - 24px))" destroy-on-close>
      <template v-if="selectedThread">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="标题" :span="2">{{ selectedThread.title }}</el-descriptions-item>
          <el-descriptions-item label="作者">{{ selectedThread.author_name }}</el-descriptions-item>
          <el-descriptions-item label="版块">{{ selectedThread.category_id }}</el-descriptions-item>
          <el-descriptions-item label="当前状态" :span="2">{{ statusLabel(selectedThread.moderation_status) }}</el-descriptions-item>
          <el-descriptions-item label="已有原因" :span="2">{{ selectedThread.moderation_reason || '-' }}</el-descriptions-item>
        </el-descriptions>
        <pre class="thread-content">{{ selectedThread.content }}</pre>
      </template>
      <template #footer><el-button @click="detailVisible = false">关闭</el-button></template>
    </el-dialog>

    <el-dialog v-model="decisionVisible" :title="decisionTitle" width="min(500px, calc(100vw - 24px))" destroy-on-close>
      <p class="dialog-copy">{{ decisionCopy }}</p>
      <el-input v-model="decisionReason" type="textarea" :rows="4" maxlength="500" show-word-limit placeholder="填写审核说明或整改原因" />
      <template #footer>
        <el-button @click="decisionVisible = false">取消</el-button>
        <el-button :type="decisionAction === 'reject' ? 'danger' : 'primary'" :loading="deciding" @click="submitDecision">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, Close, Refresh, View } from '@element-plus/icons-vue'
import { threadApi } from '@/modules/community/api'

type Decision = 'approve' | 'reject' | 'restore'
const activeTab = ref<'pending' | 'rejected' | 'taken_down'>('pending')
const threads = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const detailVisible = ref(false)
const selectedThread = ref<any>(null)
const decisionVisible = ref(false)
const decisionAction = ref<Decision | null>(null)
const decisionReason = ref('')
const deciding = ref(false)

const tabStatus = () => activeTab.value
const dataOf = (value: any) => value?.data ?? value
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatDate = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '-'
const statusLabel = (value?: string) => ({ pending: '待审核', rejected: '已拒绝', taken_down: '已下架' }[value || ''] || '-')
const decisionTitle = () => ({ approve: '通过审核', reject: '拒绝并要求整改', restore: '直接恢复内容' }[decisionAction.value || 'approve'])
const decisionCopy = () => ({ approve: '通过后内容会按作者原有发布意图恢复可见性。', reject: '拒绝会保留内容和原因，作者修改后必须重新提交审核。', restore: '这是高风险操作，会跳过作者整改流程。' }[decisionAction.value || 'approve'])

const loadThreads = async () => {
  loading.value = true
  try {
    const result: any = await threadApi.list({ page: page.value, page_size: 30, moderation_status: tabStatus() })
    const data = dataOf(result)
    threads.value = data?.items || []
    total.value = data?.pagination?.total || 0
  } catch (error: any) {
    ElMessage.error(messageOf(error, '加载审核队列失败'))
  } finally { loading.value = false }
}
const viewDetail = (thread: any) => { selectedThread.value = thread; detailVisible.value = true }
const openDecision = (thread: any, action: Decision) => {
  selectedThread.value = thread
  decisionAction.value = action
  decisionReason.value = action === 'approve' ? '审核通过' : ''
  decisionVisible.value = true
}
const submitDecision = async () => {
  const action = decisionAction.value
  const thread = selectedThread.value
  const reason = decisionReason.value.trim()
  if (!action || !thread) return
  if (!reason) { ElMessage.warning('请填写审核说明'); return }
  deciding.value = true
  try {
    if (action === 'approve') await threadApi.approve(thread.id, reason)
    if (action === 'reject') await threadApi.reject(thread.id, reason)
    if (action === 'restore') await threadApi.directRestore(thread.id, reason)
    decisionVisible.value = false
    ElMessage.success('审核操作已完成')
    await loadThreads()
  } catch (error: any) { ElMessage.error(messageOf(error, '审核操作失败')) }
  finally { deciding.value = false }
}
onMounted(loadThreads)
</script>

<style scoped>
.admin-reviews { display: grid; gap: 16px; max-width: 1500px; }.page-header, .review-panel { border: 1px solid #e4e7ed; border-radius: 6px; background: #fff; }.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 20px; }.page-header h2 { margin: 0; }.page-header p:last-child { margin: 7px 0 0; color: #606266; line-height: 1.6; }.eyebrow { margin: 0 0 6px; color: #a16207; font-size: 12px; font-weight: 700; text-transform: uppercase; }.review-panel { padding: 18px; }.row-actions { display: flex; gap: 6px; flex-wrap: wrap; }.pagination { display: flex; justify-content: flex-end; margin-top: 14px; }.thread-content { max-height: 360px; margin: 16px 0 0; overflow: auto; padding: 14px; border: 1px solid #ebeef5; border-radius: 6px; background: #fafafa; color: #303133; font-family: inherit; line-height: 1.7; white-space: pre-wrap; word-break: break-word; }.dialog-copy { color: #606266; line-height: 1.6; }
@media (max-width: 760px) { .page-header, .review-panel { padding: 14px; }.table-wrap { overflow-x: auto; }.review-table { min-width: 1050px; }.pagination { justify-content: center; } }
</style>
