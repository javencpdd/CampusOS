<template>
  <div class="listing-editor" v-loading="loading">
    <section class="editor-heading">
      <div>
        <h2>{{ editing ? '编辑二手信息' : '发布校园二手' }}</h2>
        <p>平台不提供支付、担保或仲裁。请在线下确认物品和交易安排，不要公开敏感个人信息。</p>
      </div>
      <router-link to="/secondhand"><el-button>返回列表</el-button></router-link>
    </section>

    <el-alert
      v-if="featureDisabled"
      type="warning"
      :closable="false"
      show-icon
      title="校园二手功能暂未启用"
      description="当前不能创建或编辑二手信息。"
    />

    <el-form v-else label-position="top" @submit.prevent="submit">
      <el-form-item label="标题" required>
        <el-input v-model="form.title" maxlength="255" show-word-limit placeholder="简明描述物品和主要信息" />
      </el-form-item>
      <el-form-item v-if="!editing" label="发布板块" required>
        <el-select
          v-model="form.category_id"
          filterable
          class="field-full"
          placeholder="请选择已允许二手发布的板块"
          @change="checkCategoryPolicy"
        >
          <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
        </el-select>
        <p v-if="policyHint" :class="{ invalid: !categoryAllowed }" class="field-hint">{{ policyHint }}</p>
      </el-form-item>
      <el-form-item label="物品说明" required>
        <SafeContentEditor
          v-model="form.content"
          v-model:content-format="form.content_format"
          plain-placeholder="说明品牌、使用情况、配件、瑕疵和交易注意事项。"
          html-placeholder="<p>说明品牌、使用情况、配件、瑕疵和交易注意事项。</p>"
        />
      </el-form-item>
      <div class="field-grid">
        <el-form-item label="标价（元）" required>
          <el-input-number v-model="priceYuan" :min="0" :precision="2" :step="1" class="field-full" />
          <p class="field-hint">按人民币元填写，系统以“分”保存。</p>
        </el-form-item>
        <el-form-item label="物品成色" required>
          <el-select v-model="form.item_condition" class="field-full">
            <el-option label="全新" value="new" />
            <el-option label="近新" value="like_new" />
            <el-option label="良好" value="good" />
            <el-option label="一般" value="fair" />
          </el-select>
        </el-form-item>
      </div>
      <div class="field-grid">
        <el-form-item label="交易方式" required>
          <el-select v-model="form.trade_method" class="field-full">
            <el-option label="当面交易" value="in_person" />
            <el-option label="校内送达" value="campus_dropoff" />
            <el-option label="其他自行约定" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="大致位置">
          <el-input v-model="form.location_scope" maxlength="160" show-word-limit placeholder="例如：主图书馆附近" />
        </el-form-item>
      </div>
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
      <div class="form-actions">
        <el-button @click="router.push('/secondhand')">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="!editing && !categoryAllowed" native-type="submit">
          {{ editing ? '保存修改' : '发布二手信息' }}
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
import SafeContentEditor from '@/modules/community/components/SafeContentEditor.vue'
import { hasMeaningfulContent } from '@/modules/community/content'
import { secondhandApi, type SecondhandRequest } from '../api'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const featureDisabled = ref(false)
const categories = ref<Array<{ id: string; name: string; is_closed?: boolean; node_kind?: string }>>([])
const categoryAllowed = ref(false)
const policyHint = ref('')
const priceYuan = ref(0)
const editing = computed(() => Boolean(route.params.id))
const form = reactive<SecondhandRequest>({
  title: '',
  content: '',
  content_format: 'safe_html',
  category_id: '',
  tags: [],
  price_minor: 0,
  currency: 'CNY',
  item_condition: 'good',
  trade_method: 'in_person',
  location_scope: '',
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
    categoryAllowed.value = policies.some((item: any) => item.thread_type === 'secondhand' && item.enabled)
    policyHint.value = categoryAllowed.value
      ? '该板块允许发布校园二手信息。'
      : '该板块尚未允许校园二手类型，请选择其他板块或联系管理员配置。'
  } catch {
    policyHint.value = '无法确认板块类型策略，请稍后重试。'
  }
}

const loadExisting = async () => {
  if (!editing.value) return
  loading.value = true
  try {
    const response: any = await secondhandApi.getMine(String(route.params.id))
    const result = response?.data
    const thread = result?.thread
    const detail = result?.detail
    if (!thread || !detail) throw new Error('missing secondhand detail')
    form.title = thread.title || ''
    form.content = thread.content || ''
    form.content_format = thread.content_format === 'safe_html' ? 'safe_html' : 'markdown'
    form.category_id = thread.category_id || ''
    form.tags = thread.tags || []
    form.price_minor = detail.price_minor || 0
    form.currency = 'CNY'
    form.item_condition = detail.item_condition
    form.trade_method = detail.trade_method
    form.location_scope = detail.location_scope || ''
    form.version = detail.version
    priceYuan.value = Number(detail.price_minor || 0) / 100
    categoryAllowed.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载二手信息失败')
    router.replace('/secondhand')
  } finally {
    loading.value = false
  }
}

const submit = async () => {
  if (
    !form.title.trim() ||
    !hasMeaningfulContent(form.content, form.content_format) ||
    (!editing.value && !form.category_id)
  ) {
    ElMessage.warning('请填写标题、物品说明和发布板块')
    return
  }
  if (!Number.isFinite(priceYuan.value) || priceYuan.value < 0) {
    ElMessage.warning('请填写合法的人民币标价')
    return
  }
  saving.value = true
  try {
    const payload: SecondhandRequest = {
      ...form,
      title: form.title.trim(),
      content: form.content.trim(),
      price_minor: Math.round(priceYuan.value * 100),
      currency: 'CNY',
      location_scope: form.location_scope?.trim() || '',
    }
    const response: any = editing.value
      ? await secondhandApi.update(String(route.params.id), payload)
      : await secondhandApi.create(payload)
    const id = response?.data?.thread?.id || route.params.id
    ElMessage.success(editing.value ? '二手信息已保存' : '二手信息已发布')
    router.replace('/secondhand/' + id)
  } catch (error: any) {
    if (error?.code === 50301) featureDisabled.value = true
    else ElMessage.error(error?.msg || '提交二手信息失败')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    const response: any = await secondhandApi.status()
    featureDisabled.value = response?.data?.enabled === false
    if (!featureDisabled.value) await loadCategories()
  } catch (error: any) {
    featureDisabled.value = error?.code === 50301
  }
  await loadExisting()
})
</script>

<style scoped>
.listing-editor {
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
  color: var(--campus-muted-color, #606266);
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
