<template>
  <div class="admin-reviews">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>帖子审核</span>
          <el-tag type="warning" size="small">待审核 {{ pendingThreads.length }} 篇</el-tag>
        </div>
      </template>

      <el-tabs v-model="activeTab" @tab-change="loadThreads">
        <el-tab-pane label="待审核" name="pending_review">
          <el-table :data="pendingThreads" v-loading="loading" stripe border style="width: 100%">
            <el-table-column prop="id" label="ID" width="180" show-overflow-tooltip />
            <el-table-column prop="title" label="标题" min-width="250" show-overflow-tooltip />
            <el-table-column prop="author_name" label="作者" width="120" />
            <el-table-column prop="category_id" label="版块" width="120" />
            <el-table-column prop="created_at" label="提交时间" width="180">
              <template #default="{ row }">
                {{ formatDate(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="250" align="center" fixed="right">
              <template #default="{ row }">
                <el-button-group>
                  <el-button type="success" size="small" plain @click="approveThread(row)">
                    <el-icon><Check /></el-icon> 通过
                  </el-button>
                  <el-button type="info" size="small" plain @click="viewDetail(row)">
                    <el-icon><View /></el-icon> 查看
                  </el-button>
                  <el-popconfirm
                    title="确定要拒绝该帖子吗？"
                    confirm-button-text="拒绝"
                    cancel-button-text="取消"
                    confirm-button-type="danger"
                    @confirm="rejectThread(row)"
                  >
                    <template #reference>
                      <el-button type="danger" size="small" plain>
                        <el-icon><Close /></el-icon> 拒绝
                      </el-button>
                    </template>
                  </el-popconfirm>
                </el-button-group>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!loading && pendingThreads.length === 0" description="暂无待审核帖子" />
        </el-tab-pane>

        <el-tab-pane label="已发布" name="published">
          <el-table :data="publishedThreads" v-loading="loading" stripe border style="width: 100%">
            <el-table-column prop="id" label="ID" width="180" show-overflow-tooltip />
            <el-table-column prop="title" label="标题" min-width="250" show-overflow-tooltip />
            <el-table-column prop="author_name" label="作者" width="120" />
            <el-table-column prop="view_count" label="浏览" width="80" align="center" />
            <el-table-column prop="status" label="状态" width="100" align="center">
              <template #default="{ row }">
                <el-tag type="success" size="small">已发布</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="已拒绝" name="rejected">
          <el-table :data="rejectedThreads" v-loading="loading" stripe border style="width: 100%">
            <el-table-column prop="id" label="ID" width="180" show-overflow-tooltip />
            <el-table-column prop="title" label="标题" min-width="250" show-overflow-tooltip />
            <el-table-column prop="author_name" label="作者" width="120" />
            <el-table-column prop="status" label="状态" width="100" align="center">
              <template #default="{ row }">
                <el-tag type="danger" size="small">已拒绝</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 帖子详情对话框 -->
    <el-dialog v-model="detailVisible" title="帖子详情" width="700px" destroy-on-close>
      <div v-if="selectedThread">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="标题" :span="2">{{ selectedThread.title }}</el-descriptions-item>
          <el-descriptions-item label="作者">{{ selectedThread.author_name }}</el-descriptions-item>
          <el-descriptions-item label="版块">{{ selectedThread.category_id }}</el-descriptions-item>
          <el-descriptions-item label="提交时间" :span="2">{{ formatDate(selectedThread.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <div style="margin-top: 16px">
          <h4 style="margin-bottom: 8px; color: #606266">帖子内容：</h4>
          <div class="thread-content">{{ selectedThread.content }}</div>
        </div>
      </div>
      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
        <el-button type="success" @click="approveThread(selectedThread)">通过</el-button>
        <el-button type="danger" @click="rejectThread(selectedThread)">拒绝</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, Close, View } from '@element-plus/icons-vue'
import { threadApi } from '@/api'

const activeTab = ref('pending_review')
const loading = ref(false)
const pendingThreads = ref<any[]>([])
const publishedThreads = ref<any[]>([])
const rejectedThreads = ref<any[]>([])
const detailVisible = ref(false)
const selectedThread = ref<any>(null)

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadThreads = async () => {
  loading.value = true
  try {
    const r = (await threadApi.list({ page: 1, page_size: 100 })) as any
    const threads = r?.data?.items || []
    pendingThreads.value = threads.filter((t: any) => t.status === 'pending_review')
    publishedThreads.value = threads.filter((t: any) => t.status === 'published')
    rejectedThreads.value = threads.filter((t: any) => t.status === 'rejected' || t.status === 'archived')
  } catch {
    ElMessage.error('加载帖子列表失败')
  }
  loading.value = false
}

const approveThread = async (thread: any) => {
  if (!thread) return
  try {
    await threadApi.update(thread.id, { status: 'published' })
    ElMessage.success('帖子已通过审核')
    detailVisible.value = false
    loadThreads()
  } catch {
    ElMessage.error('操作失败')
  }
}

const rejectThread = async (thread: any) => {
  if (!thread) return
  try {
    await threadApi.update(thread.id, { status: 'rejected' })
    ElMessage.success('帖子已拒绝')
    detailVisible.value = false
    loadThreads()
  } catch {
    ElMessage.error('操作失败')
  }
}

const viewDetail = (thread: any) => {
  selectedThread.value = thread
  detailVisible.value = true
}

onMounted(loadThreads)
</script>

<style scoped>
.admin-reviews {
  max-width: 1400px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.thread-content {
  background: #f5f7fa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 16px;
  font-size: 14px;
  line-height: 1.8;
  max-height: 300px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>