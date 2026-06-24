<template>
  <div class="home">
    <el-card class="welcome-card">
      <template #header>
        <div class="card-header">
          <h1>🚀 欢迎来到 CampusOS</h1>
        </div>
      </template>
      <p class="subtitle">下一代校园社区引擎 — 事件驱动、AI Native 的社区操作系统</p>
      <div class="actions">
        <router-link to="/threads">
          <el-button type="primary" size="large">浏览帖子</el-button>
        </router-link>
        <router-link to="/register" v-if="!userStore.isLoggedIn">
          <el-button size="large">立即注册</el-button>
        </router-link>
      </div>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="注册用户" :value="stats.users" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="帖子总数" :value="stats.threads" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="API 状态" value="正常运行" />
        </el-card>
      </el-col>
    </el-row>

    <el-card class="recent-threads" v-if="threads.length > 0">
      <template #header>
        <div class="card-header">
          <span>最新帖子</span>
          <router-link to="/threads">
            <el-button text>查看全部</el-button>
          </router-link>
        </div>
      </template>
      <el-table :data="threads" style="width: 100%">
        <el-table-column prop="title" label="标题">
          <template #default="{ row }">
            <router-link :to="`/threads/${row.id}`">{{ row.title }}</router-link>
          </template>
        </el-table-column>
        <el-table-column prop="author_name" label="作者" width="120" />
        <el-table-column prop="view_count" label="浏览" width="80" />
        <el-table-column prop="reply_count" label="回复" width="80" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useUserStore } from '@/stores/user'
import { threadApi, userApi, healthApi } from '@/api'

const userStore = useUserStore()
const threads = ref<any[]>([])
const stats = ref({ users: 0, threads: 0 })

onMounted(async () => {
  try {
    await healthApi.check()
    const [threadRes, userRes]: any = await Promise.all([
      threadApi.list({ page: 1, page_size: 5 }),
      userApi.list({ page: 1, page_size: 1 }),
    ])
    threads.value = threadRes?.data?.items || []
    stats.value.threads = threadRes?.data?.pagination?.total || 0
    stats.value.users = userRes?.data?.pagination?.total || 0
  } catch (e) {
    console.log('API not available yet')
  }
})
</script>

<style scoped>
.home { max-width: 900px; margin: 0 auto; }
.welcome-card { margin-bottom: 24px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header h1 { margin: 0; }
.subtitle { color: #606266; font-size: 16px; margin-bottom: 20px; }
.actions { display: flex; gap: 12px; }
.stats-row { margin-bottom: 24px; }
</style>