<template>
  <div class="admin-users">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>用户管理</span>
          <div class="header-actions">
            <el-button v-if="canManageRoles" plain @click="$router.push('/moderators')">
              版主管理
            </el-button>
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
        <el-table-column prop="email" label="邮箱" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="{ 'empty-email': !row.email }">{{ row.email || '未绑定邮箱' }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="canManageRoles" label="角色" width="180" align="center">
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
        <el-table-column label="操作" width="340" align="center" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canManageRoles" size="small" plain @click="openStorageDialog(row)">
              空间
            </el-button>
            <el-button v-if="canManageRoles" type="primary" size="small" plain @click="openRoleDialog(row)">
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
            :closable="canRevokeRole(role)"
            style="margin: 4px"
            @close="doRevokeRole(selectedUser.id, role.id)"
          >
            {{ roleNameMap[role.name] || role.name }}
          </el-tag>
        </div>
        <el-empty v-else description="暂无角色" :image-size="60" />

        <h4 style="margin: 16px 0 8px">分配新角色：</h4>
        <el-select v-model="newRoleId" placeholder="选择角色" style="width: 100%" @change="handleRoleSelection">
          <el-option
            v-for="role in allRoles"
            :key="role.id"
            :label="roleNameMap[role.name] || role.name"
            :value="role.id"
            :disabled="(isRoleAssigned(role.id) && role.name !== 'moderator') || isProtectedRole(role)"
          >
            <span>{{ roleNameMap[role.name] || role.name }}</span>
            <span style="float: right; color: #8492a6; font-size: 12px">{{ role.description }}</span>
          </el-option>
        </el-select>
        <div v-if="selectedRole?.name === 'moderator'" class="moderator-scope-editor">
          <h4>管理板块：</h4>
          <el-select
            v-model="moderatorCategoryIds"
            multiple
            filterable
            placeholder="至少选择一个板块"
            style="width: 100%"
          >
            <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
          </el-select>
          <el-alert
            type="info"
            :closable="false"
			title="版主只能在选中板块内置顶、锁定主题或删除回复；具体操作还受核心版主策略中的动作上限控制。"
          />
        </div>
        <el-button
          type="primary"
          style="width: 100%; margin-top: 12px"
          :disabled="!newRoleId || assigningRole"
          :loading="assigningRole"
          @click="doAssignRole"
        >
          分配角色
        </el-button>
      </div>
    </el-dialog>

    <el-dialog v-model="storageDialogVisible" title="个人空间配额" width="520px" destroy-on-close>
      <div v-if="storageSelectedUser" v-loading="storageLoading" class="storage-dialog-content">
        <p><strong>用户：</strong>{{ storageSelectedUser.username }}（{{ storageSelectedUser.nickname }}）</p>
        <el-descriptions v-if="storageStatus" :column="2" border>
          <el-descriptions-item label="已使用">{{ formatBytes(storageStatus.used_bytes) }}</el-descriptions-item>
          <el-descriptions-item label="当前配额">{{ formatBytes(storageStatus.quota_bytes) }}</el-descriptions-item>
          <el-descriptions-item label="可用空间">{{ formatBytes(storageStatus.available_bytes) }}</el-descriptions-item>
          <el-descriptions-item label="配额类型">{{ storageStatus.custom_quota ? '管理员授权' : '系统默认' }}</el-descriptions-item>
        </el-descriptions>
        <el-form label-position="top" @submit.prevent="saveStorageQuota">
          <el-form-item label="授权空间（MB）">
            <el-input-number v-model="storageQuotaMB" :min="1" :max="102400" :step="10" class="storage-quota-input" />
            <p class="storage-hint">默认 50 MB。调小至已用容量以下不会删除文件，但会阻止后续写入，直至释放空间或再次扩容。</p>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="storageDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="storageSaving" :disabled="!storageStatus" @click="saveStorageQuota">保存授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { categoryApi } from '@/modules/community/api'
import { moderationApi, userApi, roleApi } from '@/modules/identity/api'
import { spaceAdminApi } from '@/modules/integrations/api'
import { useAdminStore } from '@/modules/identity/store'

const users = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const searchKeyword = ref('')
const adminStore = useAdminStore()
const canManageRoles = computed(() => adminStore.isAdmin)

// 角色相关状态
const userRoles = ref<Record<string, any[]>>({})
const allRoles = ref<any[]>([])
const roleDialogVisible = ref(false)
const selectedUser = ref<any>(null)
const selectedUserRoles = ref<any[]>([])
const newRoleId = ref<number | null>(null)
const assigningRole = ref(false)
const categories = ref<any[]>([])
const moderatorCategoryIds = ref<string[]>([])
const selectedRole = computed(() => allRoles.value.find((role) => role.id === newRoleId.value))
const storageDialogVisible = ref(false)
const storageSelectedUser = ref<any>(null)
const storageStatus = ref<any>(null)
const storageQuotaMB = ref(50)
const storageLoading = ref(false)
const storageSaving = ref(false)

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

const isProtectedRole = (role: any) => role.name === 'member' || role.name === 'guest'

const canRevokeRole = (role: any) => selectedUser.value?.id !== adminStore.user?.id && !isProtectedRole(role)

const apiMessage = (error: any, fallback: string) => error?.msg || error?.message || fallback
const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const power = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** power).toFixed(power === 0 ? 0 : 2)} ${units[power]}`
}

const load = async () => {
  loading.value = true
  try {
    const r = (await userApi.list({ page: page.value, page_size: 20 })) as any
    users.value = r?.data?.items || []
    total.value = r?.data?.pagination?.total || 0
    if (canManageRoles.value) {
      await Promise.all(users.value.map((user) => loadUserRoles(user.id)))
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
  if (!canManageRoles.value) {
    allRoles.value = []
    return
  }
  try {
    const r = (await roleApi.list()) as any
    allRoles.value = r?.data || []
  } catch {
    allRoles.value = []
  }
}

const openRoleDialog = async (user: any) => {
  if (!canManageRoles.value) {
    ElMessage.warning('当前账号没有角色管理权限')
    return
  }
  selectedUser.value = user
  newRoleId.value = null
  moderatorCategoryIds.value = []
  await loadUserRoles(user.id)
  selectedUserRoles.value = userRoles.value[user.id] || []
  if (allRoles.value.length === 0) {
    await loadAllRoles()
  }
  roleDialogVisible.value = true
}

const openStorageDialog = async (user: any) => {
  storageSelectedUser.value = user
  storageStatus.value = null
  storageQuotaMB.value = 50
  storageDialogVisible.value = true
  storageLoading.value = true
  try {
    const result = (await spaceAdminApi.storage(user.id)) as any
    storageStatus.value = result?.data
    storageQuotaMB.value = Math.max(1, Math.round(Number(result?.data?.quota_bytes || 50 * 1024 * 1024) / 1024 / 1024))
  } catch (error: any) {
    ElMessage.error(apiMessage(error, '加载个人空间配额失败'))
  } finally {
    storageLoading.value = false
  }
}

const saveStorageQuota = async () => {
  if (!storageSelectedUser.value || !Number.isFinite(storageQuotaMB.value)) return
  storageSaving.value = true
  try {
    const quotaBytes = Math.round(storageQuotaMB.value * 1024 * 1024)
    const result = (await spaceAdminApi.setStorageQuota(storageSelectedUser.value.id, quotaBytes)) as any
    storageStatus.value = result?.data
    storageQuotaMB.value = Math.max(1, Math.round(Number(result?.data?.quota_bytes || quotaBytes) / 1024 / 1024))
    ElMessage.success('个人空间配额已更新')
  } catch (error: any) {
    ElMessage.error(apiMessage(error, '更新个人空间配额失败'))
  } finally {
    storageSaving.value = false
  }
}

const handleRoleSelection = async () => {
  if (selectedRole.value?.name !== 'moderator' || !selectedUser.value) {
    moderatorCategoryIds.value = []
    return
  }
  try {
    const result = (await moderationApi.get(selectedUser.value.id)) as any
    moderatorCategoryIds.value = result?.data?.category_ids || []
  } catch {
    moderatorCategoryIds.value = []
  }
}

const doAssignRole = async () => {
  if (!selectedUser.value || !newRoleId.value) return
  assigningRole.value = true
  try {
    if (selectedRole.value?.name === 'moderator') {
      if (moderatorCategoryIds.value.length === 0) {
        ElMessage.warning('请至少选择一个版主负责的板块')
        return
      }
      const result = (await moderationApi.update(selectedUser.value.id, moderatorCategoryIds.value)) as any
      ElMessage.success(result?.data?.message || '版主管理范围已保存')
    } else {
      const result = (await roleApi.assign(selectedUser.value.id, newRoleId.value)) as any
      ElMessage.success(result?.data?.message || '角色分配成功')
    }
    await loadUserRoles(selectedUser.value.id)
    selectedUserRoles.value = userRoles.value[selectedUser.value.id] || []
    newRoleId.value = null
  } catch (error: any) {
    ElMessage.error(apiMessage(error, '角色分配失败'))
  } finally {
    assigningRole.value = false
  }
}

const doRevokeRole = async (userId: string, roleId: number) => {
  try {
    const role = selectedUserRoles.value.find((item) => item.id === roleId)
    if (role?.name === 'moderator') {
      await moderationApi.update(userId, [])
    } else {
      await roleApi.revoke(userId, roleId)
    }
    ElMessage.success('角色已撤销')
    await loadUserRoles(userId)
    selectedUserRoles.value = userRoles.value[userId] || []
  } catch (error: any) {
    ElMessage.error(apiMessage(error, '角色撤销失败'))
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
  categoryApi.list().then((result: any) => {
    categories.value = Array.isArray(result?.data) ? result.data : []
  }).catch(() => {
    categories.value = []
  })
  if (canManageRoles.value) {
    loadAllRoles()
  }
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

.moderator-scope-editor {
  display: grid;
  gap: 10px;
  margin-top: 14px;
}

.moderator-scope-editor h4 {
  margin: 0;
}

.storage-dialog-content {
  display: grid;
  gap: 16px;
}

.storage-dialog-content p,
.storage-hint {
  margin: 0;
}

.storage-quota-input {
  width: 100%;
}

.storage-hint {
  margin-top: 8px;
  color: #606266;
  font-size: 12px;
  line-height: 1.6;
}

.empty-email {
  color: #909399;
}
</style>
