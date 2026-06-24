<template>
  <div class="admin-users">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>用户管理</span>
          <div class="header-actions">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索用户名/昵称"
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

      <el-table :data="users" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="200" show-overflow-tooltip />
        <el-table-column prop="username" label="用户名" width="120" />
        <el-table-column prop="nickname" label="昵称" width="120" />
        <el-table-column prop="email" label="邮箱" min-width="200" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">
              {{ row.status === 'active' ? '正常' : '已封禁' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="center" fixed="right">
          <template #default="{ row }">
            <el-popconfirm
              v-if="row.status === 'active'"
              title="确定要封禁该用户吗？"
              confirm-button-text="封禁"
              cancel-button-text="取消"
              confirm-button-type="danger"
              @confirm="doSuspend(row.id)"
            >
              <template #reference>
                <el-button type="danger" size="small" plain>封禁</el-button>
              </template>
            </el-popconfirm>
            <el-button
              v-else
              type="success"
              size="small"
              plain
              @click="doActivate(row.id)"
            >
              解封
            </el-button>
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
import { Search } from '@element-plus/icons-vue'
import { userApi } from '@/api'

const users = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')

const load = async () => {
  loading.value = true
  try {
    const r = (await userApi.list({ page: page.value, page_size: 20 })) as any
    users.value = r?.data?.items || []
    total.value = r?.data?.pagination?.total || 0
  } catch {
    ElMessage.error('加载用户列表失败')
  }
  loading.value = false
}

const doSuspend = async (id: string) => {
  try {
    await userApi.suspend(id)
    ElMessage.success('已封禁')
    load()
  } catch {
    ElMessage.error('封禁失败')
  }
}

const doActivate = async (id: string) => {
  try {
    await userApi.activate(id)
    ElMessage.success('已解封')
    load()
  } catch {
    ElMessage.error('解封失败')
  }
}

onMounted(load)
</script>

<style scoped>
.admin-users {
  max-width: 1200px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>