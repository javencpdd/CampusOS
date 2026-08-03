<template>
  <div class="thread-taxonomy" aria-label="帖子板块与标签信息">
    <div class="taxonomy-item">
      <span class="taxonomy-label">板块</span>
      <router-link
        v-if="category"
        class="category-link"
        :to="{ path: '/threads', query: { category_id: category.id } }"
        :title="category.description || `查看${category.name}板块的帖子`"
      >
        <el-tag size="small" effect="plain">{{ category.icon ? `${category.icon} ` : '' }}{{ category.name }}</el-tag>
      </router-link>
      <span v-else class="taxonomy-empty">{{ categoryStatus }}</span>
    </div>
    <div class="taxonomy-item">
      <span class="taxonomy-label">标签</span>
      <div v-if="normalizedTags.length" class="taxonomy-tags">
        <el-tag v-for="tag in normalizedTags" :key="tag" size="small" effect="plain" type="info">{{ tag }}</el-tag>
      </div>
      <span v-else class="taxonomy-empty">暂无标签</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { categoryApi } from '@/modules/community/api'
import { normalizeThreadTags } from '@/modules/community/threadTags'

type CategorySummary = {
  id: string
  name: string
  description?: string
  icon?: string
}

const props = withDefaults(
  defineProps<{
    categoryId?: string
    tags?: string[]
  }>(),
  {
    categoryId: '',
    tags: () => [],
  },
)

const category = ref<CategorySummary | null>(null)
const categoryLoading = ref(false)
const categoryFailed = ref(false)

const normalizedTags = computed(() => normalizeThreadTags(props.tags))

const categoryStatus = computed(() => {
  if (!String(props.categoryId || '').trim()) return '未指定板块'
  if (categoryLoading.value) return '正在加载…'
  return categoryFailed.value ? '板块信息暂不可用' : '未找到板块'
})

const loadCategory = async (value?: string) => {
  const categoryID = String(value || '').trim()
  category.value = null
  categoryFailed.value = false
  if (!categoryID) return

  categoryLoading.value = true
  try {
    const response: any = await categoryApi.get(categoryID)
    if (String(props.categoryId || '').trim() !== categoryID) return
    const item = response?.data
    if (item?.id && item?.name) {
      category.value = item
    } else {
      categoryFailed.value = true
    }
  } catch {
    if (String(props.categoryId || '').trim() === categoryID) categoryFailed.value = true
  } finally {
    if (String(props.categoryId || '').trim() === categoryID) categoryLoading.value = false
  }
}

watch(() => props.categoryId, loadCategory, { immediate: true })
</script>

<style scoped>
.thread-taxonomy {
  display: grid;
  gap: 8px;
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--campus-border-color, #e4e7ed);
  border-radius: 6px;
  background: var(--campus-surface-background, #f8fafc);
}
.taxonomy-item,
.taxonomy-tags {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 7px;
}
.taxonomy-label {
  min-width: 36px;
  color: var(--campus-muted-color, #606266);
  font-size: 13px;
  font-weight: 600;
}
.category-link {
  display: inline-flex;
  text-decoration: none;
}
.taxonomy-empty {
  color: var(--campus-muted-color, #909399);
  font-size: 13px;
}
</style>
