<template>
  <div class="admin-threads">
    <header class="page-header">
      <div>
        <p class="eyebrow">Content governance</p>
        <h2>帖子治理</h2>
        <p>下架、回收站和永久清除是不同操作。下架保留作者内容并允许整改重提，回收站可恢复，永久清除不可恢复。</p>
      </div>
      <el-button :icon="Refresh" circle title="刷新" aria-label="刷新帖子列表" @click="load" />
    </header>

    <section class="governance-panel">
      <div class="filters" aria-label="帖子筛选">
        <el-segmented v-model="listMode" :options="listModes" @change="changeListMode" />
        <el-select v-if="listMode === 'active'" v-model="filterModeration" placeholder="治理状态" clearable @change="load">
          <el-option label="待审核" value="pending" />
          <el-option label="已下架" value="taken_down" />
          <el-option label="已拒绝" value="rejected" />
          <el-option label="治理通过" value="clear" />
        </el-select>
        <el-select v-if="listMode === 'active'" v-model="filterPublication" placeholder="发布意图" clearable @change="load">
          <el-option label="公开" value="published" />
          <el-option label="草稿" value="draft" />
          <el-option label="私密" value="private" />
        </el-select>
        <el-select v-model="filterContentFormat" placeholder="内容类型" clearable @change="load">
          <el-option label="普通文本" value="markdown" />
          <el-option label="图文文章" value="richtext_article" />
        </el-select>
        <el-select v-model="filterCategory" placeholder="筛选版块" clearable @change="load">
          <el-option v-for="cat in categories" :key="cat.id" :label="cat.name" :value="cat.id" />
        </el-select>
        <el-input v-model="searchKeyword" clearable placeholder="搜索标题或内容" @clear="load" @keyup.enter="load">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
      </div>

      <el-alert
        v-if="listMode === 'trash'"
        type="warning"
        :closable="false"
        show-icon
        title="回收站内容不会公开。恢复操作和永久清除都需要填写原因；永久清除后无法恢复。"
      />

      <div class="batch-toolbar" aria-label="帖子批量操作">
        <span class="batch-summary">
          已选择 <strong>{{ selectedRows.length }}</strong> 项
          <small>勾选表头复选框可全选当前页</small>
        </span>
        <el-select v-model="batchAction" class="batch-action-select" placeholder="选择批量操作" :disabled="selectedRows.length === 0">
          <el-option v-for="option in batchActionOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <el-button type="primary" :disabled="selectedRows.length === 0 || !batchAction" :loading="operating" @click="startBatchAction">执行</el-button>
        <el-button text :disabled="selectedRows.length === 0" @click="clearSelection">清除选择</el-button>
      </div>

      <div class="table-wrap">
        <el-table ref="tableRef" :data="threads" row-key="id" v-loading="loading" stripe border class="thread-table" @selection-change="handleSelectionChange">
          <el-table-column type="selection" width="48" fixed="left" />
          <el-table-column prop="id" label="ID" width="150" show-overflow-tooltip />
          <el-table-column prop="title" label="内容" min-width="260" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="thread-title">
                <div class="thread-badges">
                  <el-tag v-if="row.is_pinned" type="warning" size="small">置顶</el-tag>
                  <el-tag v-if="row.is_locked" type="info" size="small">锁定</el-tag>
                  <el-tag :type="row.content_format === 'richtext_article' ? 'success' : 'info'" size="small" effect="plain">
                    {{ row.content_format === 'richtext_article' ? '图文' : '普通' }}
                  </el-tag>
                </div>
                <span>{{ row.title }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="author_name" label="作者" width="110" show-overflow-tooltip />
          <el-table-column label="发布" width="100" align="center">
            <template #default="{ row }"><el-tag size="small" effect="plain" :type="publicationType(row.publication_status)">{{ publicationLabel(row.publication_status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="治理" width="100" align="center">
            <template #default="{ row }"><el-tag size="small" :type="moderationType(row.moderation_status)">{{ moderationLabel(row.moderation_status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="删除" width="100" align="center">
            <template #default="{ row }"><el-tag size="small" :type="deletionType(row.deletion_status)">{{ deletionLabel(row.deletion_status) }}</el-tag></template>
          </el-table-column>
          <el-table-column prop="view_count" label="浏览" width="76" align="center" />
          <el-table-column label="操作" min-width="330" fixed="right">
            <template #default="{ row }">
              <div class="row-actions">
                <template v-if="listMode === 'active'">
                  <el-button v-if="row.moderation_status === 'pending'" type="success" size="small" plain @click="openReasonDialog(row, 'approve')">通过</el-button>
                  <el-button v-if="row.moderation_status === 'pending'" type="danger" size="small" plain @click="openReasonDialog(row, 'reject')">拒绝</el-button>
                  <el-button v-if="row.moderation_status === 'clear' && row.publication_status === 'published'" type="warning" size="small" plain @click="openReasonDialog(row, 'take_down')">下架</el-button>
                  <el-button v-if="['taken_down', 'rejected'].includes(row.moderation_status)" type="success" size="small" plain @click="openReasonDialog(row, 'direct_restore')">直接恢复</el-button>
                  <el-tooltip :content="row.is_pinned ? '取消置顶' : '置顶'" placement="top">
                    <el-button :type="row.is_pinned ? 'warning' : 'default'" size="small" plain :icon="row.is_pinned ? Bottom : Top" @click="togglePin(row)" />
                  </el-tooltip>
                  <el-tooltip :content="row.is_locked ? '取消锁定' : '锁定'" placement="top">
                    <el-button :type="row.is_locked ? 'info' : 'default'" size="small" plain :icon="row.is_locked ? Unlock : Lock" @click="toggleLock(row)" />
                  </el-tooltip>
                  <el-popconfirm title="将内容移入回收站？作者仍可恢复，公开页面会立即隐藏。" confirm-button-text="移入回收站" cancel-button-text="取消" confirm-button-type="warning" @confirm="moveToTrash(row)">
                    <template #reference><el-button type="warning" size="small" plain :icon="Delete" title="移入回收站" /></template>
                  </el-popconfirm>
                </template>
                <template v-else>
                  <el-button type="success" size="small" plain @click="openReasonDialog(row, 'restore_trash')">恢复内容</el-button>
                  <el-button type="danger" size="small" plain @click="openReasonDialog(row, 'purge')">永久清除</el-button>
                </template>
                <el-button text size="small" :icon="List" @click="showActions(row)">记录</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="pagination-wrapper">
        <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
      </div>
    </section>

    <el-dialog v-model="reasonDialogVisible" :title="reasonDialogTitle()" width="min(520px, calc(100vw - 24px))" destroy-on-close>
      <p class="dialog-copy">{{ reasonDialogCopy() }}</p>
      <el-input v-model="reason" type="textarea" :rows="4" maxlength="500" show-word-limit placeholder="请填写可用于审计和作者提示的原因" />
      <template #footer>
        <el-button @click="reasonDialogVisible = false">取消</el-button>
        <el-button :type="pendingAction === 'purge' || pendingAction === 'reject' ? 'danger' : 'primary'" :loading="operating" @click="confirmReasonAction">确认</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="actionsDialogVisible" title="治理记录" width="min(800px, calc(100vw - 24px))" destroy-on-close>
      <el-table :data="moderationActions" v-loading="actionsLoading" size="small" border>
        <el-table-column prop="created_at" label="时间" width="170"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column prop="action" label="操作" width="150" />
        <el-table-column prop="actor_id" label="操作者" width="120" />
        <el-table-column prop="reason" label="原因" min-width="200" show-overflow-tooltip />
        <el-table-column prop="before_state" label="变更前" min-width="150" show-overflow-tooltip />
        <el-table-column prop="after_state" label="变更后" min-width="150" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!actionsLoading && moderationActions.length === 0" description="暂无治理记录" :image-size="58" />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Bottom, Delete, List, Lock, Refresh, Search, Top, Unlock } from '@element-plus/icons-vue'
import { categoryApi, threadApi } from '@/modules/community/api'

type GovernanceAction = 'approve' | 'reject' | 'take_down' | 'direct_restore' | 'restore_trash' | 'purge'
type BatchAction = GovernanceAction | 'pin' | 'unpin' | 'lock' | 'unlock' | 'trash'

const threads = ref<any[]>([])
const categories = ref<any[]>([])
const loading = ref(false)
const operating = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')
const filterCategory = ref('')
const filterModeration = ref('')
const filterPublication = ref('')
const filterContentFormat = ref('')
const listMode = ref<'active' | 'trash'>('active')
const listModes = [{ label: '内容列表', value: 'active' }, { label: '回收站', value: 'trash' }]
const reasonDialogVisible = ref(false)
const reason = ref('')
const pendingAction = ref<GovernanceAction | null>(null)
const selectedThread = ref<any>(null)
const actionsDialogVisible = ref(false)
const actionsLoading = ref(false)
const moderationActions = ref<any[]>([])
const tableRef = ref<any>(null)
const selectedRows = ref<any[]>([])
const batchAction = ref<BatchAction | ''>('')
const batchMode = ref(false)

const publicationLabel = (status?: string) => ({ published: '公开', draft: '草稿', private: '私密' }[status || ''] || '-')
const publicationType = (status?: string) => ({ published: 'success', draft: 'info', private: 'warning' }[status || ''] || 'info')
const moderationLabel = (status?: string) => ({ clear: '通过', pending: '待审核', rejected: '已拒绝', taken_down: '已下架' }[status || ''] || '-')
const moderationType = (status?: string) => ({ clear: 'success', pending: 'warning', rejected: 'danger', taken_down: 'danger' }[status || ''] || 'info')
const deletionLabel = (status?: string) => ({ active: '正常', trashed: '回收站', purged: '已清除' }[status || ''] || '-')
const deletionType = (status?: string) => ({ active: 'success', trashed: 'warning', purged: 'danger' }[status || ''] || 'info')
const actionMeta: Record<GovernanceAction, { title: string; copy: string }> = {
  approve: { title: '通过审核', copy: '填写审核说明。内容会按作者原有发布意图恢复可见性。' },
  reject: { title: '拒绝内容', copy: '填写明确整改原因。作者修改后只能重新提交审核，不能直接公开。' },
  take_down: { title: '下架内容', copy: '填写下架原因。下架不会删除内容，作者可以整改后重新提交审核。' },
  direct_restore: { title: '直接恢复', copy: '这是高风险操作。填写恢复原因后，内容将跳过作者整改直接恢复。' },
  restore_trash: { title: '恢复回收站内容', copy: '填写恢复原因。恢复会保留内容原有的发布与治理状态。' },
  purge: { title: '永久清除内容', copy: '此操作不可恢复。请确认已满足保留要求并填写永久清除原因。' },
}
const batchActionOptions = computed<Array<{ label: string; value: BatchAction }>>(() => listMode.value === 'trash'
  ? [
      { label: '恢复回收站内容', value: 'restore_trash' },
      { label: '永久清除', value: 'purge' },
    ]
  : [
      { label: '置顶', value: 'pin' },
      { label: '取消置顶', value: 'unpin' },
      { label: '锁定', value: 'lock' },
      { label: '解除锁定', value: 'unlock' },
      { label: '审核通过', value: 'approve' },
      { label: '审核拒绝', value: 'reject' },
      { label: '下架', value: 'take_down' },
      { label: '直接恢复', value: 'direct_restore' },
      { label: '移入回收站', value: 'trash' },
    ])
const reasonDialogTitle = () => {
  if (!pendingAction.value) return '治理操作'
  const title = actionMeta[pendingAction.value].title
  return batchMode.value ? `批量${title}（${selectedRows.value.length} 项）` : title
}
const reasonDialogCopy = () => {
  if (!pendingAction.value) return ''
  const copy = actionMeta[pendingAction.value].copy
  return batchMode.value ? `${copy} 本次原因将分别写入每一项内容的治理记录。` : copy
}
const dataOf = (value: any) => value?.data ?? value
const itemsOf = (value: any) => {
  const data = dataOf(value)
  return Array.isArray(data) ? data : data?.items || []
}
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '-'

const handleSelectionChange = (rows: any[]) => { selectedRows.value = rows }
const clearSelection = () => {
  tableRef.value?.clearSelection()
  selectedRows.value = []
}
const changeListMode = () => {
  batchAction.value = ''
  clearSelection()
  page.value = 1
  void load()
}
const load = async () => {
  clearSelection()
  loading.value = true
  try {
    const params: any = { page: page.value, page_size: 20 }
    if (searchKeyword.value) params.keyword = searchKeyword.value
    if (filterCategory.value) params.category_id = filterCategory.value
    if (filterContentFormat.value) params.content_format = filterContentFormat.value
    let response: any
    if (listMode.value === 'trash') {
      response = await threadApi.trash(params)
    } else {
      if (filterModeration.value) params.moderation_status = filterModeration.value
      if (filterPublication.value) params.publication_status = filterPublication.value
      response = await threadApi.list(params)
    }
    const data = dataOf(response)
    threads.value = data?.items || []
    total.value = data?.pagination?.total || 0
  } catch (error: any) {
    ElMessage.error(messageOf(error, '加载帖子列表失败'))
  } finally {
    loading.value = false
  }
}

const loadCategories = async () => {
  try { categories.value = itemsOf(await categoryApi.list()) } catch { categories.value = [] }
}
const togglePin = async (row: any) => {
  try {
    await (row.is_pinned ? threadApi.unpin(row.id) : threadApi.pin(row.id))
    ElMessage.success(row.is_pinned ? '已取消置顶' : '已置顶')
    await load()
  } catch (error: any) { ElMessage.error(messageOf(error, '置顶操作失败')) }
}
const toggleLock = async (row: any) => {
  try {
    await (row.is_locked ? threadApi.unlock(row.id) : threadApi.lock(row.id))
    ElMessage.success(row.is_locked ? '已取消锁定' : '已锁定')
    await load()
  } catch (error: any) { ElMessage.error(messageOf(error, '锁定操作失败')) }
}
const moveToTrash = async (row: any) => {
  try {
    await threadApi.adminDelete(row.id)
    ElMessage.success('内容已移入回收站')
    await load()
  } catch (error: any) { ElMessage.error(messageOf(error, '移入回收站失败')) }
}
const openReasonDialog = (row: any, action: GovernanceAction) => {
  batchMode.value = false
  selectedThread.value = row
  pendingAction.value = action
  reason.value = action === 'approve' ? '审核通过' : ''
  reasonDialogVisible.value = true
}
const executeBatchItem = async (row: any, action: BatchAction, value = '') => {
  if (action === 'pin') return threadApi.pin(row.id)
  if (action === 'unpin') return threadApi.unpin(row.id)
  if (action === 'lock') return threadApi.lock(row.id)
  if (action === 'unlock') return threadApi.unlock(row.id)
  if (action === 'trash') return threadApi.adminDelete(row.id)
  if (action === 'approve') return threadApi.approve(row.id, value)
  if (action === 'reject') return threadApi.reject(row.id, value)
  if (action === 'take_down') return threadApi.takeDown(row.id, value)
  if (action === 'direct_restore') return threadApi.directRestore(row.id, value)
  if (action === 'restore_trash') return threadApi.restoreTrash(row.id, value)
  return threadApi.purge(row.id, value)
}
const runBatchAction = async (action: BatchAction, value = '') => {
  const rows = [...selectedRows.value]
  if (!rows.length) return
  operating.value = true
  let completed = 0
  const failed: Array<{ row: any; message: string }> = []
  try {
    for (const row of rows) {
      try {
        await executeBatchItem(row, action, value)
        completed++
      } catch (error: any) {
        failed.push({ row, message: messageOf(error, '操作失败') })
      }
    }
    if (failed.length === 0) {
      ElMessage.success(`批量操作完成，共处理 ${completed} 项`)
    } else {
      const first = failed[0]
      ElMessage.warning(`批量操作完成 ${completed} 项，失败 ${failed.length} 项。首个失败：${first.row.title || first.row.id} - ${first.message}`)
    }
    reasonDialogVisible.value = false
    batchAction.value = ''
    clearSelection()
    await load()
  } finally {
    operating.value = false
    batchMode.value = false
  }
}
const startBatchAction = async () => {
  const action = batchAction.value
  if (!action || selectedRows.value.length === 0) return
  if (['approve', 'reject', 'take_down', 'direct_restore', 'restore_trash', 'purge'].includes(action)) {
    batchMode.value = true
    selectedThread.value = null
    pendingAction.value = action as GovernanceAction
    reason.value = action === 'approve' ? '批量审核通过' : ''
    reasonDialogVisible.value = true
    return
  }
  try {
    const label = batchActionOptions.value.find((item) => item.value === action)?.label
    await ElMessageBox.confirm(`确定对当前选择的 ${selectedRows.value.length} 项内容执行“${label}”吗？`, '确认批量操作', {
      type: action === 'trash' ? 'warning' : 'info',
      confirmButtonText: '确认执行',
      cancelButtonText: '取消',
    })
    await runBatchAction(action)
  } catch {
    // Cancelling a confirmation is not an operation failure.
  }
}
const confirmReasonAction = async () => {
  const action = pendingAction.value
  const row = selectedThread.value
  const value = reason.value.trim()
  if (!action || (!row && !batchMode.value)) return
  if (!value) { ElMessage.warning('请填写治理原因'); return }
  if (batchMode.value) {
    await runBatchAction(action, value)
    return
  }
  operating.value = true
  try {
    if (action === 'approve') await threadApi.approve(row.id, value)
    if (action === 'reject') await threadApi.reject(row.id, value)
    if (action === 'take_down') await threadApi.takeDown(row.id, value)
    if (action === 'direct_restore') await threadApi.directRestore(row.id, value)
    if (action === 'restore_trash') await threadApi.restoreTrash(row.id, value)
    if (action === 'purge') await threadApi.purge(row.id, value)
    reasonDialogVisible.value = false
    ElMessage.success(`${actionMeta[action].title}已完成`)
    await load()
  } catch (error: any) {
    ElMessage.error(messageOf(error, `${actionMeta[action].title}失败`))
  } finally { operating.value = false }
}
const showActions = async (row: any) => {
  actionsDialogVisible.value = true
  actionsLoading.value = true
  moderationActions.value = []
  try { moderationActions.value = itemsOf(await threadApi.moderationActions(row.id)) }
  catch (error: any) { ElMessage.error(messageOf(error, '加载治理记录失败')) }
  finally { actionsLoading.value = false }
}

onMounted(() => { void load(); void loadCategories() })
</script>

<style scoped>
.admin-threads { display: grid; gap: 16px; max-width: 1500px; }
.page-header, .governance-panel { border: 1px solid #e4e7ed; border-radius: 6px; background: #fff; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 20px; }
.page-header h2 { margin: 0; }.page-header p:last-child { margin: 7px 0 0; color: #606266; line-height: 1.6; }.eyebrow { margin: 0 0 6px; color: #b45309; font-size: 12px; font-weight: 700; text-transform: uppercase; }
.governance-panel { display: grid; gap: 14px; padding: 18px; }.filters { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }.filters .el-select { width: 150px; }.filters .el-input { width: min(260px, 100%); }.table-wrap { min-width: 0; }.thread-table { width: 100%; }.thread-title { display: grid; gap: 5px; min-width: 0; }.thread-badges, .row-actions { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }.pagination-wrapper { display: flex; justify-content: flex-end; }.dialog-copy { margin: 0 0 12px; color: #606266; line-height: 1.6; }
.batch-toolbar { min-height: 52px; padding: 10px 12px; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; border: 1px solid #dcdfe6; background: #f8fafc; }
.batch-summary { margin-right: auto; display: inline-flex; align-items: baseline; gap: 5px; color: #606266; }.batch-summary strong { color: #303133; }.batch-summary small { margin-left: 6px; color: #909399; }.batch-action-select { width: 180px; }
@media (max-width: 760px) { .page-header { padding: 14px; }.governance-panel { padding: 12px; }.filters { align-items: stretch; }.filters :deep(.el-segmented), .filters .el-select, .filters .el-input { width: 100%; }.batch-toolbar { align-items: stretch; }.batch-summary { width: 100%; flex-wrap: wrap; }.batch-action-select { width: 100%; }.table-wrap { overflow-x: auto; }.thread-table { min-width: 1040px; }.pagination-wrapper { justify-content: center; } }
</style>
