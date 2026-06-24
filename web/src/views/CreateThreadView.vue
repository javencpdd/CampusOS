<template>
  <div class="create-thread">
    <el-card>
      <template #header><h2>发布帖子</h2></template>
      <el-form :model="form" @submit.prevent="handleSubmit" label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" placeholder="请输入帖子标题" maxlength="255" show-word-limit />
        </el-form-item>
        <el-form-item label="版块" required>
          <el-input v-model="form.category_id" placeholder="版块 ID（如 cat_001）" />
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="form.content" type="textarea" :rows="10" placeholder="请输入帖子内容（支持 Markdown）" />
        </el-form-item>
        <el-form-item label="标签">
          <el-select v-model="form.tags" multiple filterable allow-create placeholder="输入标签后回车" style="width:100%">
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSubmit" :loading="loading">发布帖子</el-button>
          <el-button @click="$router.back()">取消</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { threadApi } from '@/api'
import { ElMessage } from 'element-plus'

const router = useRouter()
const loading = ref(false)
const form = reactive({ title: '', content: '', category_id: '', tags: [] as string[] })

const handleSubmit = async () => {
  if (!form.title || !form.content || !form.category_id) return ElMessage.warning('请填写标题、内容和版块')
  loading.value = true
  try {
    const res: any = await threadApi.create(form)
    if (res.code === 0) {
      ElMessage.success('发布成功')
      router.push(`/threads/${res.data.id}`)
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '发布失败')
  } finally { loading.value = false }
}
</script>

<style scoped>
.create-thread { max-width: 800px; margin: 0 auto; }
</style>