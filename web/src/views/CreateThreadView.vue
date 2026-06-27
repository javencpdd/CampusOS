<template>
  <div class="create-thread">
    <el-card>
      <template #header><h2>发布帖子</h2></template>
      <el-form :model="form" @submit.prevent="handleSubmit" label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" placeholder="请输入帖子标题" maxlength="255" show-word-limit />
        </el-form-item>
        <el-form-item label="版块" required>
          <el-select
            v-model="form.category_id"
            :loading="categoryLoading"
            filterable
            placeholder="请选择版块"
            style="width: 100%"
          >
            <el-option
              v-for="category in categories"
              :key="category.id"
              :label="category.name"
              :value="category.id"
            />
          </el-select>
          <div v-if="!categoryLoading && categories.length === 0" class="field-hint">
            暂无可用版块，请先在后台创建版块
          </div>
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
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { categoryApi, threadApi } from '@/api'
import { ElMessage } from 'element-plus'

const router = useRouter()
const loading = ref(false)
const categoryLoading = ref(false)
const categories = ref<Array<{ id: string; name: string }>>([])
const form = reactive({ title: '', content: '', category_id: '', tags: [] as string[] })

const loadCategories = async () => {
  categoryLoading.value = true
  try {
    const res: any = await categoryApi.list()
    if (res.code === 0) {
      categories.value = res.data || []
      if (!form.category_id && categories.value.length > 0) {
        form.category_id = categories.value[0].id
      }
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '获取版块失败')
  } finally {
    categoryLoading.value = false
  }
}

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

onMounted(loadCategories)
</script>

<style scoped>
.create-thread { max-width: 800px; margin: 0 auto; }
.field-hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
  margin-top: 4px;
}
</style>
