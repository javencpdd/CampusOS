import { ref } from 'vue'
import { categoryApi } from '@/modules/community/api'

export type CategorySummary = {
  id: string
  name: string
  icon?: string
  description?: string
}

// Module-level catalog shared by every list row: one public category tree
// request resolves all board chips instead of one GET per thread row.
const catalog = ref<Map<string, CategorySummary>>(new Map())
const loaded = ref(false)
let pending: Promise<void> | null = null

const flattenInto = (nodes: any[], into: Map<string, CategorySummary>) => {
  for (const node of nodes || []) {
    if (node?.id && node?.name) {
      into.set(String(node.id), {
        id: String(node.id),
        name: String(node.name),
        icon: node.icon ? String(node.icon) : undefined,
        description: node.description ? String(node.description) : undefined,
      })
    }
    if (Array.isArray(node?.children) && node.children.length) flattenInto(node.children, into)
  }
}

export const ensureCategoryCatalog = async (): Promise<void> => {
  if (loaded.value) return
  if (pending) return pending
  pending = (async () => {
    try {
      const response: any = await categoryApi.tree()
      const next = new Map<string, CategorySummary>()
      flattenInto((response?.data || []) as any[], next)
      catalog.value = next
      loaded.value = true
    } catch (error) {
      // The list stays usable without board chips; rows simply render no chip.
      console.error(error)
    } finally {
      pending = null
    }
  })()
  return pending
}

export const resolveCategory = (categoryId?: string): CategorySummary | null => {
  const key = String(categoryId || '').trim()
  if (!key) return null
  return catalog.value.get(key) || null
}

// Test-only helper: restores the shared catalog to its initial empty state.
export const resetCategoryCatalog = (): void => {
  catalog.value = new Map()
  loaded.value = false
  pending = null
}
