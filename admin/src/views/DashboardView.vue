<template>
  <div class="admin-dashboard">
    <!-- 统计卡片 -->
    <el-row :gutter="20" class="stat-row">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-users">
          <div class="stat-content">
            <div class="stat-info">
              <div class="stat-title">用户总数</div>
              <div class="stat-value">{{ stats.users }}</div>
            </div>
            <el-icon class="stat-icon"><User /></el-icon>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-threads">
          <div class="stat-content">
            <div class="stat-info">
              <div class="stat-title">帖子总数</div>
              <div class="stat-value">{{ stats.threads }}</div>
            </div>
            <el-icon class="stat-icon"><Document /></el-icon>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-categories">
          <div class="stat-content">
            <div class="stat-info">
              <div class="stat-title">版块数量</div>
              <div class="stat-value">{{ stats.categories }}</div>
            </div>
            <el-icon class="stat-icon"><FolderOpened /></el-icon>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-plugins">
          <div class="stat-content">
            <div class="stat-info">
              <div class="stat-title">插件数量</div>
              <div class="stat-value">{{ stats.plugins }}</div>
            </div>
            <el-icon class="stat-icon"><Connection /></el-icon>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 快捷操作 -->
    <el-row :gutter="20" class="quick-actions">
      <el-col :span="24">
        <el-card>
          <template #header>
            <span>快捷操作</span>
          </template>
          <el-space wrap>
            <el-button type="primary" @click="$router.push('/admin/users')">
              <el-icon><User /></el-icon>用户管理
            </el-button>
            <el-button type="success" @click="$router.push('/admin/threads')">
              <el-icon><Document /></el-icon>帖子管理
            </el-button>
            <el-button type="warning" @click="$router.push('/admin/categories')">
              <el-icon><FolderOpened /></el-icon>版块管理
            </el-button>
            <el-button type="info" @click="$router.push('/admin/plugins')">
              <el-icon><Connection /></el-icon>插件管理
            </el-button>
            <el-button @click="$router.push('/admin/events')">
              <el-icon><Bell /></el-icon>事件日志
            </el-button>
          </el-space>
        </el-card>
      </el-col>
    </el-row>

    <!-- 数据表格 -->
    <el-row :gutter="20" class="data-row">
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>最新帖子</span>
              <el-button text type="primary" size="small" @click="$router.push('/admin/threads')">
                查看全部
              </el-button>
            </div>
          </template>
          <el-table :data="threads" size="small" stripe v-loading="loading">
            <el-table-column prop="title" label="标题" show-overflow-tooltip />
            <el-table-column prop="author_name" label="作者" width="100" />
            <el-table-column prop="status" label="状态" width="80">
              <template #default="{ row }">
                <el-tag
                  :type="row.status === 'published' ? 'success' : row.status === 'draft' ? 'info' : 'warning'"
                  size="small"
                >
                  {{ statusMap[row.status] || row.status }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>最新事件</span>
              <el-button text type="primary" size="small" @click="$router.push('/admin/events')">
                查看全部
              </el-button>
            </div>
          </template>
          <el-table :data="events" size="small" stripe v-loading="loading">
            <el-table-column prop="type" label="事件类型" width="160">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="subject" label="主题" show-overflow-tooltip />
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { threadApi, userApi } from '@/api'
import { categoryApi, eventApi, pluginApi } from '@/api'
import { User, Document, FolderOpened, Connection, Bell } from '@element-plus/icons-vue'

const loading = ref(false)
const stats = ref({ users: 0, threads: 0, categories: 0, plugins: 0 })
const threads = ref<any[]>([])
const events = ref<any[]>([])

const statusMap: Record<string, string> = {
  draft: '草稿',
  pending_review: '待审核',
  published: '已发布',
  archived: '已归档',
}

onMounted(async () => {
  loading.value = true
  try {
    const [tRes, uRes, cRes, eRes] = await Promise.all([
      threadApi.list({ page: 1, page_size: 5 }),
      userApi.list({ page: 1, page_size: 1 }),
      categoryApi.list(),
      eventApi.list({ limit: 10 }),
    ])
    threads.value = (tRes as any)?.data?.items || []
    stats.value.threads = (tRes as any)?.data?.pagination?.total || 0
    stats.value.users = (uRes as any)?.data?.pagination?.total || 0
    stats.value.categories = Array.isArray((cRes as any)?.data) ? (cRes as any).data.length : 0
    events.value = (eRes as any)?.data?.items || []

    try {
      const p = await pluginApi.list()
      stats.value.plugins = (p as any)?.data?.total || 0
    } catch {
      // 插件接口可能不可用
    }
  } catch {
    // 静默处理
  }
  loading.value = false
})
</script>

<style scoped>
.admin-dashboard {
  max-width: 1400px;
}

.stat-row {
  margin-bottom: 20px;
}

.stat-card {
  border-radius: 8px;
}

.stat-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.stat-info {
  flex: 1;
}

.stat-title {
  font-size: 14px;
  color: #909399;
  margin-bottom: 8px;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: #303133;
}

.stat-icon {
  font-size: 48px;
  opacity: 0.15;
}

.stat-users .stat-icon { color: #409eff; }
.stat-threads .stat-icon { color: #67c23a; }
.stat-categories .stat-icon { color: #e6a23c; }
.stat-plugins .stat-icon { color: #909399; }

.stat-users .stat-value { color: #409eff; }
.stat-threads .stat-value { color: #67c23a; }
.stat-categories .stat-value { color: #e6a23c; }
.stat-plugins .stat-value { color: #909399; }

.quick-actions {
  margin-bottom: 20px;
}

.data-row {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>