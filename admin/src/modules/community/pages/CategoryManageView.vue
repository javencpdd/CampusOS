<template>
  <main class="admin-categories">
    <header class="page-header">
      <div>
        <h1>版块管理</h1>
      </div>
      <div class="page-actions">
        <el-tooltip content="重新加载版块树">
          <el-button circle :icon="Refresh" aria-label="重新加载版块树" @click="load" />
        </el-tooltip>
        <el-button type="primary" :icon="FolderAdd" @click="showCreateDialog('group')">新建分组</el-button>
        <el-button type="primary" :icon="Plus" @click="showCreateDialog('board')">新建版块</el-button>
      </div>
    </header>

    <el-alert
      v-if="archivedCount"
      class="archive-alert"
      type="info"
      :closable="false"
      show-icon
      :title="`当前有 ${archivedCount} 个已归档节点；归档节点不会出现在用户端导航或发帖选择中。`"
    />

    <el-table
      v-loading="loading"
      :data="categoryTree"
      row-key="id"
      default-expand-all
      :tree-props="{ children: 'children' }"
      class="category-tree"
      table-layout="fixed"
    >
      <el-table-column label="层级与名称" min-width="250">
        <template #default="{ row }">
          <div class="category-name">
            <span class="category-icon" aria-hidden="true">{{ row.icon || (row.node_kind === 'group' ? '◫' : '▣') }}</span>
            <span class="category-name-text">{{ row.name }}</span>
            <el-tag size="small" :type="row.node_kind === 'group' ? 'info' : 'success'">
              {{ row.node_kind === 'group' ? '分组' : '版块' }}
            </el-tag>
            <el-tag v-if="row.lifecycle_status === 'archived'" size="small" type="warning">已归档</el-tag>
            <el-tag v-else-if="row.node_kind === 'board' && row.is_closed" size="small" type="danger">已关闭发帖</el-tag>
          </div>
          <span class="category-slug">{{ row.slug }}</span>
        </template>
      </el-table-column>
      <el-table-column label="默认标签" min-width="170">
        <template #default="{ row }">
          <div v-if="row.default_tags?.length" class="tag-list">
            <el-tag v-for="tag in row.default_tags" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
          </div>
          <span v-else class="muted">无</span>
        </template>
      </el-table-column>
      <el-table-column label="颜色" width="138">
        <template #default="{ row }">
          <span class="color-cell">
            <span class="color-swatch" :style="{ backgroundColor: row.color || '#d1d5db' }" :aria-label="row.color || '默认颜色'" />
            <span class="color-code">{{ row.color || '默认' }}</span>
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="sort_order" label="排序" width="76" align="center" />
      <el-table-column label="版本" width="78" align="center">
        <template #default="{ row }">v{{ row.version }}</template>
      </el-table-column>
      <el-table-column label="操作" width="252" fixed="right" align="right">
        <template #default="{ row }">
          <div class="row-actions">
            <el-tooltip content="编辑版块设置">
              <el-button circle size="small" :icon="Edit" :aria-label="`编辑 ${row.name}`" @click="showEditDialog(row)" />
            </el-tooltip>
            <el-tooltip v-if="row.node_kind === 'board'" content="配置这个版块可发布的内容类型">
              <el-button circle size="small" :icon="SetUp" :aria-label="`配置 ${row.name} 的帖子类型`" @click="showThreadTypeDialog(row)" />
            </el-tooltip>
            <el-tooltip v-if="row.node_kind === 'board' && row.lifecycle_status === 'active'" content="调整所在分组或移动到根级">
              <el-button circle size="small" :icon="Rank" :aria-label="`移动 ${row.name}`" @click="showMoveDialog(row)" />
            </el-tooltip>
            <el-tooltip v-if="row.lifecycle_status === 'active'" content="先查看影响，再归档">
              <el-button circle size="small" type="warning" :icon="Box" :aria-label="`归档 ${row.name}`" @click="prepareArchive(row)" />
            </el-tooltip>
            <el-tooltip v-else content="恢复为活动节点">
              <el-button circle size="small" type="success" :icon="RefreshLeft" :aria-label="`恢复 ${row.name}`" @click="restoreCategory(row)" />
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && categoryTree.length === 0" description="尚未创建版块或分组">
      <el-button type="primary" :icon="Plus" @click="showCreateDialog('board')">创建第一个版块</el-button>
    </el-empty>

    <el-dialog v-model="editorVisible" :title="isEdit ? '编辑版块' : '新建版块'" width="min(640px, calc(100% - 24px))" destroy-on-close>
      <el-form :model="formData" label-position="top">
        <el-form-item label="节点类型" required>
          <el-segmented v-model="formData.node_kind" :options="nodeKindOptions" :disabled="isEdit" />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="formData.name" maxlength="64" show-word-limit />
        </el-form-item>
        <el-form-item label="Slug">
          <el-input v-model="formData.slug" maxlength="64" placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item label="所属分组" v-if="formData.node_kind === 'board'">
          <el-select v-model="formData.parent_id" clearable filterable placeholder="根级版块">
            <el-option v-for="group in activeGroups" :key="group.id" :label="group.name" :value="group.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="formData.description" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12">
            <el-form-item label="图标">
              <el-input v-model="formData.icon" maxlength="128" placeholder="例如：📚" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="颜色">
              <div class="color-picker-row">
                <el-color-picker v-model="formData.color" color-format="hex" show-alpha />
                <el-button text @click="formData.color = ''">使用默认</el-button>
              </div>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="默认标签" v-if="formData.node_kind === 'board'">
          <el-select v-model="formData.default_tags" multiple filterable allow-create default-first-option class="full-width" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12">
            <el-form-item label="排序">
              <el-input-number v-model="formData.sort_order" :min="0" :max="99999" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" v-if="formData.node_kind === 'board'">
            <el-form-item label="发帖状态">
              <el-switch v-model="formData.is_closed" active-text="关闭" inactive-text="开放" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="editorVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ isEdit ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="moveVisible" title="移动版块" width="min(460px, calc(100% - 24px))">
      <el-form label-position="top">
        <el-form-item label="目标分组">
          <el-select v-model="moveParentID" clearable filterable placeholder="根级版块" class="full-width">
            <el-option v-for="group in activeGroups" :key="group.id" :label="group.name" :value="group.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="moveVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="moveCategory">确认移动</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="threadTypeVisible"
      :title="threadTypeTarget ? `帖子类型：${threadTypeTarget.name}` : '帖子类型'"
      width="min(540px, calc(100% - 24px))"
      destroy-on-close
    >
      <p class="dialog-help">
        这里决定用户可以在这个版块发布哪些内容。普通讨论和图文文章默认启用；校园互助、二手信息需要对应系统功能启用后才可创建。
      </p>
      <el-checkbox-group v-model="allowedThreadTypes" class="thread-type-list">
        <el-checkbox v-for="item in threadTypeOptions" :key="item.value" :label="item.value">
          <span>{{ item.label }}</span>
          <small>{{ item.description }}</small>
        </el-checkbox>
      </el-checkbox-group>
      <template #footer>
        <el-button @click="threadTypeVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="saveThreadTypePolicies">保存类型策略</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="archiveVisible" title="归档影响确认" width="min(500px, calc(100% - 24px))">
      <el-descriptions v-if="archiveTarget && archiveImpact" :column="1" border>
        <el-descriptions-item label="节点">{{ archiveTarget.name }}</el-descriptions-item>
        <el-descriptions-item label="关联帖子">{{ archiveImpact.associated_threads }}</el-descriptions-item>
        <el-descriptions-item label="活动子版块">{{ archiveImpact.active_child_boards }}</el-descriptions-item>
        <el-descriptions-item label="效果">
          {{ archiveImpact.will_block_new_posting ? '将阻止新帖子和回复' : '将从用户端导航中隐藏' }}
        </el-descriptions-item>
      </el-descriptions>
      <el-alert
        v-if="archiveImpact?.active_child_boards"
        class="archive-blocked"
        type="warning"
        :closable="false"
        show-icon
        title="请先归档或移动所有活动子版块。"
      />
      <template #footer>
        <el-button @click="archiveVisible = false">取消</el-button>
        <el-button
          type="warning"
          :loading="submitting"
          :disabled="Boolean(archiveImpact?.active_child_boards)"
          @click="archiveCategory"
        >
          确认归档
        </el-button>
      </template>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Box, Edit, FolderAdd, Plus, Rank, Refresh, RefreshLeft, SetUp } from '@element-plus/icons-vue'
import {
  categoryApi,
  type CategoryArchiveImpact,
  type CategoryNodeKind,
  type ManagedCategory,
  type ThreadType,
} from '@/modules/community/api'

const loading = ref(false)
const submitting = ref(false)
const categoryTree = ref<ManagedCategory[]>([])
const editorVisible = ref(false)
const moveVisible = ref(false)
const archiveVisible = ref(false)
const threadTypeVisible = ref(false)
const isEdit = ref(false)
const editingCategory = ref<ManagedCategory | null>(null)
const moveTarget = ref<ManagedCategory | null>(null)
const moveParentID = ref<string | undefined>()
const archiveTarget = ref<ManagedCategory | null>(null)
const archiveImpact = ref<CategoryArchiveImpact | null>(null)
const threadTypeTarget = ref<ManagedCategory | null>(null)
const allowedThreadTypes = ref<ThreadType[]>([])

const nodeKindOptions = [
  { label: '分组', value: 'group' },
  { label: '版块', value: 'board' },
]

const threadTypeOptions: Array<{ value: ThreadType; label: string; description: string }> = [
  { value: 'discussion', label: '普通讨论', description: '使用标准帖子编辑器发布讨论和问答。' },
  { value: 'article', label: '图文文章', description: '使用受控富文本编辑器创建长文。' },
  { value: 'mutual_aid', label: '校园互助', description: '发布有状态的求助、互助与进展信息。' },
  { value: 'secondhand', label: '二手信息', description: '发布带价格和交易状态的校园闲置信息。' },
]

const formData = reactive({
  name: '',
  slug: '',
  description: '',
  icon: '',
  color: '',
  default_tags: [] as string[],
  parent_id: undefined as string | undefined,
  node_kind: 'board' as CategoryNodeKind,
  sort_order: 0,
  is_closed: false,
})

const allCategories = computed(() => flatten(categoryTree.value))
const activeGroups = computed(() =>
  allCategories.value.filter((item) => item.node_kind === 'group' && item.lifecycle_status === 'active'),
)
const archivedCount = computed(() => allCategories.value.filter((item) => item.lifecycle_status === 'archived').length)

const unwrap = <T,>(response: any): T => response?.data ?? response

const load = async () => {
  loading.value = true
  try {
    categoryTree.value = unwrap<ManagedCategory[]>(await categoryApi.tree()) || []
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载版块树失败')
  } finally {
    loading.value = false
  }
}

const resetForm = (kind: CategoryNodeKind) => {
  formData.name = ''
  formData.slug = ''
  formData.description = ''
  formData.icon = ''
  formData.color = ''
  formData.default_tags = []
  formData.parent_id = undefined
  formData.node_kind = kind
  formData.sort_order = 0
  formData.is_closed = false
}

const showCreateDialog = (kind: CategoryNodeKind) => {
  isEdit.value = false
  editingCategory.value = null
  resetForm(kind)
  editorVisible.value = true
}

const showEditDialog = (category: ManagedCategory) => {
  isEdit.value = true
  editingCategory.value = category
  formData.name = category.name || ''
  formData.slug = category.slug || ''
  formData.description = category.description || ''
  formData.icon = category.icon || ''
  formData.color = category.color || ''
  formData.default_tags = [...(category.default_tags || [])]
  formData.parent_id = category.parent_id
  formData.node_kind = category.node_kind || 'board'
  formData.sort_order = category.sort_order || 0
  formData.is_closed = Boolean(category.is_closed)
  editorVisible.value = true
}

const submitForm = async () => {
  if (!formData.name.trim()) {
    ElMessage.warning('请输入名称')
    return
  }
  submitting.value = true
  try {
    const color = normalizeColor(formData.color)
    const base = {
      name: formData.name.trim(),
      slug: formData.slug.trim(),
      description: formData.description.trim(),
      icon: formData.icon.trim(),
      color,
      default_tags: normalizeTags(formData.default_tags),
      sort_order: formData.sort_order,
      is_closed: formData.node_kind === 'board' ? formData.is_closed : false,
    }
    if (isEdit.value && editingCategory.value) {
      await categoryApi.update(editingCategory.value.id, { ...base, version: editingCategory.value.version })
      ElMessage.success('版块设置已保存')
    } else {
      await categoryApi.create({
        ...base,
        node_kind: formData.node_kind,
        parent_id: formData.node_kind === 'board' ? formData.parent_id : undefined,
      })
      ElMessage.success(formData.node_kind === 'group' ? '分组已创建' : '版块已创建')
    }
    editorVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存失败，请刷新后重试')
  } finally {
    submitting.value = false
  }
}

const showMoveDialog = (category: ManagedCategory) => {
  moveTarget.value = category
  moveParentID.value = category.parent_id
  moveVisible.value = true
}

const moveCategory = async () => {
  if (!moveTarget.value) return
  submitting.value = true
  try {
    await categoryApi.move(moveTarget.value.id, { parent_id: moveParentID.value || null, version: moveTarget.value.version })
    ElMessage.success('版块位置已更新')
    moveVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '移动失败，请刷新后重试')
  } finally {
    submitting.value = false
  }
}

const showThreadTypeDialog = async (category: ManagedCategory) => {
  if (category.node_kind !== 'board') return
  threadTypeTarget.value = category
  allowedThreadTypes.value = []
  try {
    const result = unwrap<{ items?: Array<{ thread_type: ThreadType; enabled: boolean }> }>(await categoryApi.threadTypes(category.id))
    allowedThreadTypes.value = (result?.items || []).filter((item) => item.enabled).map((item) => item.thread_type)
    threadTypeVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载帖子类型策略失败')
  }
}

const saveThreadTypePolicies = async () => {
  if (!threadTypeTarget.value) return
  if (allowedThreadTypes.value.length === 0) {
    ElMessage.warning('请至少启用一种帖子类型')
    return
  }
  submitting.value = true
  try {
    await categoryApi.updateThreadTypes(threadTypeTarget.value.id, {
      allowed_types: [...allowedThreadTypes.value],
      version: threadTypeTarget.value.version,
    })
    ElMessage.success('帖子类型策略已保存')
    threadTypeVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存帖子类型策略失败，请刷新后重试')
  } finally {
    submitting.value = false
  }
}

const prepareArchive = async (category: ManagedCategory) => {
  try {
    archiveTarget.value = category
    archiveImpact.value = unwrap<CategoryArchiveImpact>(await categoryApi.archiveImpact(category.id))
    archiveVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '无法读取归档影响')
  }
}

const archiveCategory = async () => {
  if (!archiveTarget.value) return
  submitting.value = true
  try {
    await categoryApi.archive(archiveTarget.value.id, archiveTarget.value.version)
    ElMessage.success('节点已归档')
    archiveVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '归档失败，请刷新后重试')
  } finally {
    submitting.value = false
  }
}

const restoreCategory = async (category: ManagedCategory) => {
  try {
    await ElMessageBox.confirm(`恢复“${category.name}”后将重新出现在用户端。`, '恢复节点', {
      confirmButtonText: '恢复',
      cancelButtonText: '取消',
      type: 'info',
    })
    await categoryApi.restore(category.id, category.version)
    ElMessage.success('节点已恢复')
    await load()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.msg || '恢复失败，请刷新后重试')
  }
}

const flatten = (items: ManagedCategory[]): ManagedCategory[] =>
  items.flatMap((item) => [item, ...flatten(item.children || [])])

const normalizeTags = (tags: string[]) => {
  const seen = new Set<string>()
  return tags
    .map((tag) => String(tag || '').trim())
    .filter((tag) => {
      const key = tag.toLowerCase()
      if (!key || seen.has(key)) return false
      seen.add(key)
      return true
    })
    .slice(0, 20)
}

const normalizeColor = (color: string) => {
  const value = String(color || '').trim()
  if (!value) return ''
  if (/^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$/.test(value)) return value.toUpperCase()
  const channels = value.match(/^rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)$/i)
  if (!channels) return value
  const rgb = channels.slice(1, 4).map((item) => Number(item).toString(16).padStart(2, '0')).join('')
  const alpha = channels[4] === undefined ? '' : Math.round(Math.min(1, Math.max(0, Number(channels[4]))) * 255).toString(16).padStart(2, '0')
  return `#${rgb}${alpha}`.toUpperCase()
}

onMounted(load)
</script>

<style scoped>
.admin-categories {
  min-width: 0;
}
.page-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 16px;
}
.page-header h1 {
  margin: 0;
  font-size: 22px;
  line-height: 1.3;
}
.page-actions,
.row-actions,
.color-picker-row,
.tag-list,
.color-cell,
.category-name {
  display: flex;
  align-items: center;
  gap: 8px;
}
.page-actions,
.row-actions,
.tag-list {
  flex-wrap: wrap;
}
.archive-alert {
  margin-bottom: 16px;
}
.category-tree {
  width: 100%;
}
.category-name {
  min-width: 0;
}
.category-icon {
  width: 22px;
  text-align: center;
  color: var(--el-color-primary);
  font-size: 18px;
}
.category-name-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.category-slug,
.muted,
.color-code {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.category-slug {
  display: block;
  margin-left: 30px;
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.color-swatch {
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  border: 1px solid rgba(15, 23, 42, 0.22);
  border-radius: 4px;
}
.full-width {
  width: 100%;
}
.archive-blocked {
  margin-top: 16px;
}
.dialog-help {
  margin: 0 0 16px;
  color: var(--el-text-color-secondary);
  line-height: 1.65;
}
.thread-type-list {
  display: grid;
  gap: 12px;
}
.thread-type-list :deep(.el-checkbox) {
  align-items: flex-start;
  margin-right: 0;
  white-space: normal;
}
.thread-type-list small {
  display: block;
  margin-top: 3px;
  color: var(--el-text-color-secondary);
  line-height: 1.45;
}
@media (max-width: 760px) {
  .page-header {
    flex-direction: column;
  }
  .page-actions {
    width: 100%;
  }
  .page-actions :deep(.el-button) {
    flex: 1 1 auto;
  }
  .category-tree :deep(.el-table__fixed-right) {
    display: none;
  }
  .category-tree :deep(.el-table__cell:nth-child(3)),
  .category-tree :deep(.el-table__cell:nth-child(4)),
  .category-tree :deep(.el-table__cell:nth-child(5)) {
    display: none;
  }
}
</style>
