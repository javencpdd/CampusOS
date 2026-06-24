<template>
  <div class="admin-categories">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>版块管理</span>
          <el-button type="primary" @click="showCreateDialog">
            <el-icon><Plus /></el-icon>
            新建版块
          </el-button>
        </div>
      </template>

      <el-table :data="categories" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="200" show-overflow-tooltip />
        <el-table-column prop="icon" label="图标" width="60" align="center">
          <template #default="{ row }">
            <span style="font-size: 20px">{{ row.icon || '📁' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="名称" width="150" />
        <el-table-column prop="description" label="描述" min-width="250" show-overflow-tooltip />
        <el-table-column prop="sort_order" label="排序" width="80" align="center" />
        <el-table-column prop="color" label="颜色" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.color" :color="row.color" style="color: #fff" size="small">
              {{ row.color }}
            </el-tag>
            <span v-else style="color: #c0c4cc">默认</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" plain @click="showEditDialog(row)">
              <el-icon><Edit /></el-icon>
              编辑
            </el-button>
            <el-popconfirm
              title="确定要删除该版块吗？"
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
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑版块' : '新建版块'"
      width="500px"
      destroy-on-close
    >
      <el-form :model="formData" label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="formData.name" placeholder="请输入版块名称" maxlength="50" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            placeholder="请输入版块描述"
            maxlength="200"
          />
        </el-form-item>
        <el-form-item label="图标">
          <el-input v-model="formData.icon" placeholder="输入 emoji 图标，如 📁" maxlength="10" style="width: 120px" />
        </el-form-item>
        <el-form-item label="颜色">
          <el-color-picker v-model="formData.color" show-alpha />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="formData.sort_order" :min="0" :max="999" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">
          {{ isEdit ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { categoryApi } from '@/api'

const categories = ref<any[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editingId = ref('')

const formData = reactive({
  name: '',
  description: '',
  icon: '',
  color: '',
  sort_order: 0,
})

const resetForm = () => {
  formData.name = ''
  formData.description = ''
  formData.icon = ''
  formData.color = ''
  formData.sort_order = 0
}

const load = async () => {
  loading.value = true
  try {
    const r = (await categoryApi.list()) as any
    categories.value = Array.isArray(r?.data) ? r.data : []
  } catch {
    ElMessage.error('加载版块列表失败')
  }
  loading.value = false
}

const showCreateDialog = () => {
  isEdit.value = false
  editingId.value = ''
  resetForm()
  dialogVisible.value = true
}

const showEditDialog = (row: any) => {
  isEdit.value = true
  editingId.value = row.id
  formData.name = row.name || ''
  formData.description = row.description || ''
  formData.icon = row.icon || ''
  formData.color = row.color || ''
  formData.sort_order = row.sort_order || 0
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!formData.name.trim()) {
    ElMessage.warning('请输入版块名称')
    return
  }
  submitting.value = true
  try {
    const data = { ...formData }
    if (isEdit.value) {
      await categoryApi.update(editingId.value, data)
      ElMessage.success('版块已更新')
    } else {
      await categoryApi.create(data)
      ElMessage.success('版块已创建')
    }
    dialogVisible.value = false
    load()
  } catch {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  }
  submitting.value = false
}

const doDelete = async (id: string) => {
  try {
    await categoryApi.delete(id)
    ElMessage.success('已删除')
    load()
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(load)
</script>

<style scoped>
.admin-categories {
  max-width: 1200px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>