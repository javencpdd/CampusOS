<template>
  <div class="admin-threads">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>帖子管理</span>
          <div class="header-actions">
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
            <span>{{ row.title }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="author_name" label="作者" width="100" />
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
        <el-table-column label="操作" width="280" align="center" fixed="right">
          <template #default="{ row }">
            <el-button-group>
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
                @confirm="doDelete(row.id)"
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
import { threadApi, categoryApi } from '@/api'

const threads = ref<any[]>([])
const categories = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')
const filterCategory = ref('')

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

const doDelete = async (id: string) => {
  try {
    await threadApi.delete(id)
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