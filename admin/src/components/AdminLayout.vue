<template>
  <div class="admin-layout">
    <el-container style="height: 100vh">
      <div v-if="isMobile && mobileNavOpen" class="nav-scrim" @click="mobileNavOpen = false" />
      <el-aside width="220px" class="admin-aside" :class="{ 'is-open': mobileNavOpen }">
        <div class="admin-logo">
          <h2>🔧 管理后台</h2>
        </div>
        <el-menu
          :default-active="activeMenu"
          router
          background-color="#304156"
          text-color="#bfcbd9"
          active-text-color="#409eff"
		  @select="closeMobileNav"
        >
          <el-menu-item index="/">
            <el-icon><DataAnalysis /></el-icon>
            <span>仪表盘</span>
          </el-menu-item>
          <el-menu-item index="/users">
            <el-icon><User /></el-icon>
            <span>用户管理</span>
          </el-menu-item>
          <el-menu-item v-if="adminStore.isAdmin" index="/moderators">
            <el-icon><Lock /></el-icon>
            <span>版主管理</span>
          </el-menu-item>
          <el-menu-item index="/threads">
            <el-icon><Document /></el-icon>
            <span>帖子管理</span>
          </el-menu-item>
          <el-menu-item index="/categories">
            <el-icon><FolderOpened /></el-icon>
            <span>版块管理</span>
          </el-menu-item>
          <el-menu-item index="/docs">
            <el-icon><Reading /></el-icon>
            <span>相关资料</span>
          </el-menu-item>
          <el-menu-item index="/architecture">
            <el-icon><DataAnalysis /></el-icon>
            <span>数据架构</span>
          </el-menu-item>
		  <el-menu-item index="/plugins">
			<el-icon><Connection /></el-icon>
			<span>外部插件</span>
		  </el-menu-item>
		  <el-menu-item index="/plugin-center">
			<el-icon><Grid /></el-icon>
			<span>插件中心</span>
		  </el-menu-item>
		  <el-menu-item index="/features">
			<el-icon><SetUp /></el-icon>
			<span>内置功能</span>
          </el-menu-item>
          <el-menu-item index="/integrations">
            <el-icon><Operation /></el-icon>
            <span>集成中心</span>
          </el-menu-item>
          <el-menu-item index="/reviews">
            <el-icon><Stamp /></el-icon>
            <span>帖子审核</span>
          </el-menu-item>
          <el-menu-item index="/events">
            <el-icon><Bell /></el-icon>
            <span>事件日志</span>
          </el-menu-item>
          <el-menu-item index="/platform-logs">
            <el-icon><Monitor /></el-icon>
            <span>平台日志</span>
          </el-menu-item>
        </el-menu>
      </el-aside>
      <el-container>
        <el-header class="admin-header">
          <div class="header-left">
			<el-button v-if="isMobile" text aria-label="打开导航" @click="mobileNavOpen = !mobileNavOpen">
			  <el-icon><Menu /></el-icon>
			</el-button>
            <el-breadcrumb separator="/">
              <el-breadcrumb-item :to="{ path: '/' }">管理后台</el-breadcrumb-item>
              <el-breadcrumb-item v-if="currentPageTitle">{{ currentPageTitle }}</el-breadcrumb-item>
            </el-breadcrumb>
          </div>
          <div class="header-right">
            <span class="user-info">
              <el-icon><UserFilled /></el-icon>
              {{ adminStore.user?.nickname || '管理员' }}
            </span>
            <el-button text @click="handleLogout">
              <el-icon><SwitchButton /></el-icon>
              退出
            </el-button>
          </div>
        </el-header>
        <el-main class="admin-main">
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAdminStore } from '@/modules/identity/store'
import {
  DataAnalysis,
  User,
  UserFilled,
  Document,
  FolderOpened,
	Connection,
	Grid,
  Operation,
  Bell,
  Monitor,
  Reading,
  Stamp,
  Lock,
	SetUp,
	Menu,
  SwitchButton,
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const adminStore = useAdminStore()
const isMobile = ref(false)
const mobileNavOpen = ref(false)
let mediaQuery: MediaQueryList | undefined
const updateViewport = () => {
  isMobile.value = Boolean(mediaQuery?.matches)
  if (!isMobile.value) mobileNavOpen.value = false
}
const closeMobileNav = () => { if (isMobile.value) mobileNavOpen.value = false }

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 800px)')
  updateViewport()
  mediaQuery.addEventListener('change', updateViewport)
})
onBeforeUnmount(() => mediaQuery?.removeEventListener('change', updateViewport))

const activeMenu = computed(() => route.path)

const currentPageTitle = computed(() => {
  if (route.meta?.title) return route.meta.title as string
  const titles: Record<string, string> = {
    '/': '仪表盘',
    '/users': '用户管理',
    '/moderators': '版主管理',
    '/threads': '帖子管理',
    '/categories': '版块管理',
    '/docs': '相关资料',
    '/architecture': '数据架构',
	'/plugins': '外部插件',
	'/plugin-center': '插件中心',
	'/features': '内置功能',
    '/integrations': '集成中心',
    '/reviews': '帖子审核',
    '/events': '事件日志',
    '/platform-logs': '平台日志',
  }
  return titles[route.path] || ''
})

const handleLogout = () => {
  adminStore.logout()
  router.push('/login')
}
</script>

<style scoped>
.admin-aside {
  background-color: #304156;
  overflow-y: auto;
}

.nav-scrim { display: none; }

.admin-aside::-webkit-scrollbar {
  width: 0;
}

.admin-logo {
  padding: 16px;
  text-align: center;
  color: #fff;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}

.admin-logo h2 {
  margin: 0;
  font-size: 18px;
}

.admin-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #ebeef5;
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 21, 41, 0.08);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #606266;
  font-size: 14px;
}

.admin-main {
  background: #f5f7fa;
  min-height: calc(100vh - 60px);
  padding: 20px;
}
@media (max-width: 800px) {
  .admin-aside { position: fixed; inset: 0 auto 0 0; z-index: 1002; height: 100vh; transform: translateX(-100%); transition: transform 160ms ease; box-shadow: 3px 0 14px rgba(0, 21, 41, 0.2); }
  .admin-aside.is-open { transform: translateX(0); }
  .nav-scrim { display: block; position: fixed; inset: 0; z-index: 1001; background: rgba(20, 30, 44, 0.36); }
  .admin-header { padding: 0 12px; }
  .admin-main { padding: 12px; }
  .header-right { gap: 4px; }
  .user-info { max-width: 130px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
}
</style>
