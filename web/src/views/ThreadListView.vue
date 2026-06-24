<template>
  <div class="thread-list">
    <div class="list-header">
      <h2>帖子列表</h2>
      <div class="search-bar">
        <el-input v-model="keyword" placeholder="搜索帖子..." @keyup.enter="loadThreads" style="width:300px">
          <template #append><el-button @click="loadThreads"><el-icon><Search /></el-icon></el-button></template>
        </el-input>
      </div>
    </div>
    <el-table :data="threads" style="width: 100%" v-loading="loading">
      <el-table-column prop="title" label="标题" min-width="300">
        <template #default="{ row }">
          <router-link :to="`/threads/${row.id}`" class="thread-link">
            <el-tag v-if="row.is_pinned" type="danger" size="small" style="margin-right:8px">置顶</el-tag>
            {{ row.title }}
          </router-link>
        </template>
      </el-table-column>
      <el-table-column prop="author_name" label="作者" width="120" />
      <el-table-column prop="view_count" label="浏览" width="80" />
      <el-table-column prop="reply_count" label="回复" width="80" />
      <el-table-column prop="created_at" label="发布时间" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
    </el-table>
    <div class="pagination" v-if="total > 0">
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" @current-change="loadThreads" layout="prev, pager, next" />
    </div>
    <el-empty v-if="!loading && threads.length === 0" description="暂无帖子" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { threadApi } from '@/api'

const threads = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const keyword = ref('')

const loadThreads = async () => {
  loading.value = true
  try {
    const res: any = await threadApi.list({ page: page.value, page_size: 20, keyword: keyword.value })
    if (res.code === 0) {
      threads.value = res.data?.items || []
      total.value = res.data?.pagination?.total || 0
    }
  } catch (e) { console.error(e) }
  finally { loading.value = false }
}

onMounted(loadThreads)
</script>

<style scoped>
.list-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.thread-link { color: #303133; text-decoration: none; }
.thread-link:hover { color: #409eff; }
.pagination { margin-top: 20px; display: flex; justify-content: center; }
</style>