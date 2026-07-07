<template>
  <div class="home">
    <section class="welcome-hero" :class="{ 'has-background': Boolean(homeConfig.background_image) }" :style="heroStyle">
      <div class="hero-content">
        <h1>{{ homeConfig.hero_title }}</h1>
        <p class="subtitle">{{ homeConfig.hero_subtitle }}</p>
        <div class="actions">
          <router-link to="/threads">
            <el-button type="primary" size="large">浏览帖子</el-button>
          </router-link>
          <router-link to="/register" v-if="!userStore.isLoggedIn">
            <el-button size="large">立即注册</el-button>
          </router-link>
        </div>
        <div v-if="homeConfig.show_category_tags && homeConfig.category_tags.length" class="category-tags">
          <router-link
            v-for="category in homeConfig.category_tags"
            :key="category.id"
            :to="{ path: '/threads', query: { category_id: category.id } }"
          >
            <el-tag effect="plain" size="large">{{ category.name }}</el-tag>
          </router-link>
        </div>
      </div>
    </section>

    <section
      v-if="homeConfig.custom_html_enabled && homeConfig.custom_html"
      class="custom-home-html"
      v-html="homeConfig.custom_html"
    />

    <el-card class="welcome-card">
      <template #header>
        <div class="card-header">
          <span>社区概览</span>
        </div>
      </template>
      <p class="overview-text">查看最新帖子、注册用户规模和当前 API 状态。</p>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="注册用户" :value="stats.users" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="帖子总数" :value="stats.threads" />
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="API 状态" value="正常运行" />
        </el-card>
      </el-col>
    </el-row>

    <el-card class="recent-threads" v-if="threads.length > 0">
      <template #header>
        <div class="card-header">
          <span>最新帖子</span>
          <router-link to="/threads">
            <el-button text>查看全部</el-button>
          </router-link>
        </div>
      </template>
      <el-table :data="threads" style="width: 100%">
        <el-table-column prop="title" label="标题">
          <template #default="{ row }">
            <router-link :to="`/threads/${row.id}`">{{ row.title }}</router-link>
          </template>
        </el-table-column>
        <el-table-column prop="author_name" label="作者" width="120" />
        <el-table-column prop="view_count" label="浏览" width="80" />
        <el-table-column prop="reply_count" label="回复" width="80" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import { useUserStore } from '@/stores/user'
import { threadApi, userApi, healthApi, homeApi } from '@/api'

const userStore = useUserStore()
const threads = ref<any[]>([])
const stats = ref({ users: 0, threads: 0 })
const homeConfig = reactive({
  hero_title: '欢迎来到 CampusOS',
  hero_subtitle: '下一代校园社区引擎 - 事件驱动、AI Native 的社区操作系统',
  background_image: '',
  background_overlay: 'rgba(15, 23, 42, 0.45)',
  show_category_tags: true,
  category_tags: [] as Array<{ id: string; name: string; slug: string }>,
  custom_html_enabled: false,
  custom_html: '',
})

const heroStyle = computed<Record<string, string>>(() => {
  if (!homeConfig.background_image) return {} as Record<string, string>
  return {
    backgroundImage: `linear-gradient(${homeConfig.background_overlay}, ${homeConfig.background_overlay}), url("${homeConfig.background_image}")`,
  }
})

onMounted(async () => {
  try {
    await healthApi.check()
    const [threadRes, userRes, homeRes]: any = await Promise.all([
      threadApi.list({ page: 1, page_size: 5 }),
      userApi.list({ page: 1, page_size: 1 }),
      homeApi.config(),
    ])
    threads.value = threadRes?.data?.items || []
    stats.value.threads = threadRes?.data?.pagination?.total || 0
    stats.value.users = userRes?.data?.pagination?.total || 0
    Object.assign(homeConfig, {
      hero_title: homeRes?.data?.hero_title || homeConfig.hero_title,
      hero_subtitle: homeRes?.data?.hero_subtitle || homeConfig.hero_subtitle,
      background_image: homeRes?.data?.background_image || '',
      background_overlay: homeRes?.data?.background_overlay || homeConfig.background_overlay,
      show_category_tags: homeRes?.data?.show_category_tags ?? true,
      category_tags: homeRes?.data?.category_tags || [],
      custom_html_enabled: Boolean(homeRes?.data?.custom_html_enabled),
      custom_html: homeRes?.data?.custom_html || '',
    })
  } catch (e) {
    console.log('API not available yet')
  }
})
</script>

<style scoped>
.home { max-width: 980px; margin: 0 auto; }
.welcome-hero {
  min-height: 260px;
  margin-bottom: 24px;
  padding: 34px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #f8fafc;
  background-position: center;
  background-size: cover;
  display: flex;
  align-items: center;
}
.welcome-hero.has-background {
  color: #fff;
}
.hero-content {
  max-width: 760px;
}
.hero-content h1 {
  margin: 0 0 12px;
  font-size: 34px;
  line-height: 1.2;
}
.welcome-card { margin-bottom: 24px; }
.custom-home-html {
  margin-bottom: 24px;
  overflow-wrap: anywhere;
}
.custom-home-html :deep(*) {
  max-width: 100%;
}
.custom-home-html :deep(img) {
  height: auto;
}
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header h1 { margin: 0; }
.subtitle { color: inherit; font-size: 16px; margin-bottom: 20px; line-height: 1.7; }
.overview-text { margin: 0; color: #606266; }
.actions { display: flex; gap: 12px; }
.category-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}
.category-tags a {
  text-decoration: none;
}
.stats-row { margin-bottom: 24px; }
@media (max-width: 720px) {
  .welcome-hero {
    padding: 24px;
  }
  .hero-content h1 {
    font-size: 28px;
  }
}
</style>
