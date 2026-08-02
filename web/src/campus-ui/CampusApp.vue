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
            <template v-if="!isCompact">
              <router-link to="/threads"><el-button text>帖子</el-button></router-link>
              <router-link v-for="item in headerNavigation" :key="`${item.plugin}:${item.id}`" :to="item.path"
                ><el-button text>{{ item.label }}</el-button></router-link
              >
            </template>
            <router-link v-if="userStore.isLoggedIn" to="/threads/create"
              ><el-button
                type="primary"
                size="small"
                :circle="isCompact"
                :aria-label="isCompact ? '发布帖子' : undefined"
              >
                <el-icon v-if="isCompact"><EditPen /></el-icon>
                <span v-else>发帖</span>
              </el-button></router-link
            >
            <router-link v-if="userStore.isLoggedIn && !isCompact" :to="publicSpacePath"
              ><el-button text>个人主页</el-button></router-link
            >
            <NotificationCenter v-if="userStore.isLoggedIn" />
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
                  <el-dropdown-item @click="router.push('/account/security')">账号安全</el-dropdown-item>
                  <el-dropdown-item @click="router.push('/plugins')">插件中心</el-dropdown-item>
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
        <div v-if="!isCompact" class="primary-nav" aria-label="站点主导航">
          <router-link to="/">首页</router-link>
          <router-link to="/threads">全部帖子</router-link>
          <template v-for="node in categoryNavigation" :key="node.key">
            <el-dropdown v-if="node.kind === 'group'" trigger="click" @command="openCategoryBoard">
              <button type="button" class="category-nav-group" :aria-label="`打开${node.name}下的板块`">
                <span>{{ node.name }}</span>
                <el-icon aria-hidden="true"><ArrowDown /></el-icon>
              </button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item :command="node.id">{{ node.name }} · 全部帖子</el-dropdown-item>
                  <el-dropdown-item v-for="board in node.children" :key="board.key" :command="board.id">
                    {{ board.name }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <router-link v-else :to="{ path: '/threads', query: { category_id: node.id } }">{{
              node.name
            }}</router-link>
          </template>
        </div>
        <div v-else class="mobile-primary-nav">
          <strong>{{ mobileNavigationLabel }}</strong>
          <el-button text :icon="Menu" aria-label="打开主导航" @click="mobileNavigationOpen = true"> 导航 </el-button>
        </div>
        <el-drawer
          v-model="mobileNavigationOpen"
          class="mobile-navigation-drawer"
          title="站点导航"
          direction="ltr"
          size="min(88vw, 340px)"
          append-to-body
        >
          <nav class="mobile-navigation-list" aria-label="移动端站点导航">
            <router-link class="mobile-navigation-link" to="/" @click="closeMobileNavigation">首页</router-link>
            <router-link class="mobile-navigation-link" to="/threads" @click="closeMobileNavigation"
              >全部帖子</router-link
            >
            <template v-for="node in categoryNavigation" :key="`mobile:${node.key}`">
              <section v-if="node.kind === 'group'" class="mobile-navigation-group">
                <h3>
                  <router-link
                    :to="{ path: '/threads', query: { category_id: node.id } }"
                    @click="closeMobileNavigation"
                  >
                    {{ node.name }} · 全部帖子
                  </router-link>
                </h3>
                <router-link
                  v-for="board in node.children"
                  :key="`mobile:${board.key}`"
                  class="mobile-navigation-link is-child"
                  :to="{ path: '/threads', query: { category_id: board.id } }"
                  @click="closeMobileNavigation"
                >
                  {{ board.name }}
                </router-link>
              </section>
              <router-link
                v-else
                class="mobile-navigation-link"
                :to="{ path: '/threads', query: { category_id: node.id } }"
                @click="closeMobileNavigation"
              >
                {{ node.name }}
              </router-link>
            </template>
            <section v-if="headerNavigation.length" class="mobile-navigation-group">
              <h3>校园功能</h3>
              <router-link
                v-for="item in headerNavigation"
                :key="`mobile:${item.plugin}:${item.id}`"
                class="mobile-navigation-link is-child"
                :to="item.path"
                @click="closeMobileNavigation"
              >
                {{ item.label }}
              </router-link>
            </section>
            <section v-if="userStore.isLoggedIn" class="mobile-navigation-group">
              <h3>我的</h3>
              <router-link class="mobile-navigation-link is-child" to="/threads/create" @click="closeMobileNavigation"
                >发布帖子</router-link
              >
              <router-link class="mobile-navigation-link is-child" :to="publicSpacePath" @click="closeMobileNavigation"
                >个人主页</router-link
              >
            </section>
          </nav>
        </el-drawer>
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
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, EditPen, Menu } from '@element-plus/icons-vue'
import { categoryApi, threadApi } from '@/modules/community/api'
import { scheduleApi } from '@/modules/schedule/api'
import { spaceApi } from '@/modules/space/api'
import StyleEffectLayer from '@/components/StyleEffectLayer.vue'
import { useUserStore } from '@/modules/identity/store'
import { useWebThemeStore } from '@/modules/appearance/store'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import { useLayoutCapability } from '@/shared/layout/useLayoutCapability'
import NotificationCenter from '@/modules/notifications/components/NotificationCenter.vue'
import AppShell from './AppShell.vue'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import ThemeRoot from './ThemeRoot.vue'
import { buildCategoryNavigation, type CategoryNavigationNode, type PublicCategory } from './categoryNavigation'
import { preloadCoreViews } from '@/router/preload'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const themeStore = useWebThemeStore()
const runtimeStore = useUIRuntimeStore()
const { isCompact } = useLayoutCapability()
const mobileNavigationOpen = ref(false)
const categoryNavigation = ref<CategoryNavigationNode[]>([])
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
const mobileNavigationLabel = computed(() => {
  if (route.path === '/') return '首页'
  if (route.path === '/threads') {
    const selectedCategory = String(route.query.category_id || '')
    for (const node of categoryNavigation.value) {
      if (node.kind === 'board' && node.id === selectedCategory) return node.name
      if (node.kind === 'group') {
        const board = node.children.find((item) => item.id === selectedCategory)
        if (board) return board.name
      }
    }
    return '全部帖子'
  }
  return typeof route.meta.title === 'string' ? route.meta.title : '校园功能'
})
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
const closeMobileNavigation = () => {
  mobileNavigationOpen.value = false
}
const openCategoryBoard = (categoryID: string) => router.push({ path: '/threads', query: { category_id: categoryID } })
const handleLogout = () => {
  userStore.logout()
  router.push('/login')
}

const loadCategoryNavigation = async () => {
  try {
    const response: any = await categoryApi.tree()
    const nodes = (response?.data || []) as PublicCategory[]
    categoryNavigation.value = buildCategoryNavigation(nodes)
  } catch {
    categoryNavigation.value = []
  }
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
  void preloadCoreViews()
  void syncSpaceAvatar()
  void runtimeStore.initialize()
  void loadCategoryNavigation()
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
watch(
  () => route.fullPath,
  () => closeMobileNavigation(),
)
watch(isCompact, (compact) => {
  if (!compact) closeMobileNavigation()
})
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
.mobile-primary-nav {
  width: min(var(--campus-content-width, 1200px), 100%);
  min-height: 48px;
  margin: 0 auto;
  padding: 4px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--campus-muted-color, #4b5563);
  box-sizing: border-box;
}
.mobile-primary-nav strong {
  min-width: 0;
  overflow: hidden;
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mobile-navigation-list {
  display: grid;
  gap: 4px;
  padding-bottom: max(20px, env(safe-area-inset-bottom, 0));
}
.mobile-navigation-link {
  min-height: 44px;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  border-radius: 6px;
  color: var(--campus-text-color, #1f2937);
  text-decoration: none;
}
.mobile-navigation-link:hover,
.mobile-navigation-link:focus-visible,
.mobile-navigation-link.router-link-active {
  color: var(--campus-brand-color, #174ea6);
  background: var(--campus-surface-background, #f3f6f9);
}
.mobile-navigation-link.is-child {
  padding-left: 24px;
}
.mobile-navigation-group {
  display: grid;
  gap: 2px;
  padding-top: 8px;
  border-top: 1px solid var(--campus-border-color, #dfe3e8);
}
.mobile-navigation-group h3 {
  margin: 0;
  padding: 8px 12px 4px;
  color: var(--campus-muted-color, #4b5563);
  font-size: 12px;
  font-weight: 700;
}
.mobile-navigation-group h3 a {
  color: inherit;
  text-decoration: none;
}
.mobile-navigation-group h3 a:hover,
.mobile-navigation-group h3 a:focus-visible {
  color: var(--campus-brand-color, #174ea6);
}
.category-nav-group {
  color: var(--campus-muted-color, #4b5563);
  border: 0;
  padding: 0;
  background: transparent;
  font: inherit;
  font-size: 14px;
  white-space: nowrap;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.category-nav-group:hover,
.category-nav-group:focus-visible {
  color: var(--campus-brand-color, #174ea6);
}
.category-nav-group:focus-visible {
  outline: 2px solid var(--campus-brand-color, #174ea6);
  outline-offset: 4px;
}
p {
  margin: 0;
}
@media (max-width: 760px), (max-height: 540px) and (max-width: 1000px) {
  .header-content {
    min-height: 64px;
    padding: 8px 12px;
    align-items: center;
    flex-wrap: nowrap;
  }
  .header-actions {
    gap: 4px;
    width: auto;
    margin-left: auto;
    justify-content: flex-end;
    flex-wrap: nowrap;
  }
  .brand {
    font-size: 18px;
  }
}
</style>
