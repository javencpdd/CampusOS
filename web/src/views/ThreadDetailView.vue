<template>
  <div class="thread-detail" v-loading="loading">
    <el-card v-if="thread">
      <template #header>
        <h2>{{ thread.title }}</h2>
        <div class="meta">
          <span>作者：{{ thread.author_name }}</span>
          <span>发布于：{{ new Date(thread.created_at).toLocaleString() }}</span>
          <span>浏览：{{ thread.view_count }}</span>
          <el-tag v-for="tag in thread.tags" :key="tag" size="small" style="margin-left:8px">{{ tag }}</el-tag>
        </div>
      </template>
      <div class="content">{{ thread.content }}</div>
    </el-card>
    <el-empty v-if="!loading && !thread" description="帖子不存在" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { threadApi } from '@/api'

const route = useRoute()
const thread = ref<any>(null)
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    const res: any = await threadApi.get(route.params.id as string)
    if (res.code === 0) thread.value = res.data
  } catch (e) { console.error(e) }
  finally { loading.value = false }
})
</script>

<style scoped>
.meta { display: flex; gap: 16px; color: #909399; font-size: 14px; flex-wrap: wrap; align-items: center; }
.content { line-height: 1.8; white-space: pre-wrap; }
</style>