<template>
  <div class="admin-threads">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>帖子管理</span>
          <div class="header-actions">
            <el-select v-model="filterStatus" placeholder="状态" style="width: 120px" @change="load">
              <el-option label="全部状态" value="all" />
              <el-option label="已发布" value="published" />
              <el-option label="草稿" value="draft" />
              <el-option label="已归档" value="archived" />
            </el-select>
            <el-select v-model="filterContentFormat" placeholder="内容类型" clearable style="width: 140px" @change="load">
              <el-option label="全部类型" value="" />
              <el-option label="普通文本" value="markdown" />
              <el-option label="图文文章" value="richtext_article" />
            </el-select>
            <el-select v-model="filterCategory" placeholder="筛选版块" clearable style="width: 150px" @change="load">
              <el-option label="全部版块" value="" />
              <el-option v-for="cat in categories" :key="cat.id" :label="cat.name" :value="cat.id" />
            </el-select>
            <el-input
              v-model="searchKeyword"
              placeholder="搜索帖子标题"
              clearable
              style="width: 200px"
              @clear="load"
              @keyup.enter="load"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
          </div>
        </div>
      </template>

      <el-table :data="threads" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="180" show-overflow-tooltip />
        <el-table-column prop="title" label="标题" min-width="250" show-overflow-tooltip>
          <template #default="{ row }">
            <el-tag v-if="row.is_pinned" type="warning" size="small" style="margin-right: 4px">置顶</el-tag>
            <el-tag v-if="row.is_locked" type="info" size="small" style="margin-right: 4px">锁定</el-tag>
            <el-tag v-if="isRichText(row)" type="success" size="small" style="margin-right: 4px">图文</el-tag>
            <span>{{ row.title }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="author_name" label="作者" width="100" />
        <el-table-column label="类型" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="isRichText(row) ? 'success' : 'info'" size="small" effect="plain">
              {{ isRichText(row) ? '图文' : '普通' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="view_count" label="浏览" width="80" align="center" />
        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag
              :type="statusTagType(row.status)"
              size="small"
            >
              {{ statusMap[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="360" align="center" fixed="right">
          <template #default="{ row }">
            <el-button-group>
              <el-tooltip v-if="isRichText(row) && row.status === 'published'" content="下架图文文章" placement="top">
                <el-button type="warning" size="small" plain @click="offlineRichText(row)">
                  下架
                </el-button>
              </el-tooltip>
              <el-tooltip v-if="isRichText(row) && row.status === 'archived'" content="恢复图文文章" placement="top">
                <el-button type="success" size="small" plain @click="restoreRichText(row)">
                  恢复
                </el-button>
              </el-tooltip>
              <el-tooltip :content="row.is_pinned ? '取消置顶' : '置顶'" placement="top">
                <el-button
                  :type="row.is_pinned ? 'warning' : 'default'"
                  size="small"
                  plain
                  @click="togglePin(row)"
                >
                  <el-icon><Top /></el-icon>
                </el-button>
              </el-tooltip>
              <el-tooltip :content="row.is_locked ? '取消锁定' : '锁定'" placement="top">
                <el-button
                  :type="row.is_locked ? 'info' : 'default'"
                  size="small"
                  plain
                  @click="toggleLock(row)"
                >
                  <el-icon><Lock /></el-icon>
                </el-button>
              </el-tooltip>
              <el-popconfirm
                title="确定要删除该帖子吗？此操作不可恢复。"
                confirm-button-text="删除"
                cancel-button-text="取消"
                confirm-button-type="danger"
                @confirm="doDelete(row)"
              >
                <template #reference>
                  <el-button type="danger" size="small" plain>
                    <el-icon><Delete /></el-icon>
                  </el-button>
                </template>
              </el-popconfirm>
            </el-button-group>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="page"
          :page-size="20"
          :total="total"
          layout="total, prev, pager, next, jumper"
          @current-change="load"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Top, Lock, Delete } from '@element-plus/icons-vue'
import { threadApi, categoryApi, richTextAdminApi } from '@/api'

const threads = ref<any[]>([])
const categories = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')
const filterCategory = ref('')
const filterStatus = ref('published')
const filterContentFormat = ref('')

const statusMap: Record<string, string> = {
  draft: '草稿',
  pending_review: '待审核',
  published: '已发布',
  archived: '已归档',
}

const statusTagType = (status: string) => {
  const map: Record<string, string> = {
    published: 'success',
    draft: 'info',
    pending_review: 'warning',
    archived: '',
  }
  return map[status] || ''
}

const load = async () => {
  loading.value = true
  try {
    const params: any = { page: page.value, page_size: 20 }
    if (searchKeyword.value) params.keyword = searchKeyword.value
    if (filterCategory.value) params.category_id = filterCategory.value
    if (filterStatus.value) params.status = filterStatus.value
    if (filterContentFormat.value) params.content_format = filterContentFormat.value
    const r = (await threadApi.list(params)) as any
    threads.value = r?.data?.items || []
    total.value = r?.data?.pagination?.total || 0
  } catch {
    ElMessage.error('加载帖子列表失败')
  }
  loading.value = false
}

const loadCategories = async () => {
  try {
    const r = (await categoryApi.list()) as any
    categories.value = Array.isArray(r?.data) ? r.data : []
  } catch {
    // 静默处理
  }
}

const togglePin = async (row: any) => {
  try {
    if (row.is_pinned) {
      await threadApi.unpin(row.id)
      ElMessage.success('已取消置顶')
    } else {
      await threadApi.pin(row.id)
      ElMessage.success('已置顶')
    }
    load()
  } catch {
    ElMessage.error('操作失败')
  }
}

const toggleLock = async (row: any) => {
  try {
    if (row.is_locked) {
      await threadApi.unlock(row.id)
      ElMessage.success('已取消锁定')
    } else {
      await threadApi.lock(row.id)
      ElMessage.success('已锁定')
    }
    load()
  } catch {
    ElMessage.error('操作失败')
  }
}

const isRichText = (row: any) => row?.content_format === 'richtext_article'

const offlineRichText = async (row: any) => {
  try {
    await richTextAdminApi.offline(row.id)
    ElMessage.success('图文文章已下架')
    load()
  } catch {
    ElMessage.error('下架失败')
  }
}

const restoreRichText = async (row: any) => {
  try {
    await richTextAdminApi.restore(row.id)
    ElMessage.success('图文文章已恢复')
    load()
  } catch {
    ElMessage.error('恢复失败')
  }
}

const doDelete = async (row: any) => {
  try {
    if (isRichText(row)) {
      await richTextAdminApi.delete(row.id)
    } else {
      await threadApi.adminDelete(row.id)
    }
    ElMessage.success('已删除')
    load()
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(() => {
  load()
  loadCategories()
})
</script>

<style scoped>
.admin-threads {
  max-width: 1400px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 12px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
