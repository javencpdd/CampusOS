<template>
  <el-container class="app-container">
    <el-header class="app-header">
      <div class="header-content">
        <router-link to="/" class="logo">
          <h2>🚀 CampusOS</h2>
        </router-link>
        <nav class="nav-links">
          <router-link to="/threads">
            <el-button text>帖子列表</el-button>
          </router-link>
          <router-link to="/threads/create" v-if="userStore.isLoggedIn">
            <el-button type="primary" size="small">发帖</el-button>
          </router-link>
          <router-link :to="publicSpacePath" v-if="userStore.isLoggedIn">
            <el-button text>个人主页</el-button>
          </router-link>
          <template v-if="userStore.isLoggedIn">
            <el-dropdown trigger="click">
              <el-avatar
                class="nav-avatar"
                :size="32"
                :src="displayAvatar"
                role="button"
                tabindex="0"
              >
                {{ avatarInitial }}
              </el-avatar>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="goSpaceSettings">主页设置</el-dropdown-item>
                  <el-dropdown-item @click="goSchedule">个人课表</el-dropdown-item>
                  <el-dropdown-item @click="goPublicSpace">查看主页</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-dropdown>
              <span class="user-info">
                {{ userStore.user?.nickname || userStore.user?.username }}
                <el-icon><ArrowDown /></el-icon>
              </span>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="goSpaceSettings">主页设置</el-dropdown-item>
                  <el-dropdown-item @click="goSchedule">个人课表</el-dropdown-item>
                  <el-dropdown-item @click="goPublicSpace">查看主页</el-dropdown-item>
                  <el-dropdown-item @click="handleLogout">退出登录</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
          <template v-else>
            <router-link to="/login">
              <el-button text>登录</el-button>
            </router-link>
            <router-link to="/register">
              <el-button type="primary" size="small">注册</el-button>
            </router-link>
          </template>
        </nav>
      </div>
    </el-header>
    <el-main class="app-main">
      <router-view />
    </el-main>
    <el-footer class="app-footer">
      <p>© 2024 CampusOS - 下一代校园社区引擎</p>
    </el-footer>
  </el-container>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { ArrowDown } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { spaceApi } from '@/api'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const userStore = useUserStore()

const publicSpacePath = computed(() => {
  const username = userStore.user?.username
  return username ? `/u/${username}` : '/space/settings'
})
const displayAvatar = computed(() => userStore.user?.avatar || '')
const avatarInitial = computed(() => {
  const name = userStore.user?.nickname || userStore.user?.username || 'U'
  return name.slice(0, 1).toUpperCase()
})

const syncSpaceAvatar = async () => {
  if (!userStore.isLoggedIn) return
  try {
    const res: any = await spaceApi.me()
    const avatar = res?.data?.owner?.avatar || res?.data?.space?.avatar
    if (avatar) {
      userStore.setAvatar(avatar)
    }
  } catch {
    // Header avatar is non-critical; settings page reports detailed errors.
  }
}

const goSpaceSettings = () => {
  router.push('/space/settings')
}

const goPublicSpace = () => {
  const username = userStore.user?.username
  router.push(username ? `/u/${username}` : '/space/settings')
}

const goSchedule = () => {
  router.push('/schedule')
}

const handleLogout = () => {
  userStore.logout()
  router.push('/login')
}

onMounted(syncSpaceAvatar)
watch(() => userStore.isLoggedIn, (loggedIn) => {
  if (loggedIn) {
    void syncSpaceAvatar()
  }
})
</script>

<style scoped>
.app-container {
  min-height: 100vh;
}
.app-header {
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  display: flex;
  align-items: center;
}
.header-content {
  width: 1200px;
  max-width: 100%;
  margin: 0 auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.logo {
  text-decoration: none;
  color: #303133;
}
.logo h2 {
  margin: 0;
}
.nav-links {
  display: flex;
  align-items: center;
  gap: 16px;
}
.nav-avatar {
  flex: 0 0 auto;
  cursor: pointer;
  outline: none;
}
.nav-avatar:focus-visible {
  box-shadow: 0 0 0 2px #409eff;
}
.user-info {
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
}
.app-main {
  max-width: 1200px;
  margin: 0 auto;
  width: 100%;
}
.app-footer {
  text-align: center;
  color: #909399;
  font-size: 14px;
}
</style>
