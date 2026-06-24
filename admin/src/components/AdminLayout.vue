<template>
  <div class="admin-layout">
    <el-container style="height: 100vh">
      <el-aside width="220px" class="admin-aside">
        <div class="admin-logo">
          <h2>🔧 管理后台</h2>
        </div>
        <el-menu
          :default-active="activeMenu"
          router
          background-color="#304156"
          text-color="#bfcbd9"
          active-text-color="#409eff"
        >
          <el-menu-item index="/">
            <el-icon><DataAnalysis /></el-icon>
            <span>仪表盘</span>
          </el-menu-item>
          <el-menu-item index="/users">
            <el-icon><User /></el-icon>
            <span>用户管理</span>
          </el-menu-item>
          <el-menu-item index="/threads">
            <el-icon><Document /></el-icon>
            <span>帖子管理</span>
          </el-menu-item>
          <el-menu-item index="/categories">
            <el-icon><FolderOpened /></el-icon>
            <span>版块管理</span>
          </el-menu-item>
          <el-menu-item index="/plugins">
            <el-icon><Connection /></el-icon>
            <span>插件管理</span>
          </el-menu-item>
          <el-menu-item index="/events">
            <el-icon><Bell /></el-icon>
            <span>事件日志</span>
          </el-menu-item>
        </el-menu>
      </el-aside>
      <el-container>
        <el-header class="admin-header">
          <div class="header-left">
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
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAdminStore } from '@/stores/admin'
import {
  DataAnalysis,
  User,
  UserFilled,
  Document,
  FolderOpened,
  Connection,
  Bell,
  SwitchButton,
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const adminStore = useAdminStore()

const activeMenu = computed(() => route.path)

const currentPageTitle = computed(() => {
  if (route.meta?.title) return route.meta.title as string
  const titles: Record<string, string> = {
    '/': '仪表盘',
    '/users': '用户管理',
    '/threads': '帖子管理',
    '/categories': '版块管理',
    '/plugins': '插件管理',
    '/events': '事件日志',
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
</style>