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
        <el-table-column label="角色" width="180" align="center">
          <template #default="{ row }">
            <div v-if="userRoles[row.id] && userRoles[row.id].length > 0">
              <el-tag
                v-for="role in userRoles[row.id]"
                :key="role.id"
                :type="roleTagType(role.name)"
                size="small"
                style="margin: 2px"
              >
                {{ roleNameMap[role.name] || role.name }}
              </el-tag>
            </div>
            <el-tag v-else type="info" size="small">member</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">
              {{ row.status === 'active' ? '正常' : '已封禁' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" plain @click="openRoleDialog(row)">
              角色
            </el-button>
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

    <!-- 角色管理对话框 -->
    <el-dialog v-model="roleDialogVisible" title="角色管理" width="500px" destroy-on-close>
      <div v-if="selectedUser" class="role-dialog-content">
        <p><strong>用户：</strong>{{ selectedUser.username }}（{{ selectedUser.nickname }}）</p>

        <h4 style="margin: 16px 0 8px">当前角色：</h4>
        <div v-if="selectedUserRoles.length > 0" style="margin-bottom: 16px">
          <el-tag
            v-for="role in selectedUserRoles"
            :key="role.id"
            :type="roleTagType(role.name)"
            size="default"
            closable
            style="margin: 4px"
            @close="doRevokeRole(selectedUser.id, role.id)"
          >
            {{ roleNameMap[role.name] || role.name }}
          </el-tag>
        </div>
        <el-empty v-else description="暂无角色" :image-size="60" />

        <h4 style="margin: 16px 0 8px">分配新角色：</h4>
        <el-select v-model="newRoleId" placeholder="选择角色" style="width: 100%">
          <el-option
            v-for="role in allRoles"
            :key="role.id"
            :label="roleNameMap[role.name] || role.name"
            :value="role.id"
            :disabled="isRoleAssigned(role.id)"
          >
            <span>{{ roleNameMap[role.name] || role.name }}</span>
            <span style="float: right; color: #8492a6; font-size: 12px">{{ role.description }}</span>
          </el-option>
        </el-select>
        <el-button
          type="primary"
          style="width: 100%; margin-top: 12px"
          :disabled="!newRoleId"
          @click="doAssignRole"
        >
          分配角色
        </el-button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { userApi, roleApi } from '@/api'

const users = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')

// 角色相关状态
const userRoles = ref<Record<string, any[]>>({})
const allRoles = ref<any[]>([])
const roleDialogVisible = ref(false)
const selectedUser = ref<any>(null)
const selectedUserRoles = ref<any[]>([])
const newRoleId = ref<number | null>(null)

const roleNameMap: Record<string, string> = {
  admin: '管理员',
  moderator: '版主',
  member: '普通会员',
  guest: '访客',
}

const roleTagType = (name: string) => {
  const map: Record<string, string> = {
    admin: 'danger',
    moderator: 'warning',
    member: '',
    guest: 'info',
  }
  return map[name] || ''
}

const isRoleAssigned = (roleId: number) => {
  return selectedUserRoles.value.some((r) => r.id === roleId)
}

const load = async () => {
  loading.value = true
  try {
    const r = (await userApi.list({ page: page.value, page_size: 20 })) as any
    users.value = r?.data?.items || []
    total.value = r?.data?.pagination?.total || 0
    // 加载每个用户的角色
    for (const user of users.value) {
      loadUserRoles(user.id)
    }
  } catch {
    ElMessage.error('加载用户列表失败')
  }
  loading.value = false
}

const loadUserRoles = async (userId: string) => {
  try {
    const r = (await roleApi.getUserRoles(userId)) as any
    userRoles.value[userId] = r?.data || []
  } catch {
    userRoles.value[userId] = []
  }
}

const loadAllRoles = async () => {
  try {
    const r = (await roleApi.list()) as any
    allRoles.value = r?.data || []
  } catch {
    allRoles.value = []
  }
}

const openRoleDialog = async (user: any) => {
  selectedUser.value = user
  newRoleId.value = null
  await loadUserRoles(user.id)
  selectedUserRoles.value = userRoles.value[user.id] || []
  if (allRoles.value.length === 0) {
    await loadAllRoles()
  }
  roleDialogVisible.value = true
}

const doAssignRole = async () => {
  if (!selectedUser.value || !newRoleId.value) return
  try {
    await roleApi.assign(selectedUser.value.id, newRoleId.value)
    ElMessage.success('角色分配成功')
    await loadUserRoles(selectedUser.value.id)
    selectedUserRoles.value = userRoles.value[selectedUser.value.id] || []
    newRoleId.value = null
  } catch {
    ElMessage.error('角色分配失败')
  }
}

const doRevokeRole = async (userId: string, roleId: number) => {
  try {
    await roleApi.revoke(userId, roleId)
    ElMessage.success('角色已撤销')
    await loadUserRoles(userId)
    selectedUserRoles.value = userRoles.value[userId] || []
  } catch {
    ElMessage.error('角色撤销失败')
  }
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

onMounted(() => {
  load()
  loadAllRoles()
})
</script>

<style scoped>
.admin-users {
  max-width: 1400px;
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

.role-dialog-content h4 {
  color: #303133;
}
</style>