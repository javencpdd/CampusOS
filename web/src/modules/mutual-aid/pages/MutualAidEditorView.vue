<template>
  <div class="aid-editor" v-loading="loading">
    <section class="editor-heading">
      <div>
        <h2>{{ editing ? '编辑互助信息' : '发布校园互助' }}</h2>
        <p>不要在公开内容中填写身份证、银行卡、完整宿舍地址或付款担保信息。</p>
      </div>
      <router-link to="/mutual-aid"><el-button>返回列表</el-button></router-link>
    </section>

    <el-alert
      v-if="featureDisabled"
      type="warning"
      :closable="false"
      show-icon
      title="校园互助功能暂未启用"
      description="当前不能创建或编辑互助信息。"
    />

    <el-form v-else label-position="top" @submit.prevent="submit">
      <el-form-item label="标题" required>
        <el-input v-model="form.title" maxlength="255" show-word-limit placeholder="简明说明你需要或提供什么帮助" />
      </el-form-item>
      <el-form-item label="互助类型" required>
        <el-radio-group v-model="form.aid_type">
          <el-radio-button label="request">求助</el-radio-button>
          <el-radio-button label="offer">提供帮助</el-radio-button>
          <el-radio-button label="volunteer">志愿服务</el-radio-button>
          <el-radio-button label="resource_share">资源共享</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="!editing" label="发布板块" required>
        <el-select
          v-model="form.category_id"
          filterable
          class="field-full"
          placeholder="请选择已允许互助发布的板块"
          @change="checkCategoryPolicy"
        >
          <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
        </el-select>
        <p v-if="policyHint" :class="{ invalid: !categoryAllowed }" class="field-hint">{{ policyHint }}</p>
      </el-form-item>
      <el-form-item label="正文" required>
        <el-input
          v-model="form.content"
          type="textarea"
          :rows="10"
          maxlength="5000"
          show-word-limit
          placeholder="说明时间、需求、可提供的帮助和必要的注意事项。"
        />
      </el-form-item>
      <div class="field-grid">
        <el-form-item label="大致位置">
          <el-input v-model="form.location_scope" maxlength="160" show-word-limit placeholder="例如：主图书馆附近" />
        </el-form-item>
        <el-form-item label="截止时间">
          <el-date-picker
            v-model="deadlineInput"
            type="datetime"
            value-format="YYYY-MM-DDTHH:mm:ss"
            class="field-full"
            placeholder="可选"
          />
        </el-form-item>
      </div>
      <div class="field-grid">
        <el-form-item label="联系方式" required>
          <el-select v-model="form.contact_mode" class="field-full">
            <el-option label="评论区联系" value="comment" />
            <el-option label="站内联系" value="in_app" />
            <el-option label="邮箱联系" value="email" />
            <el-option label="其他约定方式" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="form.tags"
            multiple
            filterable
            allow-create
            default-first-option
            class="field-full"
            placeholder="输入后回车"
          />
        </el-form-item>
      </div>
      <div class="form-actions">
        <el-button @click="router.push('/mutual-aid')">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="!editing && !categoryAllowed" native-type="submit">
          {{ editing ? '保存修改' : '发布互助' }}
        </el-button>
      </div>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { categoryApi } from '@/modules/community/api'
import { mutualAidApi, type MutualAidRequest } from '../api'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const featureDisabled = ref(false)
const categories = ref<Array<{ id: string; name: string; is_closed?: boolean; node_kind?: string }>>([])
const categoryAllowed = ref(false)
const policyHint = ref('')
const deadlineInput = ref('')
const editing = computed(() => Boolean(route.params.id))
const form = reactive<MutualAidRequest>({
  title: '',
  content: '',
  category_id: '',
  tags: [],
  aid_type: 'request',
  location_scope: '',
  contact_mode: 'comment',
})

const loadCategories = async () => {
  const response: any = await categoryApi.list()
  const rows = response?.data?.items || response?.data || []
  categories.value = rows.filter((item: any) => !item.is_closed && (item.node_kind || 'board') === 'board')
}

const checkCategoryPolicy = async () => {
  categoryAllowed.value = false
  policyHint.value = ''
  if (!form.category_id) return
  try {
    const response: any = await categoryApi.threadTypes(form.category_id)
    const policies = response?.data?.items || []
    categoryAllowed.value = policies.some((item: any) => item.thread_type === 'mutual_aid' && item.enabled)
    policyHint.value = categoryAllowed.value
      ? '该板块允许发布校园互助信息。'
      : '该板块尚未允许校园互助类型，请选择其他板块或联系管理员配置。'
  } catch {
    policyHint.value = '无法确认板块类型策略，请稍后重试。'
  }
}

const loadExisting = async () => {
  if (!editing.value) return
  loading.value = true
  try {
    const response: any = await mutualAidApi.getMine(String(route.params.id))
    const result = response?.data
    const thread = result?.thread
    const detail = result?.detail
    if (!thread || !detail) throw new Error('missing mutual aid detail')
    form.title = thread.title || ''
    form.content = thread.content || ''
    form.category_id = thread.category_id || ''
    form.tags = thread.tags || []
    form.aid_type = detail.aid_type
    form.location_scope = detail.location_scope || ''
    form.contact_mode = detail.contact_mode
    form.version = detail.version
    deadlineInput.value = detail.deadline ? new Date(detail.deadline).toISOString().slice(0, 19) : ''
    categoryAllowed.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载互助信息失败')
    router.replace('/mutual-aid')
  } finally {
    loading.value = false
  }
}

const submit = async () => {
  if (!form.title.trim() || !form.content.trim() || (!editing.value && !form.category_id)) {
    ElMessage.warning('请填写标题、正文和发布板块')
    return
  }
  saving.value = true
  try {
    const payload: MutualAidRequest = {
      ...form,
      title: form.title.trim(),
      content: form.content.trim(),
      location_scope: form.location_scope?.trim() || '',
      deadline: deadlineInput.value ? new Date(deadlineInput.value).toISOString() : null,
    }
    const response: any = editing.value
      ? await mutualAidApi.update(String(route.params.id), payload)
      : await mutualAidApi.create(payload)
    const id = response?.data?.thread?.id || route.params.id
    ElMessage.success(editing.value ? '互助信息已保存' : '互助信息已发布')
    router.replace(`/mutual-aid/${id}`)
  } catch (error: any) {
    if (error?.code === 50301) featureDisabled.value = true
    else ElMessage.error(error?.msg || '提交互助信息失败')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    const response: any = await mutualAidApi.status()
    featureDisabled.value = response?.data?.enabled === false
    if (!featureDisabled.value) await loadCategories()
  } catch (error: any) {
    featureDisabled.value = error?.code === 50301
  }
  await loadExisting()
})
</script>

<style scoped>
.aid-editor {
  max-width: 860px;
  margin: 0 auto;
  display: grid;
  gap: 18px;
}
.editor-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid #dcdfe6;
}
.editor-heading h2,
.editor-heading p {
  margin: 0;
}
.editor-heading p {
  margin-top: 8px;
  color: var(--campus-muted-color, #606266);
  line-height: 1.6;
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.field-full {
  width: 100%;
}
.field-hint {
  margin: 6px 0 0;
  color: #438663;
  font-size: 13px;
  line-height: 1.5;
}
.field-hint.invalid {
  color: #b54747;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding-top: 8px;
}
@media (max-width: 640px) {
  .editor-heading,
  .field-grid {
    grid-template-columns: 1fr;
    flex-direction: column;
  }
}
</style>
