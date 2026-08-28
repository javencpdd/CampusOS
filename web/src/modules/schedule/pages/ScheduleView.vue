<template>
  <div ref="scheduleRoot" class="schedule-view" :data-layout-mode="layoutMode" v-loading="loading">
    <el-card class="schedule-card" shadow="never">
      <template #header>
        <div class="schedule-header">
          <div>
            <h2>个人课表</h2>
            <div class="schedule-meta">
              <span>{{ termLabel }}</span>
              <span>第 {{ selectedWeek }} 周</span>
              <span>{{ weekRangeText }}</span>
            </div>
          </div>
          <div class="header-actions">
            <el-button @click="prevWeek">上一周</el-button>
            <el-tooltip content="回到今天所在日期，并同步显示对应课程周次" placement="top">
              <el-button @click="goCurrentWeek">本周</el-button>
            </el-tooltip>
            <el-button @click="nextWeek">下一周</el-button>
            <el-button type="primary" :disabled="termReadOnly" @click="saveSchedule" :loading="saving">保存</el-button>
          </div>
        </div>
      </template>

      <el-alert v-if="disabled" type="warning" :closable="false" show-icon title="个人课表插件当前未启用" />
      <el-alert v-else-if="!availableTerms.length" type="info" :closable="false" show-icon title="管理员尚未开放教学学期；暂不能新建或编辑课表。" />
      <el-alert v-else-if="termReadOnly" type="info" :closable="false" show-icon title="当前为已关闭或历史学期，只读查看；请切换到开放学期后编辑。" />

      <div class="toolbar">
        <el-tooltip content="选择本人已保存的学期；关闭学期仅可读取" placement="top">
          <el-select
            v-model="selectedTermKey"
            class="term-select"
            placeholder="选择已保存课表"
            @change="selectSavedTerm"
          >
            <el-option
              v-for="term in terms"
              :key="termKey(term.term_year, term.semester)"
              :label="termOptionLabel(term)"
              :value="termKey(term.term_year, term.semester)"
            />
          </el-select>
        </el-tooltip>
        <el-tooltip content="只显示管理员开放的春季或秋季学期；选中后可打开或新建自己的课表" placement="top">
          <el-select v-model="selectedAvailableTermKey" class="term-select" placeholder="选择平台开放学期" @change="selectAvailableTerm">
            <el-option v-for="term in availableTerms" :key="termKey(term.year, term.semester)" :label="availableTermLabel(term)" :value="termKey(term.year, term.semester)" />
          </el-select>
        </el-tooltip>
        <el-tooltip content="只可为管理员开放的学期打开或新建课表，第一周开始日期由平台目录设定" placement="top">
          <el-button type="primary" :disabled="!selectedAvailableTermKey" @click="openTerm" :loading="termSwitching">打开/新建受管课表</el-button>
        </el-tooltip>
        <el-tooltip content="设置每天显示的课程节数" placement="top">
          <el-input-number
            v-model="schedule.settings.periods_per_day"
            :min="1"
            :max="24"
            :controls-position="isCompact ? '' : 'right'"
            aria-label="每日课程节数"
            @change="markDirty"
          />
        </el-tooltip>
        <el-switch
          v-model="schedule.settings.show_weekend"
          active-text="显示周末"
          inactive-text="工作日"
          @change="markDirty"
        />
        <el-tooltip content="Excel、CSV 或 JSON 导入的数据只会写入当前选中的学期课表" placement="top">
          <el-checkbox v-model="replaceImport">导入时替换</el-checkbox>
        </el-tooltip>
        <input ref="importInput" class="hidden-input" type="file" accept=".xls,.csv,.json" @change="handleImport" />
        <el-tooltip content="导入课程到当前学期对应的 JSON 课表" placement="top">
          <el-button :disabled="termReadOnly" @click="chooseImport" :loading="importing">导入</el-button>
        </el-tooltip>
        <el-button :disabled="termReadOnly" @click="openCourseDialog()">新增课程</el-button>
        <el-tooltip content="仅编辑课程、显示设置和元数据；学期与第一周开始日期由管理员治理" placement="top">
          <el-button :disabled="termReadOnly" @click="openJsonEditor">编辑 JSON</el-button>
        </el-tooltip>
      </div>

      <el-tabs v-model="viewMode" class="schedule-tabs">
        <el-tab-pane label="周课表" name="week">
          <div v-if="isCompact" class="schedule-agenda" aria-label="紧凑周课程列表">
            <section v-for="day in weekdays" :key="day.value" class="agenda-day">
              <header>
                <strong>{{ day.label }}</strong>
                <span>{{ weekDate(selectedWeek, day.value - 1) }}</span>
              </header>
              <div v-if="coursesForDay(day.value).length" class="agenda-course-list">
                <button
                  v-for="course in coursesForDay(day.value)"
                  :key="course.id"
                  class="agenda-course"
                  :style="{ borderLeftColor: course.color, backgroundColor: softColor(course.color) }"
                  @click="openCourseDialog(course)"
                >
                  <strong>{{ course.name }}</strong>
                  <span>第 {{ course.start_period }}-{{ course.end_period }} 节</span>
                  <span v-if="course.location || course.teacher">{{
                    [course.location, course.teacher].filter(Boolean).join(' · ')
                  }}</span>
                </button>
              </div>
              <span v-else class="agenda-empty">暂无课程</span>
            </section>
          </div>
          <div v-else class="schedule-grid" :style="{ '--day-count': String(weekdays.length) }">
            <div class="grid-corner">节次</div>
            <div v-for="day in weekdays" :key="day.value" class="grid-day">
              {{ day.label }}
            </div>
            <template v-for="period in periods" :key="period">
              <div class="grid-period">{{ periodLabel(period) }}</div>
              <div v-for="day in weekdays" :key="`${day.value}-${period}`" class="grid-cell">
                <button
                  v-for="course in coursesAt(day.value, period)"
                  :key="course.id"
                  class="course-chip"
                  :style="{ borderColor: course.color, backgroundColor: softColor(course.color) }"
                  @click="openCourseDialog(course)"
                >
                  <strong>{{ course.name }}</strong>
                  <span>{{ course.start_period }}-{{ course.end_period }} 节</span>
                  <span v-if="course.location">{{ course.location }}</span>
                  <span v-if="course.teacher">{{ course.teacher }}</span>
                </button>
              </div>
            </template>
          </div>
        </el-tab-pane>
        <el-tab-pane label="日历" name="calendar">
          <div v-if="isCompact" class="calendar-agenda">
            <el-date-picker v-model="calendarDate" type="date" aria-label="选择日历日期" class="calendar-date-picker" />
            <section v-for="day in calendarAgendaDays" :key="day.date" class="calendar-agenda-day">
              <header>
                <strong>{{ day.label }}</strong>
                <span>{{ day.date }}</span>
              </header>
              <button
                v-for="course in day.courses"
                :key="`${day.date}-${course.id}`"
                class="agenda-course"
                :style="{ borderLeftColor: course.color, backgroundColor: softColor(course.color) }"
                @click="openCourseDialog(course)"
              >
                <strong>{{ course.name }}</strong>
                <span>第 {{ course.start_period }}-{{ course.end_period }} 节</span>
                <span v-if="course.location || course.teacher">{{
                  [course.location, course.teacher].filter(Boolean).join(' · ')
                }}</span>
              </button>
              <span v-if="!day.courses.length" class="agenda-empty">暂无课程</span>
            </section>
          </div>
          <div v-else class="calendar-scroll">
            <el-calendar v-model="calendarDate" class="schedule-calendar">
              <template #date-cell="{ data }">
                <div class="calendar-cell" :class="{ 'calendar-cell-selected': data.isSelected }">
                  <span class="calendar-day-number">{{ calendarDay(data.day) }}</span>
                  <div class="calendar-courses">
                    <span
                      v-for="course in calendarCourses(data.day).slice(0, 3)"
                      :key="`${data.day}-${course.id}`"
                      class="calendar-course"
                      :style="{ borderLeftColor: course.color, backgroundColor: softColor(course.color) }"
                    >
                      {{ course.name }}
                    </span>
                    <span v-if="calendarCourses(data.day).length > 3" class="calendar-overflow">
                      +{{ calendarCourses(data.day).length - 3 }}
                    </span>
                  </div>
                </div>
              </template>
            </el-calendar>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-drawer v-model="courseDrawer" title="课程" :size="isCompact ? '100%' : '420px'">
      <el-form label-position="top" :model="courseForm">
        <el-form-item label="课程名称" required>
          <el-input v-model="courseForm.name" maxlength="120" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="isCompact ? 24 : 12">
            <el-form-item label="星期" required>
              <el-select v-model="courseForm.weekday" class="field-full">
                <el-option v-for="day in allWeekdays" :key="day.value" :label="day.label" :value="day.value" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="isCompact ? 24 : 12">
            <el-form-item label="颜色">
              <el-color-picker v-model="courseForm.color" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="12">
          <el-col :span="isCompact ? 24 : 12">
            <el-form-item label="开始节" required>
              <el-input-number
                v-model="courseForm.start_period"
                :min="1"
                :max="24"
                :controls-position="isCompact ? '' : 'right'"
                aria-label="课程开始节次"
                class="field-full"
              />
            </el-form-item>
          </el-col>
          <el-col :span="isCompact ? 24 : 12">
            <el-form-item label="结束节" required>
              <el-input-number
                v-model="courseForm.end_period"
                :min="1"
                :max="24"
                :controls-position="isCompact ? '' : 'right'"
                aria-label="课程结束节次"
                class="field-full"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="周次">
          <el-input v-model="courseForm.weeks_text" placeholder="1-16,18" />
        </el-form-item>
        <el-form-item label="教师">
          <el-input v-model="courseForm.teacher" maxlength="80" />
        </el-form-item>
        <el-form-item label="地点">
          <el-input v-model="courseForm.location" maxlength="120" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="courseForm.note" type="textarea" :rows="3" maxlength="500" />
        </el-form-item>
      </el-form>
      <div class="drawer-actions">
        <el-button v-if="editingCourseId" type="danger" plain @click="deleteCourse">删除</el-button>
        <span></span>
        <el-button @click="courseDrawer = false">取消</el-button>
        <el-button type="primary" @click="saveCourse">保存课程</el-button>
      </div>
    </el-drawer>

    <el-drawer v-model="jsonDrawer" title="当前学期课表 JSON" :size="isCompact ? '100%' : '55%'">
      <el-input v-model="rawJson" type="textarea" :rows="24" spellcheck="false" class="json-editor" />
      <div class="drawer-actions">
        <span></span>
        <el-button @click="jsonDrawer = false">取消</el-button>
        <el-button type="primary" @click="applyJson">应用</el-button>
      </div>
    </el-drawer>

  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { scheduleApi } from '@/modules/schedule/api'
import { useLayoutCapability } from '@/shared/layout/useLayoutCapability'

interface Course {
  id: string
  code?: string
  name: string
  teacher?: string
  location?: string
  weekday: number
  start_period: number
  end_period: number
  weeks?: number[]
  color?: string
  note?: string
  source?: string
  extra?: Record<string, string>
}

type Semester = 'spring' | 'fall'

interface TermSummary {
  term_year: number
  semester: Semester
  first_week_start: string
  course_count: number
  updated_at: string
  active: boolean
}

interface AvailableTerm {
  id: string
  year: number
  semester: Semester
  display_name: string
  first_week_start: string
  status: 'open'
  is_default: boolean
}

const loading = ref(false)
const scheduleRoot = ref<HTMLElement | null>(null)
const { mode: layoutMode, isCompact } = useLayoutCapability(scheduleRoot)
const saving = ref(false)
const importing = ref(false)
const disabled = ref(false)
const dirty = ref(false)
const selectedWeek = ref(1)
const viewMode = ref<'week' | 'calendar'>('week')
const calendarDate = ref(new Date())
const terms = ref<TermSummary[]>([])
const availableTerms = ref<AvailableTerm[]>([])
const selectedTermKey = ref('')
const selectedAvailableTermKey = ref('')
const termSwitching = ref(false)
const replaceImport = ref(false)
const importInput = ref<HTMLInputElement | null>(null)
const courseDrawer = ref(false)
const jsonDrawer = ref(false)
const editingCourseId = ref('')
const rawJson = ref('')
const week = reactive({ current_week: 1, week_start: '', week_end: '', today: '' })
const termDraft = reactive({
  term_year: new Date().getFullYear(),
  semester: 'spring' as Semester,
})
const schedule = reactive({
  term_year: new Date().getFullYear(),
  semester: 'spring' as Semester,
  first_week_start: '',
  settings: {
    periods_per_day: 12,
    show_weekend: true,
    period_labels: [] as string[],
  },
  courses: [] as Course[],
  metadata: {} as Record<string, any>,
})
const courseForm = reactive({
  name: '',
  teacher: '',
  location: '',
  weekday: 1,
  start_period: 1,
  end_period: 2,
  weeks_text: '1-16',
  color: '#2563eb',
  note: '',
})

const allWeekdays = [
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' },
  { value: 7, label: '周日' },
]

const weekdays = computed(() => (schedule.settings.show_weekend ? allWeekdays : allWeekdays.slice(0, 5)))
const periods = computed(() =>
  Array.from({ length: Number(schedule.settings.periods_per_day || 12) }, (_, index) => index + 1),
)
const formatTermLabel = (termYear: number, semester: Semester) =>
  `${termYear} 年${semester === 'fall' ? '秋季' : '春季'}学期`
const termLabel = computed(() => formatTermLabel(schedule.term_year, schedule.semester))
const termReadOnly = computed(() => !availableTerms.value.some((term) => termKey(term.year, term.semester) === termKey(schedule.term_year, schedule.semester)))
const weekRangeText = computed(() => {
  const start = weekDate(selectedWeek.value, 0)
  const end = weekDate(selectedWeek.value, 6)
  return start && end ? `${start} 至 ${end}` : ''
})
const coursesForSelectedWeek = computed(() =>
  schedule.courses.filter((course) => {
    const weeks = course.weeks || []
    return weeks.length === 0 || weeks.includes(selectedWeek.value)
  }),
)
const calendarAgendaDays = computed(() => {
  const selected = new Date(calendarDate.value)
  const weekday = selected.getDay() === 0 ? 7 : selected.getDay()
  selected.setDate(selected.getDate() - weekday + 1)
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(selected)
    day.setDate(day.getDate() + index)
    const date = formatDate(day)
    return {
      date,
      label: allWeekdays[index].label,
      courses: calendarCourses(date),
    }
  })
})

const unwrap = (res: any) => res?.data || res

const loadSchedule = async () => {
  loading.value = true
  try {
    const [availableResult, termsResult] = await Promise.all([scheduleApi.availableTerms(), scheduleApi.terms()])
    applyAvailableTerms(unwrap(availableResult))
    applyTerms(unwrap(termsResult))
    const scheduleResult = await scheduleApi.me()
    const payload = unwrap(scheduleResult)
    applyPayload(payload)
    dirty.value = false
  } catch (error: any) {
    disabled.value = error?.code === 'feature.disabled'
    ElMessage.error(error?.msg || (availableTerms.value.length ? '加载课表失败' : '管理员尚未开放教学学期'))
  } finally {
    loading.value = false
  }
}

const applyPayload = (payload: any) => {
  disabled.value = payload?.enabled === false
  const value = payload?.schedule || payload
  const fallbackDate = new Date()
  schedule.term_year = Number(value?.term_year || fallbackDate.getFullYear())
  schedule.semester = value?.semester === 'fall' ? 'fall' : 'spring'
  termDraft.term_year = schedule.term_year
  termDraft.semester = schedule.semester
  selectedTermKey.value = termKey(schedule.term_year, schedule.semester)
  if (availableTerms.value.some((term) => termKey(term.year, term.semester) === selectedTermKey.value)) {
    selectedAvailableTermKey.value = selectedTermKey.value
  }
  schedule.first_week_start = value?.first_week_start || ''
  schedule.settings.periods_per_day = Number(value?.settings?.periods_per_day || 12)
  schedule.settings.show_weekend = value?.settings?.show_weekend !== false
  schedule.settings.period_labels = [...(value?.settings?.period_labels || [])]
  schedule.courses = [...(value?.courses || [])]
  schedule.metadata = { ...(value?.metadata || {}) }
  week.current_week = Number(payload?.week?.current_week || 1)
  week.week_start = payload?.week?.week_start || ''
  week.week_end = payload?.week?.week_end || ''
  week.today = payload?.week?.today || ''
  selectedWeek.value = week.current_week
  const calendarStart = parseScheduleDate(week.week_start || schedule.first_week_start)
  if (calendarStart) calendarDate.value = calendarStart
}

const applyTerms = (payload: any) => {
  terms.value = (payload?.items || []).map((term: any) => ({
    term_year: Number(term.term_year),
    semester: term.semester === 'fall' ? 'fall' : 'spring',
    first_week_start: term.first_week_start || '',
    course_count: Number(term.course_count || 0),
    updated_at: term.updated_at || '',
    active: Boolean(term.active),
  }))
  const active = terms.value.find((term) => term.active)
  if (active) selectedTermKey.value = termKey(active.term_year, active.semester)
}

const applyAvailableTerms = (payload: any) => {
  availableTerms.value = (payload?.items || []).map((term: any) => ({
    id: String(term.id || ''),
    year: Number(term.year),
    semester: term.semester === 'fall' ? 'fall' : 'spring',
    display_name: term.display_name || formatTermLabel(Number(term.year), term.semester === 'fall' ? 'fall' : 'spring'),
    first_week_start: term.first_week_start || '',
    status: 'open' as const,
    is_default: Boolean(term.is_default),
  }))
  const current = termKey(schedule.term_year, schedule.semester)
  const defaultTerm = availableTerms.value.find((term) => term.is_default)
  if (availableTerms.value.some((term) => termKey(term.year, term.semester) === current)) selectedAvailableTermKey.value = current
  else if (!selectedAvailableTermKey.value && defaultTerm) selectedAvailableTermKey.value = termKey(defaultTerm.year, defaultTerm.semester)
}

const loadTerms = async () => {
  const payload = unwrap(await scheduleApi.terms())
  applyTerms(payload)
}

const termKey = (termYear: number, semester: Semester) => `${termYear}-${semester}`

const termOptionLabel = (term: TermSummary) =>
  `${term.term_year} 年${term.semester === 'fall' ? '秋季' : '春季'}学期 (${term.course_count} 门)`
const availableTermLabel = (term: AvailableTerm) => `${term.display_name}${term.is_default ? '（默认）' : ''} · 第一周 ${term.first_week_start}`

const confirmTermSwitch = () => !dirty.value || window.confirm('当前课表还有未保存的修改，确定切换吗？')

const openTerm = async () => {
  const selected = availableTerms.value.find((term) => termKey(term.year, term.semester) === selectedAvailableTermKey.value)
  if (!selected) {
    ElMessage.warning('请选择管理员开放的教学学期')
    return
  }
  const termYear = selected.year
  const semester = selected.semester
  const currentKey = termKey(schedule.term_year, schedule.semester)
  const targetKey = termKey(termYear, semester)
  if (targetKey === currentKey) {
    selectedTermKey.value = currentKey
    ElMessage.info(`当前已打开 ${formatTermLabel(termYear, semester)} 课表`)
    return
  }
  if (!confirmTermSwitch()) {
    selectedTermKey.value = currentKey
    return
  }
  termSwitching.value = true
  try {
    const payload = unwrap(
      await scheduleApi.activate({
        term_year: termYear,
        semester,
      }),
    )
    applyPayload(payload)
    await loadTerms()
    dirty.value = false
    ElMessage.success(`已打开 ${formatTermLabel(termYear, semester)} 课表`)
  } catch (error: any) {
    selectedTermKey.value = currentKey
    ElMessage.error(error?.msg || '打开课表失败')
  } finally {
    termSwitching.value = false
  }
}

const selectSavedTerm = (value: string) => {
  const term = terms.value.find((item) => termKey(item.term_year, item.semester) === value)
  if (!term) return
  if (!confirmTermSwitch()) return
  termSwitching.value = true
  void scheduleApi.me({ term_year: term.term_year, semester: term.semester })
    .then((result) => {
      applyPayload(unwrap(result))
      dirty.value = false
    })
    .catch((error: any) => {
      selectedTermKey.value = termKey(schedule.term_year, schedule.semester)
      ElMessage.error(error?.msg || '读取已保存课表失败')
    })
    .finally(() => { termSwitching.value = false })
}

const selectAvailableTerm = (value: string) => {
  const term = availableTerms.value.find((item) => termKey(item.year, item.semester) === value)
  if (!term) return
  termDraft.term_year = term.year
  termDraft.semester = term.semester
}

const saveSchedule = async () => {
  saving.value = true
  try {
    const payload = {
      term_year: schedule.term_year,
      semester: schedule.semester,
      first_week_start: schedule.first_week_start,
      settings: schedule.settings,
      courses: schedule.courses,
      metadata: schedule.metadata,
    }
    const res = unwrap(await scheduleApi.save(payload))
    applyPayload(res)
    await loadTerms()
    dirty.value = false
    ElMessage.success('课表已保存')
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存课表失败')
  } finally {
    saving.value = false
  }
}

const chooseImport = () => importInput.value?.click()

const handleImport = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  try {
    const res = unwrap(
      await scheduleApi.import(
        file,
        {
          term_year: schedule.term_year,
          semester: schedule.semester,
        },
        replaceImport.value,
      ),
    )
    applyPayload(res.schedule)
    await loadTerms()
    dirty.value = false
    ElMessage.success(`已导入 ${res.imported || 0} 条课程`)
    if (res.warnings?.length) {
      ElMessage.warning(res.warnings.slice(0, 3).join('；'))
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '导入失败')
  } finally {
    input.value = ''
    importing.value = false
  }
}

const coursesAt = (weekday: number, period: number) =>
  coursesForSelectedWeek.value.filter((course) => course.weekday === weekday && course.start_period === period)

const coursesForDay = (weekday: number) =>
  coursesForSelectedWeek.value
    .filter((course) => course.weekday === weekday)
    .sort((left, right) => left.start_period - right.start_period)

const periodLabel = (period: number) => schedule.settings.period_labels?.[period - 1] || `${period}`

const openCourseDialog = (course?: Course) => {
  if (termReadOnly.value) {
    ElMessage.info('当前为已关闭或历史学期，只能查看课表')
    return
  }
  editingCourseId.value = course?.id || ''
  courseForm.name = course?.name || ''
  courseForm.teacher = course?.teacher || ''
  courseForm.location = course?.location || ''
  courseForm.weekday = course?.weekday || 1
  courseForm.start_period = course?.start_period || 1
  courseForm.end_period = course?.end_period || Math.min(2, Number(schedule.settings.periods_per_day || 12))
  courseForm.weeks_text = weeksToText(course?.weeks || [selectedWeek.value])
  courseForm.color = course?.color || '#2563eb'
  courseForm.note = course?.note || ''
  courseDrawer.value = true
}

const saveCourse = () => {
  const name = courseForm.name.trim()
  if (!name) {
    ElMessage.warning('请填写课程名称')
    return
  }
  if (courseForm.end_period < courseForm.start_period) {
    ElMessage.warning('结束节不能小于开始节')
    return
  }
  const next: Course = {
    id: editingCourseId.value || String(Date.now()),
    name,
    teacher: courseForm.teacher.trim(),
    location: courseForm.location.trim(),
    weekday: Number(courseForm.weekday),
    start_period: Number(courseForm.start_period),
    end_period: Number(courseForm.end_period),
    weeks: parseWeeksText(courseForm.weeks_text),
    color: courseForm.color,
    note: courseForm.note.trim(),
  }
  const index = schedule.courses.findIndex((course) => course.id === editingCourseId.value)
  if (index >= 0) {
    schedule.courses.splice(index, 1, next)
  } else {
    schedule.courses.push(next)
  }
  markDirty()
  courseDrawer.value = false
}

const deleteCourse = async () => {
  try {
    await ElMessageBox.confirm('确定删除这门课程吗？', '删除课程', { type: 'warning' })
  } catch {
    return
  }
  const index = schedule.courses.findIndex((course) => course.id === editingCourseId.value)
  if (index >= 0) {
    schedule.courses.splice(index, 1)
    markDirty()
  }
  courseDrawer.value = false
}

const openJsonEditor = () => {
  rawJson.value = JSON.stringify(
    {
      settings: schedule.settings,
      courses: schedule.courses,
      metadata: schedule.metadata,
    },
    null,
    2,
  )
  jsonDrawer.value = true
}

const applyJson = async () => {
  try {
    const parsed = JSON.parse(rawJson.value)
    schedule.settings = {
      periods_per_day: Number(parsed.settings?.periods_per_day || 12),
      show_weekend: parsed.settings?.show_weekend !== false,
      period_labels: [...(parsed.settings?.period_labels || [])],
    }
    schedule.courses = [...(parsed.courses || [])]
    schedule.metadata = { ...(parsed.metadata || {}) }
    markDirty()
    jsonDrawer.value = false
    await saveSchedule()
  } catch (error: any) {
    ElMessage.error(error?.message || 'JSON 格式不正确')
  }
}

const prevWeek = () => {
  if (selectedWeek.value > 1) {
    selectedWeek.value -= 1
    syncCalendarToSelectedWeek()
  }
}
const nextWeek = () => {
  selectedWeek.value += 1
  syncCalendarToSelectedWeek()
}
const goCurrentWeek = () => {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const currentWeek = weekNumberForDate(today)
  week.today = formatDate(today)
  calendarDate.value = today
  if (currentWeek > 0) {
    selectedWeek.value = currentWeek
    return
  }
  ElMessage.info('今天在当前学期开始前，日历已定位到今天')
}

const markDirty = () => {
  dirty.value = true
}

const weekDate = (weekNumber: number, offset: number) => {
  const first = parseScheduleDate(schedule.first_week_start)
  if (!first) return ''
  first.setDate(first.getDate() + (weekNumber - 1) * 7 + offset)
  return formatDate(first)
}

const syncCalendarToSelectedWeek = () => {
  const date = parseScheduleDate(weekDate(selectedWeek.value, 0))
  if (date) calendarDate.value = date
}

const calendarDay = (value: string) => value.slice(-2).replace(/^0/, '')

const calendarCourses = (value: string) => {
  const date = parseScheduleDate(value)
  const first = parseScheduleDate(schedule.first_week_start)
  if (!date || !first) return [] as Course[]
  const diff = Math.round((date.getTime() - first.getTime()) / 86400000)
  if (diff < 0) return [] as Course[]
  const weekNumber = Math.floor(diff / 7) + 1
  const weekday = date.getDay() === 0 ? 7 : date.getDay()
  return schedule.courses.filter((course) => {
    const weeks = course.weeks || []
    return course.weekday === weekday && (weeks.length === 0 || weeks.includes(weekNumber))
  })
}

const parseScheduleDate = (value?: string) => {
  if (!value) return null
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  const date = new Date(year, month - 1, day)
  return Number.isNaN(date.getTime()) ? null : date
}

const weekNumberForDate = (date: Date) => {
  const first = parseScheduleDate(schedule.first_week_start)
  if (!first) return 0
  const target = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  const diff = Math.round((target.getTime() - first.getTime()) / 86400000)
  if (diff < 0) return 0
  return Math.floor(diff / 7) + 1
}

const parseWeeksText = (value: string) => {
  const weeks = new Set<number>()
  value.split(/[,，、\s]+/).forEach((part) => {
    if (!part) return
    if (part.includes('-')) {
      const [startText, endText] = part.split('-', 2)
      const start = Number(startText)
      const end = Number(endText)
      if (Number.isFinite(start) && Number.isFinite(end)) {
        for (let weekNumber = start; weekNumber <= end; weekNumber += 1) {
          if (weekNumber > 0 && weekNumber <= 60) weeks.add(weekNumber)
        }
      }
      return
    }
    const weekNumber = Number(part)
    if (Number.isFinite(weekNumber) && weekNumber > 0 && weekNumber <= 60) weeks.add(weekNumber)
  })
  return Array.from(weeks).sort((a, b) => a - b)
}

const weeksToText = (weeks: number[]) => {
  if (!weeks.length) return ''
  const sorted = [...weeks].sort((a, b) => a - b)
  const ranges: string[] = []
  let start = sorted[0]
  let end = sorted[0]
  for (let i = 1; i < sorted.length; i += 1) {
    if (sorted[i] === end + 1) {
      end = sorted[i]
      continue
    }
    ranges.push(start === end ? String(start) : `${start}-${end}`)
    start = sorted[i]
    end = sorted[i]
  }
  ranges.push(start === end ? String(start) : `${start}-${end}`)
  return ranges.join(',')
}

const formatDate = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const softColor = (color?: string) => {
  const value = color && /^#[0-9a-fA-F]{6}$/.test(color) ? color : '#2563eb'
  const red = parseInt(value.slice(1, 3), 16)
  const green = parseInt(value.slice(3, 5), 16)
  const blue = parseInt(value.slice(5, 7), 16)
  return `rgba(${red}, ${green}, ${blue}, 0.12)`
}

const handleBeforeUnload = (event: BeforeUnloadEvent) => {
  if (!dirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

watch(calendarDate, (value) => {
  const weekNumber = weekNumberForDate(value)
  if (weekNumber > 0) selectedWeek.value = weekNumber
})

onBeforeRouteLeave(() => {
  if (!dirty.value) return true
  return window.confirm('课表还有未保存的修改，确定要离开吗？')
})

onMounted(() => {
  void loadSchedule()
  window.addEventListener('beforeunload', handleBeforeUnload)
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload)
})
</script>

<style scoped>
.schedule-view {
  display: grid;
  gap: 16px;
  container-type: inline-size;
}
.schedule-card {
  border-radius: 8px;
}
.schedule-header,
.header-actions,
.toolbar,
.drawer-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.schedule-header {
  justify-content: space-between;
}
.schedule-header h2 {
  margin: 0 0 6px;
}
.schedule-meta {
  display: flex;
  gap: 12px;
  color: #606266;
  font-size: 13px;
}
.toolbar {
  margin-bottom: 14px;
}
.term-select {
  width: min(240px, 100%);
}
.schedule-tabs :deep(.el-tabs__header) {
  margin-bottom: 14px;
}
.hidden-input {
  display: none;
}
.schedule-grid {
  display: grid;
  grid-template-columns: 74px repeat(var(--day-count), minmax(116px, 1fr));
  border: 1px solid #ebeef5;
  border-radius: 8px;
  overflow: auto;
}
.schedule-agenda,
.calendar-agenda {
  display: grid;
  gap: 10px;
}
.agenda-day,
.calendar-agenda-day {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #fff;
}
.agenda-day > header,
.calendar-agenda-day > header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: #303133;
}
.agenda-day > header span,
.calendar-agenda-day > header span,
.agenda-empty {
  color: #606266;
  font-size: 13px;
}
.agenda-course-list {
  display: grid;
  gap: 8px;
}
.agenda-course {
  display: grid;
  width: 100%;
  gap: 3px;
  padding: 10px 12px;
  border: 1px solid transparent;
  border-left: 4px solid;
  border-radius: 6px;
  color: #303133;
  cursor: pointer;
  text-align: left;
}
.agenda-course strong {
  font-size: 14px;
}
.agenda-course span {
  color: #606266;
  font-size: 13px;
  line-height: 1.45;
}
.calendar-date-picker {
  width: 100%;
}
.grid-corner,
.grid-day,
.grid-period,
.grid-cell {
  min-height: 58px;
  border-right: 1px solid #ebeef5;
  border-bottom: 1px solid #ebeef5;
}
.grid-corner,
.grid-day,
.grid-period {
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f7f8fa;
  font-weight: 600;
  color: #303133;
}
.grid-cell {
  padding: 6px;
  background: #fff;
}
.course-chip {
  width: 100%;
  min-height: 46px;
  display: grid;
  gap: 2px;
  padding: 7px 8px;
  border: 1px solid;
  border-radius: 8px;
  color: #303133;
  text-align: left;
  cursor: pointer;
}
.course-chip + .course-chip {
  margin-top: 6px;
}
.course-chip strong {
  font-size: 13px;
}
.course-chip span {
  font-size: 12px;
  color: #606266;
}
.calendar-scroll {
  overflow-x: auto;
}
.schedule-calendar {
  min-width: 680px;
}
.schedule-calendar :deep(.el-calendar-day) {
  height: 118px;
  padding: 5px;
}
.calendar-cell {
  display: grid;
  align-content: start;
  gap: 5px;
  min-height: 100%;
}
.calendar-cell-selected .calendar-day-number {
  color: #2563eb;
}
.calendar-day-number {
  color: #303133;
  font-size: 13px;
  font-weight: 600;
}
.calendar-courses {
  display: grid;
  gap: 3px;
}
.calendar-course,
.calendar-overflow {
  display: block;
  min-width: 0;
  overflow: hidden;
  padding: 2px 4px;
  border-left: 3px solid;
  border-radius: 3px;
  color: #303133;
  font-size: 11px;
  line-height: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.calendar-overflow {
  border-left-color: #909399;
  background: #f4f4f5;
  color: #606266;
}
.field-full {
  width: 100%;
}
.drawer-actions {
  justify-content: space-between;
  margin-top: 14px;
}
.json-editor {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
@media (max-width: 760px), (max-height: 540px) and (max-width: 1000px) {
  .schedule-header {
    align-items: flex-start;
    flex-direction: column;
  }
  .schedule-meta {
    gap: 6px 10px;
    flex-wrap: wrap;
  }
  .header-actions {
    width: 100%;
  }
  .header-actions .el-button {
    flex: 1 1 calc(50% - 8px);
    min-height: 44px;
  }
  .toolbar {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: stretch;
  }
  .toolbar > * {
    min-width: 0;
    width: 100%;
  }
  .toolbar :deep(.el-radio-group) {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .toolbar :deep(.el-radio-button__inner) {
    width: 100%;
  }
  .schedule-view[data-layout-mode^='compact'] :deep(.el-input-number),
  .schedule-view[data-layout-mode^='compact'] :deep(.el-input-number .el-input__wrapper) {
    min-height: var(--campus-touch-target, 44px);
  }
  .schedule-view[data-layout-mode^='compact'] :deep(.el-input-number__decrease),
  .schedule-view[data-layout-mode^='compact'] :deep(.el-input-number__increase) {
    top: 0;
    bottom: 0;
    width: var(--campus-touch-target, 44px);
    height: var(--campus-touch-target, 44px);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .schedule-view[data-layout-mode^='compact'] :deep(.el-input-number .el-input__wrapper) {
    padding-right: calc(var(--campus-touch-target, 44px) + 4px);
    padding-left: calc(var(--campus-touch-target, 44px) + 4px);
  }
  .schedule-tabs :deep(.el-tabs__nav) {
    width: 100%;
  }
  .schedule-tabs :deep(.el-tabs__item) {
    width: 50%;
    text-align: center;
  }
}
@media (max-width: 420px) {
  .toolbar {
    grid-template-columns: 1fr;
  }
  .header-actions .el-button {
    flex-basis: 100%;
  }
}
</style>
