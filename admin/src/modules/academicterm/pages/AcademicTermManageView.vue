<template>
  <div class="academic-term-page" v-loading="loading">
    <header class="page-header">
      <div>
        <p class="eyebrow">AcademicTerm Core</p>
        <h2>学期治理</h2>
        <p>只有开放学期可供用户新建、保存或导入课表；关闭后历史课表仍可读取。每次管理操作均需填写原因并携带当前版本。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" circle title="刷新" aria-label="刷新学期" @click="load" />
        <el-button type="primary" :icon="Plus" @click="openCreate">创建学期</el-button>
      </div>
    </header>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="默认学期只能是开放状态；切换默认会原子撤销旧默认。第一周开始日期必须是星期一，修改只影响之后创建的空课表。"
    />

    <el-table :data="items" border stripe class="term-table">
      <el-table-column prop="display_name" label="学期" min-width="170">
        <template #default="{ row }">
          <strong>{{ row.display_name }}</strong>
          <small>{{ row.year }} · {{ semesterLabel(row.semester) }}</small>
        </template>
      </el-table-column>
      <el-table-column prop="first_week_start" label="第一周开始" width="135" />
      <el-table-column label="状态" width="130" align="center">
        <template #default="{ row }">
          <el-tag :type="row.status === 'open' ? 'success' : 'info'">{{ row.status === 'open' ? '开放' : '已关闭' }}</el-tag>
          <el-tag v-if="row.is_default" type="warning" effect="plain" class="default-tag">默认</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="课表引用" width="105" align="center">
        <template #default="{ row }">
          <el-tag :type="row.schedule_reference_count ? 'warning' : 'info'" effect="plain">{{ row.schedule_reference_count || 0 }} 个</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="版本" width="92" align="center"><template #default="{ row }">v{{ row.version }}</template></el-table-column>
      <el-table-column label="更新时间" min-width="175"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
      <el-table-column label="操作" min-width="320" fixed="right">
        <template #default="{ row }">
          <el-button size="small" plain @click="openEdit(row)">修改首周</el-button>
          <el-button v-if="row.status === 'open'" size="small" type="warning" plain @click="transition(row, 'close')">关闭</el-button>
          <el-button v-else size="small" type="success" plain @click="transition(row, 'open')">重新开放</el-button>
          <el-button v-if="row.status === 'open' && !row.is_default" size="small" type="primary" plain @click="transition(row, 'default')">设为默认</el-button>
          <el-button size="small" type="danger" text :disabled="Boolean(row.schedule_reference_count)" :title="row.schedule_reference_count ? '已有用户课表引用，不能删除' : '删除学期'" @click="transition(row, 'delete')">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && items.length === 0" description="尚未配置学期；用户不能创建任意学期课表" />

    <el-dialog v-model="formVisible" :title="editing ? '修改学期第一周' : '创建学期'" width="min(620px, calc(100vw - 24px))" destroy-on-close>
      <el-form label-position="top">
        <template v-if="!editing">
          <div class="term-pair">
            <el-form-item label="学年" required><el-input-number v-model="form.year" :min="2000" :max="2200" controls-position="right" /></el-form-item>
            <el-form-item label="学期" required><el-select v-model="form.semester"><el-option label="春季学期" value="spring" /><el-option label="秋季学期" value="fall" /></el-select></el-form-item>
            <el-form-item label="初始状态" required><el-select v-model="form.status"><el-option label="开放" value="open" /><el-option label="已关闭" value="closed" /></el-select></el-form-item>
          </div>
          <el-form-item label="设为默认开放学期"><el-switch v-model="form.is_default" :disabled="form.status === 'closed'" /></el-form-item>
        </template>
        <el-form-item label="第一周开始日期" required><el-date-picker v-model="form.first_week_start" value-format="YYYY-MM-DD" type="date" placeholder="必须为星期一" /></el-form-item>
        <el-form-item label="操作原因" required><el-input v-model="form.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="至少 2 个字符，供审计和事后追踪" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="submitForm">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { academicTermApi, type AcademicTerm } from '../api'

const loading = ref(false)
const saving = ref(false)
const formVisible = ref(false)
const editing = ref<AcademicTerm | null>(null)
const items = ref<AcademicTerm[]>([])
const form = reactive({ year: new Date().getFullYear(), semester: 'fall', status: 'open', is_default: false, first_week_start: '', reason: '' })

const dataOf = (value: any) => value?.data ?? value
const itemsOf = (value: any): AcademicTerm[] => Array.isArray(dataOf(value)?.items) ? dataOf(value).items : []
const messageOf = (error: any, fallback: string) => error?.msg || error?.message || fallback
const semesterLabel = (semester: string) => semester === 'spring' ? '春季' : '秋季'
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '-'

const resetForm = () => Object.assign(form, { year: new Date().getFullYear(), semester: 'fall', status: 'open', is_default: false, first_week_start: '', reason: '' })
const load = async () => {
  loading.value = true
  try { items.value = itemsOf(await academicTermApi.list()) }
  catch (error: any) { ElMessage.error(messageOf(error, '加载学期目录失败')) }
  finally { loading.value = false }
}
const openCreate = () => { editing.value = null; resetForm(); formVisible.value = true }
const openEdit = (item: AcademicTerm) => {
  editing.value = item
  Object.assign(form, { year: item.year, semester: item.semester, status: item.status, is_default: item.is_default, first_week_start: item.first_week_start, reason: '' })
  formVisible.value = true
}
const validate = () => {
  if (!form.first_week_start) return '请选择第一周开始日期'
  if (new Date(`${form.first_week_start}T00:00:00`).getDay() !== 1) return '第一周开始日期必须是星期一'
  if (form.reason.trim().length < 2) return '请填写至少 2 个字符的操作原因'
  return ''
}
const submitForm = async () => {
  const invalid = validate(); if (invalid) return ElMessage.warning(invalid)
  saving.value = true
  try {
    if (editing.value) await academicTermApi.updateFirstWeek(editing.value.id, { first_week_start: form.first_week_start, expected_version: editing.value.version, reason: form.reason.trim() })
    else await academicTermApi.create({ year: form.year, semester: form.semester, status: form.status, is_default: form.is_default, first_week_start: form.first_week_start, reason: form.reason.trim() })
    ElMessage.success(editing.value ? '第一周开始日期已更新' : '学期已创建')
    formVisible.value = false; await load()
  } catch (error: any) { ElMessage.error(messageOf(error, '保存学期失败')) }
  finally { saving.value = false }
}
const transition = async (item: AcademicTerm, action: 'open' | 'close' | 'default' | 'delete') => {
  const label = ({ open: '重新开放', close: '关闭', default: '设为默认', delete: '删除' } as const)[action]
  let reason = ''
  try {
    const result: any = await ElMessageBox.prompt(`请说明“${item.display_name}”${label}的原因（至少 2 个字符）。`, `${label}学期`, { inputPattern: /\S[\s\S]+/, inputErrorMessage: '请填写至少 2 个字符的操作原因', confirmButtonText: '确认', cancelButtonText: '取消' })
    reason = String(result.value || '').trim()
  } catch { return }
  try {
    const data = { expected_version: item.version, reason }
    if (action === 'open') await academicTermApi.open(item.id, data)
    else if (action === 'close') await academicTermApi.close(item.id, data)
    else if (action === 'default') await academicTermApi.setDefault(item.id, data)
    else await academicTermApi.remove(item.id, data)
    ElMessage.success(`学期已${label}`); await load()
  } catch (error: any) { ElMessage.error(messageOf(error, `${label}失败；如数据已被其他管理员更新，请刷新后重试`)) }
}
onMounted(load)
</script>

<style scoped>
.academic-term-page { display: grid; gap: 16px; max-width: 1450px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 20px; border: 1px solid #e4e7ed; border-radius: 6px; background: #fff; }
.page-header h2 { margin: 0; }.page-header p:last-child { margin: 6px 0 0; color: #606266; line-height: 1.6; }.eyebrow { margin: 0 0 6px; color: #166534; font-size: 12px; font-weight: 700; text-transform: uppercase; }.header-actions { display: flex; gap: 8px; flex-wrap: wrap; }.term-table { width: 100%; }.term-table strong, .term-table small { display: block; }.term-table small { margin-top: 4px; color: #909399; }.default-tag { margin-left: 6px; }.term-pair { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
@media (max-width: 720px) { .page-header { flex-direction: column; padding: 14px; }.header-actions { width: 100%; }.header-actions .el-button:last-child { flex: 1; }.term-pair { grid-template-columns: 1fr; } }
</style>
