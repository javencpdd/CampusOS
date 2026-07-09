<template>
  <div class="thread-detail" v-loading="loading">
    <el-card v-if="thread" class="thread-card">
      <template #header>
        <div class="thread-heading">
          <div>
            <h2>{{ article?.title || thread.title }}</h2>
            <div class="meta">
              <span>作者：{{ thread.author_name }}</span>
              <span>发布于：{{ new Date(article?.published_at || thread.created_at).toLocaleString() }}</span>
              <span>浏览：{{ thread.view_count }}</span>
              <span>回复：{{ thread.reply_count }}</span>
              <el-tag v-if="thread.content_format === 'richtext_article'" type="success" size="small">图文</el-tag>
              <el-tag v-if="thread.status === 'private'" type="warning" size="small">私密</el-tag>
              <el-tag v-for="tag in thread.tags" :key="tag" size="small">{{ tag }}</el-tag>
            </div>
          </div>
          <div v-if="canManageArticle || canManagePlainThread" class="article-actions">
            <template v-if="canManageArticle">
              <el-button size="small" @click="$router.push(`/threads/${thread.id}/edit`)">编辑</el-button>
              <el-button size="small" @click="offlineArticle" :loading="articleOperating">下架</el-button>
              <el-button size="small" type="danger" plain @click="deleteArticle" :loading="articleOperating">删除</el-button>
            </template>
            <template v-else-if="canManagePlainThread">
              <el-button size="small" @click="$router.push(`/threads/${thread.id}/edit`)">编辑</el-button>
              <el-button size="small" @click="togglePlainPrivacy" :loading="articleOperating">
                {{ thread.status === 'private' ? '设为公开' : '设为私密' }}
              </el-button>
              <el-button size="small" type="danger" plain @click="deletePlainThread" :loading="articleOperating">删除</el-button>
            </template>
          </div>
        </div>
      </template>
      <template v-if="article">
        <img v-if="article.cover_url" class="article-cover" :src="article.cover_url" alt="cover" />
        <p v-if="article.summary" class="article-summary">{{ article.summary }}</p>
        <article class="article-content" v-html="article.sanitized_html"></article>
      </template>
      <div v-else class="content">{{ thread.content }}</div>
    </el-card>

    <el-card v-if="thread" class="reply-card" v-loading="postsLoading">
      <template #header>
        <div class="reply-header">
          <span>回复</span>
          <el-tag size="small" effect="plain">{{ postsTotal }}</el-tag>
        </div>
      </template>

      <div v-if="posts.length > 0" class="reply-list">
        <div v-for="post in posts" :key="post.id" class="reply-item">
          <div class="reply-meta">
            <strong class="reply-floor">第 {{ post.floor_number || '-' }} 楼</strong>
            <span class="reply-author">{{ post.author_name }}</span>
            <span>{{ new Date(post.created_at).toLocaleString() }}</span>
            <el-button text type="primary" size="small" @click="setReplyTarget(post)">回复</el-button>
          </div>
          <div v-if="post.parent_id" class="reply-parent">
            回复：第 {{ parentFloor(post.parent_id) || post.parent_id }} 楼
          </div>
          <div class="reply-content">{{ post.content }}</div>
        </div>
      </div>
      <el-empty v-else description="暂无回复" />

      <div class="pagination" v-if="postsTotal > postsPageSize">
        <el-pagination
          v-model:current-page="postsPage"
          :page-size="postsPageSize"
          :total="postsTotal"
          layout="prev, pager, next"
          @current-change="loadPosts"
        />
      </div>
    </el-card>

    <el-card v-if="thread" class="reply-editor">
      <template #header>
        <div class="reply-header">
          <span>{{ replyTarget ? `回复第 ${replyTarget.floor_number || '-'} 楼` : '发表回复' }}</span>
          <el-button v-if="replyTarget" text size="small" @click="clearReplyTarget">取消引用</el-button>
        </div>
      </template>

      <template v-if="isLoggedIn">
        <el-input
          v-model="replyContent"
          type="textarea"
          :rows="5"
          maxlength="2000"
          show-word-limit
          placeholder="写下你的回复..."
        />
        <div class="reply-actions">
          <el-button type="primary" :loading="submitting" @click="submitReply">提交回复</el-button>
        </div>
      </template>
      <el-alert v-else type="info" :closable="false" show-icon>
        <template #title>
          登录后可以回复帖子。
          <router-link class="login-link" to="/login">去登录</router-link>
        </template>
      </el-alert>
    </el-card>

    <el-empty v-if="!loading && !thread" description="帖子不存在" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { postApi, richTextApi, threadApi } from '@/api'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const thread = ref<any>(null)
const article = ref<any>(null)
const posts = ref<any[]>([])
const loading = ref(false)
const postsLoading = ref(false)
const submitting = ref(false)
const articleOperating = ref(false)
const replyContent = ref('')
const replyTarget = ref<any>(null)
const postsPage = ref(1)
const postsPageSize = 20
const postsTotal = ref(0)
const isLoggedIn = computed(() => userStore.isLoggedIn)
const canManageArticle = computed(() => Boolean(article.value && userStore.user?.id === thread.value?.author_id))
const canManagePlainThread = computed(() => Boolean(
  !article.value &&
  thread.value?.content_format !== 'richtext_article' &&
  userStore.user?.id === thread.value?.author_id,
))

const threadID = () => route.params.id as string

const loadThread = async () => {
  loading.value = true
  try {
    const res: any = userStore.isLoggedIn
      ? await threadApi.getMine(threadID()).catch(() => threadApi.get(threadID()))
      : await threadApi.get(threadID())
    if (res.code === 0) {
      thread.value = res.data
      await loadArticle()
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '加载帖子失败')
  } finally {
    loading.value = false
  }
}

const loadArticle = async () => {
  article.value = null
  if (!threadID()) return
  try {
    const res: any = userStore.isLoggedIn
      ? await richTextApi.getMine(threadID()).catch(() => richTextApi.getPublished(threadID()))
      : await richTextApi.getPublished(threadID())
    if (res.code === 0) article.value = res.data
  } catch {
    article.value = null
  }
}

const loadPosts = async () => {
  if (!threadID()) return
  postsLoading.value = true
  try {
    const res: any = userStore.isLoggedIn
      ? await postApi.listMine(threadID(), { page: postsPage.value, page_size: postsPageSize }).catch(() => postApi.list(threadID(), { page: postsPage.value, page_size: postsPageSize }))
      : await postApi.list(threadID(), { page: postsPage.value, page_size: postsPageSize })
    if (res.code === 0) {
      posts.value = res.data?.items || []
      postsTotal.value = res.data?.pagination?.total || 0
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '加载回复失败')
  } finally {
    postsLoading.value = false
  }
}

const offlineArticle = async () => {
  articleOperating.value = true
  try {
    await richTextApi.offline(threadID())
    ElMessage.success('文章已下架')
    router.push('/threads')
  } catch (e: any) {
    ElMessage.error(e?.msg || '下架失败')
  } finally {
    articleOperating.value = false
  }
}

const deleteArticle = async () => {
  articleOperating.value = true
  try {
    await richTextApi.delete(threadID())
    ElMessage.success('文章已删除')
    router.push('/threads')
  } catch (e: any) {
    ElMessage.error(e?.msg || '删除失败')
  } finally {
    articleOperating.value = false
  }
}

const togglePlainPrivacy = async () => {
  if (!thread.value) return
  articleOperating.value = true
  try {
    const nextStatus = thread.value.status === 'private' ? 'published' : 'private'
    const res: any = await threadApi.update(threadID(), { status: nextStatus })
    if (res.code === 0) {
      thread.value = res.data
      ElMessage.success(nextStatus === 'private' ? '已设为私密' : '已设为公开')
    }
  } catch (e: any) {
    ElMessage.error(e?.msg || '可见性更新失败')
  } finally {
    articleOperating.value = false
  }
}

const deletePlainThread = async () => {
  articleOperating.value = true
  try {
    await threadApi.delete(threadID())
    ElMessage.success('帖子已删除')
    router.push('/threads')
  } catch (e: any) {
    ElMessage.error(e?.msg || '删除失败')
  } finally {
    articleOperating.value = false
  }
}

const setReplyTarget = (post: any) => {
  replyTarget.value = post
}

const clearReplyTarget = () => {
  replyTarget.value = null
}

const parentFloor = (parentID: string) => {
  const parent = posts.value.find((item) => item.id === parentID)
  return parent?.floor_number
}

const submitReply = async () => {
  const content = replyContent.value.trim()
  if (!content) {
    ElMessage.warning('请输入回复内容')
    return
  }
  submitting.value = true
  try {
    await postApi.create(threadID(), {
      content,
      parent_id: replyTarget.value?.id,
    })
    replyContent.value = ''
    replyTarget.value = null
    if (thread.value) {
      thread.value.reply_count = Number(thread.value.reply_count || 0) + 1
    }
    await loadPosts()
    ElMessage.success('回复已发布')
  } catch (e: any) {
    ElMessage.error(e?.msg || '回复失败')
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await loadThread()
  await loadPosts()
})
</script>

<style scoped>
.thread-detail {
  display: grid;
  gap: 16px;
}
.thread-card,
.reply-card,
.reply-editor {
  border-radius: 8px;
}
.thread-heading {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}
.thread-heading h2 {
  margin: 0 0 8px;
}
.article-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-content: flex-start;
  justify-content: flex-end;
}
.meta { display: flex; gap: 16px; color: #909399; font-size: 14px; flex-wrap: wrap; align-items: center; }
.content { line-height: 1.8; white-space: pre-wrap; }
.article-cover {
  width: 100%;
  max-height: 360px;
  object-fit: cover;
  border-radius: 8px;
  margin-bottom: 16px;
}
.article-summary {
  margin: 0 0 18px;
  padding: 12px 14px;
  border-left: 4px solid #409eff;
  background: #f5f7fa;
  color: #606266;
  line-height: 1.7;
}
.article-content {
  max-width: 760px;
  margin: 0 auto;
  font-size: 16px;
  line-height: 1.8;
  color: #222;
}
.article-content :deep(p) {
  margin: 12px 0;
}
.article-content :deep(img) {
  max-width: 100%;
  height: auto;
  display: block;
  margin: 16px auto;
  border-radius: 8px;
}
.article-content :deep(h1),
.article-content :deep(h2),
.article-content :deep(h3) {
  margin: 24px 0 12px;
  line-height: 1.5;
}
.article-content :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  background: #f6f8fa;
  border-left: 4px solid #ddd;
}
.reply-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.reply-list {
  display: grid;
  gap: 14px;
}
.reply-item {
  padding-bottom: 14px;
  border-bottom: 1px solid #ebeef5;
}
.reply-item:last-child {
  border-bottom: 0;
}
.reply-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  color: #909399;
  font-size: 13px;
}
.reply-meta strong {
  color: #303133;
}
.reply-floor {
  min-width: 64px;
}
.reply-author {
  color: #303133;
  font-weight: 600;
}
.reply-parent {
  margin-top: 8px;
  padding: 8px 10px;
  background: #f5f7fa;
  color: #606266;
  border-radius: 6px;
  font-size: 13px;
}
.reply-content {
  margin-top: 10px;
  line-height: 1.7;
  white-space: pre-wrap;
}
.reply-actions {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
.login-link {
  margin-left: 8px;
}
.pagination {
  margin-top: 16px;
  display: flex;
  justify-content: center;
}
@media (max-width: 720px) {
  .thread-heading {
    flex-direction: column;
  }
  .article-actions {
    justify-content: flex-start;
  }
}
</style>
