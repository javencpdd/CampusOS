<template>
  <div v-if="category || normalizedTags.length" class="thread-board-chips" aria-label="帖子板块与标签">
    <router-link
      v-if="category"
      class="board-chip-link"
      :to="{ path: '/threads', query: { category_id: category.id } }"
      :title="category.description || `查看${category.name}板块的帖子`"
      @click.stop
    >
      <el-tag size="small" effect="plain">{{ category.icon ? `${category.icon} ` : '' }}{{ category.name }}</el-tag>
    </router-link>
    <el-tag v-for="tag in normalizedTags" :key="tag" class="thread-tag-chip" size="small" effect="plain" type="info">
      {{ tag }}
    </el-tag>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { normalizeThreadTags } from '@/modules/community/threadTags'
import type { CategorySummary } from '@/modules/community/useCategoryCatalog'

const props = withDefaults(
  defineProps<{
    category?: CategorySummary | null
    tags?: string[]
  }>(),
  {
    category: null,
    tags: () => [],
  },
)

const normalizedTags = computed(() => normalizeThreadTags(props.tags))
</script>

<style scoped>
.thread-board-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.board-chip-link {
  display: inline-flex;
  text-decoration: none;
}
.thread-tag-chip {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
