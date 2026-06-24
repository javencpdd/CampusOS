<template>
  <div class="admin-plugins">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>插件管理</span>
          <el-tag type="info" size="small">已安装 {{ plugins.length }} 个插件</el-tag>
        </div>
      </template>

      <el-table :data="plugins" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="name" label="插件名称" width="180">
          <template #default="{ row }">
            <div class="plugin-name">
              <el-icon style="margin-right: 4px"><Connection /></el-icon>
              <strong>{{ row.name }}</strong>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="display_name" label="显示名称" width="150" />
        <el-table-column prop="version" label="版本" width="100" align="center">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">v{{ row.version }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="250" show-overflow-tooltip />
        <el-table-column prop="runtime" label="运行时" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.runtime === 'grpc' ? 'success' : 'warning'" size="small">
              {{ row.runtime?.toUpperCase() || 'N/A' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enabled' ? 'success' : 'danger'" size="small">
              {{ row.status === 'enabled' ? '已启用' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="center" fixed="right">
          <template #default="{ row }">
            <el-switch
              v-model="row.status"
              active-value="enabled"
              inactive-value="disabled"
              active-text="启用"
              inactive-text="禁用"
              inline-prompt
              style="margin-right: 8px"
              @change="togglePlugin(row)"
            />
            <el-popconfirm
              title="确定要卸载该插件吗？此操作不可恢复。"
              confirm-button-text="卸载"
              cancel-button-text="取消"
              confirm-button-type="danger"
              @confirm="doUninstall(row.name)"
            >
              <template #reference>
                <el-button type="danger" size="small" plain>卸载</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && plugins.length === 0" description="暂无已安装的插件" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Connection } from '@element-plus/icons-vue'
import { pluginApi } from '@/api'

const plugins = ref<any[]>([])
const loading = ref(false)

const load = async () => {
  loading.value = true
  try {
    const r = (await pluginApi.list()) as any
    plugins.value = r?.data?.items || r?.data || []
  } catch {
    // 插件接口可能不可用，静默处理
    plugins.value = []
  }
  loading.value = false
}

const togglePlugin = async (row: any) => {
  try {
    if (row.status === 'enabled') {
      await pluginApi.enable(row.name)
      ElMessage.success(`插件 ${row.name} 已启用`)
    } else {
      await pluginApi.disable(row.name)
      ElMessage.success(`插件 ${row.name} 已禁用`)
    }
  } catch {
    ElMessage.error('操作失败')
    // 回滚状态
    row.status = row.status === 'enabled' ? 'disabled' : 'enabled'
  }
}

const doUninstall = async (name: string) => {
  try {
    await pluginApi.uninstall(name)
    ElMessage.success('插件已卸载')
    load()
  } catch {
    ElMessage.error('卸载失败')
  }
}

onMounted(load)
</script>

<style scoped>
.admin-plugins {
  max-width: 1400px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.plugin-name {
  display: flex;
  align-items: center;
}
</style>