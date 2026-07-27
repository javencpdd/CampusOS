<template>
  <div class="notification-center">
    <el-tooltip content="消息通知" placement="bottom">
      <el-badge :value="badgeValue" :hidden="unreadCount === 0" :max="99">
        <el-button text circle aria-label="消息通知" @click="openCenter">
          <el-icon><Bell /></el-icon>
        </el-button>
      </el-badge>
    </el-tooltip>

    <el-drawer
      v-model="visible"
      class="notification-drawer"
      title="消息通知"
      direction="rtl"
      size="min(92vw, 420px)"
      append-to-body
    >
      <div class="notification-toolbar">
        <span>{{ unreadCount ? `${unreadCount} 条未读` : '没有未读消息' }}</span>
        <el-button text :icon="Check" :disabled="unreadCount === 0" :loading="markingAll" @click="markAllRead">
          全部已读
        </el-button>
      </div>

      <div v-loading="loading" class="notification-list" aria-live="polite">
        <button
          v-for="item in items"
          :key="item.id"
          type="button"
          class="notification-item"
          :class="{ 'is-unread': !item.is_read }"
          @click="openNotification(item)"
        >
          <span class="notification-indicator" aria-hidden="true"></span>
          <span class="notification-body">
            <strong>{{ item.title }}</strong>
            <span>{{ item.content }}</span>
            <time :datetime="item.created_at">{{ formatTime(item.created_at) }}</time>
          </span>
          <el-icon v-if="item.action_url" aria-hidden="true"><ArrowRight /></el-icon>
        </button>
        <el-empty v-if="!loading && items.length === 0" description="暂无消息通知" :image-size="72" />
      </div>

      <el-button v-if="items.length < total" class="load-more" :loading="loadingMore" @click="loadMore">
        加载更多
      </el-button>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElNotification } from 'element-plus'
import { ArrowRight, Bell, Check } from '@element-plus/icons-vue'
import { notificationApi, type NotificationListData, type UserNotification } from '../api'

const router = useRouter()
const visible = ref(false)
const items = ref<UserNotification[]>([])
const unreadCount = ref(0)
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const loadingMore = ref(false)
const markingAll = ref(false)
const initialized = ref(false)
const knownIDs = new Set<string>()
let pollingTimer: number | undefined

const badgeValue = computed(() => (unreadCount.value > 99 ? '99+' : unreadCount.value))
const unwrap = (value: any) => (value?.data || value) as NotificationListData

const load = async (options: { append?: boolean; announce?: boolean } = {}) => {
  const append = Boolean(options.append)
  if (append) loadingMore.value = true
  else loading.value = true
  try {
    const nextPage = append ? page.value + 1 : 1
    const data = unwrap(await notificationApi.list({ page: nextPage, page_size: 20 }))
    const nextItems = data?.items || []
    if (options.announce && initialized.value) {
      const latest = nextItems.find((item) => !item.is_read && !knownIDs.has(item.id))
      if (latest) {
        ElNotification({
          title: latest.title,
          message: latest.content,
          type: 'warning',
          duration: 6000,
          onClick: () => void openNotification(latest),
        })
      }
    }
    nextItems.forEach((item) => knownIDs.add(item.id))
    items.value = append ? [...items.value, ...nextItems] : nextItems
    page.value = nextPage
    total.value = data?.pagination?.total || 0
    unreadCount.value = data?.unread_count || 0
    initialized.value = true
  } catch (error: any) {
    if (visible.value) ElMessage.error(error?.msg || '加载消息通知失败')
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

const openCenter = () => {
  visible.value = true
  void load()
}

const loadMore = () => void load({ append: true })

const openNotification = async (item: UserNotification) => {
  if (!item.is_read) {
    try {
      await notificationApi.markRead(item.id)
      item.is_read = true
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    } catch (error: any) {
      ElMessage.error(error?.msg || '更新通知状态失败')
      return
    }
  }
  if (item.action_url && /^\/(?!\/)/.test(item.action_url)) {
    visible.value = false
    await router.push(item.action_url)
  }
}

const markAllRead = async () => {
  markingAll.value = true
  try {
    await notificationApi.markAllRead()
    items.value.forEach((item) => {
      item.is_read = true
    })
    unreadCount.value = 0
    ElMessage.success('全部消息已标记为已读')
  } catch (error: any) {
    ElMessage.error(error?.msg || '批量更新通知状态失败')
  } finally {
    markingAll.value = false
  }
}

const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN')
const refreshWhenVisible = () => {
  if (document.visibilityState === 'visible') void load({ announce: true })
}

onMounted(() => {
  void load()
  pollingTimer = window.setInterval(refreshWhenVisible, 30_000)
  document.addEventListener('visibilitychange', refreshWhenVisible)
})

onBeforeUnmount(() => {
  if (pollingTimer !== undefined) window.clearInterval(pollingTimer)
  document.removeEventListener('visibilitychange', refreshWhenVisible)
})
</script>

<style scoped>
.notification-center {
  display: inline-flex;
  align-items: center;
}
.notification-toolbar {
  min-height: 44px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--campus-muted-color, #606266);
  border-bottom: 1px solid var(--campus-border-color, #dcdfe6);
}
.notification-list {
  min-height: 180px;
  display: grid;
  align-content: start;
}
.notification-item {
  width: 100%;
  min-height: 88px;
  padding: 14px 4px;
  display: grid;
  grid-template-columns: 8px minmax(0, 1fr) 20px;
  align-items: start;
  gap: 10px;
  color: var(--campus-text-color, #303133);
  text-align: left;
  border: 0;
  border-bottom: 1px solid var(--campus-border-color, #ebeef5);
  background: transparent;
  cursor: pointer;
}
.notification-item:hover,
.notification-item:focus-visible {
  background: var(--campus-surface-background, #f5f7fa);
}
.notification-item:focus-visible {
  outline: 2px solid var(--campus-brand-color, #409eff);
  outline-offset: -2px;
}
.notification-indicator {
  width: 7px;
  height: 7px;
  margin-top: 7px;
  border-radius: 50%;
  background: transparent;
}
.notification-item.is-unread .notification-indicator {
  background: var(--campus-brand-color, #409eff);
}
.notification-body {
  min-width: 0;
  display: grid;
  gap: 5px;
}
.notification-body strong,
.notification-body span {
  overflow-wrap: anywhere;
}
.notification-body span {
  color: var(--campus-muted-color, #606266);
  font-size: 13px;
  line-height: 1.55;
}
.notification-body time {
  color: var(--campus-muted-color, #909399);
  font-size: 12px;
}
.load-more {
  width: 100%;
  margin-top: 14px;
}
</style>
