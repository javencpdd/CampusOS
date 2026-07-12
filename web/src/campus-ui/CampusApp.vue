<template>
  <ThemeRoot>
    <StyleEffectLayer
      :script="themeStore.activePackage?.effect_js"
      :capabilities="themeStore.effectiveCapabilities"
      :resolve-query="resolveThemeQuery"
    />
    <AppShell :health="runtimeStore.degradedPlugins.length ? 'degraded' : 'healthy'">
      <template #header>
        <div class="header-content">
          <router-link to="/" class="brand logo"><h2>CampusOS</h2></router-link>
          <div class="header-actions">
            <router-link to="/threads"><el-button text>帖子</el-button></router-link>
            <router-link v-for="item in headerNavigation" :key="`${item.plugin}:${item.id}`" :to="item.path"
              ><el-button text>{{ item.label }}</el-button></router-link
            >
            <router-link v-if="userStore.isLoggedIn" to="/threads/create"
              ><el-button type="primary" size="small">发帖</el-button></router-link
            >
            <router-link v-if="userStore.isLoggedIn" :to="publicSpacePath"
              ><el-button text>个人主页</el-button></router-link
            >
            <el-dropdown v-if="userStore.isLoggedIn" trigger="click">
              <el-avatar class="nav-avatar" :size="32" :src="displayAvatar" role="button" tabindex="0">{{
                avatarInitial
              }}</el-avatar>
              <template #dropdown
                ><el-dropdown-menu>
                  <el-dropdown-item
                    v-for="item in userNavigation"
                    :key="`${item.plugin}:${item.id}`"
                    @click="router.push(item.path)"
                    >{{ item.label }}</el-dropdown-item
                  >
                  <el-dropdown-item @click="goPublicSpace">查看主页</el-dropdown-item>
                  <el-dropdown-item divided @click="handleLogout">退出登录</el-dropdown-item>
                </el-dropdown-menu></template
              >
            </el-dropdown>
            <template v-else>
              <router-link to="/login"><el-button text>登录</el-button></router-link>
              <router-link to="/register"><el-button type="primary" size="small">注册</el-button></router-link>
            </template>
          </div>
        </div>
      </template>
      <template #primary-navigation>
        <div class="primary-nav">
          <router-link to="/">首页</router-link><router-link to="/threads">全部帖子</router-link>
        </div>
      </template>
      <template v-if="slotSurfaces('hero').length" #hero
        ><DeclarativeRenderer
          v-for="surface in slotSurfaces('hero')"
          :key="surface.id"
          :node="surface.schema!"
          :plugin="surface.plugin"
      /></template>
      <template v-if="slotSurfaces('left-sidebar').length" #left-sidebar
        ><DeclarativeRenderer
          v-for="surface in slotSurfaces('left-sidebar')"
          :key="surface.id"
          :node="surface.schema!"
          :plugin="surface.plugin"
      /></template>
      <template #page-outlet><router-view /></template>
      <template v-if="slotSurfaces('right-sidebar').length" #right-sidebar
        ><DeclarativeRenderer
          v-for="surface in slotSurfaces('right-sidebar')"
          :key="surface.id"
          :node="surface.schema!"
          :plugin="surface.plugin"
      /></template>
      <template v-if="slotSurfaces('floating-action').length" #floating-action
        ><DeclarativeRenderer
          v-for="surface in slotSurfaces('floating-action')"
          :key="surface.id"
          :node="surface.schema!"
          :plugin="surface.plugin"
      /></template>
      <template #footer><p>CampusOS · 校园社区引擎</p></template>
      <template #safety>
        <el-alert v-if="runtimeStore.error" :title="runtimeStore.error" type="error" show-icon :closable="false" />
        <el-alert
          v-else-if="runtimeStore.degradedPlugins.length"
          title="部分插件服务暂时不可用，界面与已有内容仍可查看"
          type="warning"
          show-icon
          :closable="false"
        />
      </template>
    </AppShell>
  </ThemeRoot>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { categoryApi, scheduleApi, spaceApi, threadApi } from '@/api'
import StyleEffectLayer from '@/components/StyleEffectLayer.vue'
import { useUserStore } from '@/stores/user'
import { useWebThemeStore } from '@/stores/webTheme'
import { useUIRuntimeStore } from '@/stores/uiRuntime'
import AppShell from './AppShell.vue'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import ThemeRoot from './ThemeRoot.vue'

const router = useRouter()
const userStore = useUserStore()
const themeStore = useWebThemeStore()
const runtimeStore = useUIRuntimeStore()
const publicSpacePath = computed(() => (userStore.user?.username ? `/u/${userStore.user.username}` : '/space/settings'))
const displayAvatar = computed(() => userStore.user?.avatar || '')
const avatarInitial = computed(() =>
  (userStore.user?.nickname || userStore.user?.username || 'U').slice(0, 1).toUpperCase(),
)
const visibleNavigation = computed(() =>
  runtimeStore.navigation.filter((item) => !item.requiresAuth || userStore.isLoggedIn),
)
const headerNavigation = computed(() =>
  visibleNavigation.value.filter((item) => !item.location || item.location === 'header'),
)
const userNavigation = computed(() => visibleNavigation.value.filter((item) => item.location === 'user-menu'))
const slotSurfaces = (name: string) =>
  (runtimeStore.slots.get(name) || []).filter((surface) => surface.renderer === 'schema' && surface.schema)

const syncSpaceAvatar = async () => {
  if (!userStore.isLoggedIn) return
  try {
    const res: any = await spaceApi.me()
    const avatar = res?.data?.owner?.avatar || res?.data?.space?.avatar
    if (avatar) userStore.setAvatar(avatar)
  } catch {
    /* non-critical header data */
  }
}
const goPublicSpace = () => router.push(publicSpacePath.value)
const handleLogout = () => {
  userStore.logout()
  router.push('/login')
}

const resolveThemeQuery = async (method: string, params: Record<string, unknown>) => {
  if (method === 'community.threads.read') {
    const requested = Number(params.limit || 10)
    const limit = Math.max(1, Math.min(20, Number.isFinite(requested) ? requested : 10))
    const response: any = await threadApi.list({ page: 1, page_size: limit })
    return {
      items: response?.data?.items || [],
      total: response?.data?.pagination?.total || 0,
    }
  }
  if (method === 'categories.read') {
    const response: any = await categoryApi.list()
    return { items: response?.data?.items || response?.data || [] }
  }
  if (method === 'schedule.me.read') {
    if (!userStore.isLoggedIn) throw new Error('需要登录后才能读取个人课表')
    const response: any = await scheduleApi.me()
    return response?.data
  }
  throw new Error('不支持的系统风格包数据能力')
}

onMounted(() => {
  void syncSpaceAvatar()
  void runtimeStore.initialize()
})
watch(
  () => userStore.user?.id,
  (userID) => {
    void themeStore.initialize(userID)
  },
  { immediate: true },
)
watch(
  () => userStore.user?.id,
  () => void runtimeStore.sync(),
)
watch(
  () => userStore.isLoggedIn,
  (loggedIn) => {
    if (loggedIn) void syncSpaceAvatar()
  },
)
</script>

<style scoped>
.header-content {
  width: min(var(--campus-content-width, 1200px), 100%);
  margin: 0 auto;
  padding: 0 20px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  box-sizing: border-box;
}
.brand {
  color: var(--campus-brand-color, #174ea6);
  font-size: 21px;
  font-weight: 750;
  text-decoration: none;
  white-space: nowrap;
  letter-spacing: 0;
}
.brand h2 {
  margin: 0;
  font: inherit;
  letter-spacing: 0;
}
.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}
.nav-avatar {
  cursor: pointer;
  outline: none;
}
.nav-avatar:focus-visible {
  box-shadow: 0 0 0 2px var(--campus-brand-color, #174ea6);
}
.primary-nav {
  width: min(var(--campus-content-width, 1200px), 100%);
  margin: 0 auto;
  padding: 10px 20px;
  display: flex;
  gap: 20px;
  box-sizing: border-box;
}
.primary-nav a {
  color: var(--campus-muted-color, #4b5563);
  text-decoration: none;
  font-size: 14px;
}
p {
  margin: 0;
}
@media (max-width: 720px) {
  .header-content {
    padding: 10px 12px;
    align-items: flex-start;
  }
  .header-actions {
    gap: 4px;
  }
  .brand {
    font-size: 18px;
    padding-top: 6px;
  }
}
</style>
