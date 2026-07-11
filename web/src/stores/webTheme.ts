import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { webThemeApi } from '@/api'

export interface WebThemeItem {
  name: string
  display_name: string
  version: string
  description?: string
  preview_url?: string
  tokens?: Record<string, string>
  capabilities?: string[]
}

interface WebThemeCatalog {
  enabled: boolean
  allow_user_switch: boolean
  default_style_pack?: string
  items: WebThemeItem[]
}

interface RuntimePackage {
  manifest: WebThemeItem & { capabilities?: string[] }
  css?: string
  effect_js?: string
}

interface ApiEnvelope<T> {
  data: T
}

const publicCapabilities = new Set(['community.threads.read', 'categories.read'])

export const useWebThemeStore = defineStore('web-theme', () => {
  const catalog = ref<WebThemeCatalog>({ enabled: false, allow_user_switch: false, items: [] })
  const activeName = ref('')
  const activePackage = ref<RuntimePackage | null>(null)
  const grantedCapabilities = ref<string[]>([])
  const loading = ref(false)
  const error = ref('')
  let styleElement: HTMLStyleElement | null = null

  const activeItem = computed(() => catalog.value.items.find((item) => item.name === activeName.value) || null)
  const effectiveCapabilities = computed(() => {
    const declared = activePackage.value?.manifest.capabilities || []
    return declared.filter((capability) => publicCapabilities.has(capability) || grantedCapabilities.value.includes(capability))
  })

  const userKey = (userID?: string) => userID || 'guest'
  const selectionKey = (userID?: string) => `campusos.web-theme.selection.${userKey(userID)}`
  const grantsKey = (userID?: string, themeName?: string) => `campusos.web-theme.grants.${userKey(userID)}.${themeName || 'none'}`

  const readGrants = (userID: string | undefined, themeName: string) => {
    try {
      const parsed = JSON.parse(localStorage.getItem(grantsKey(userID, themeName)) || '[]')
      return Array.isArray(parsed) ? parsed.filter((item) => typeof item === 'string') : []
    } catch {
      return []
    }
  }

  const clearRuntime = () => {
    styleElement?.remove()
    styleElement = null
    activePackage.value = null
    activeName.value = ''
    grantedCapabilities.value = []
  }

  const applyPackage = async (name: string, userID?: string, grants?: string[]) => {
    const response = await webThemeApi.package(name) as unknown as ApiEnvelope<RuntimePackage>
    const pack = response.data
    styleElement?.remove()
    styleElement = document.createElement('style')
    styleElement.setAttribute('data-campusos-style-pack', 'web')
    styleElement.setAttribute('data-theme-name', name)
    styleElement.textContent = pack.css || ''
    document.head.appendChild(styleElement)
    activeName.value = name
    activePackage.value = pack
    grantedCapabilities.value = grants ?? readGrants(userID, name)
  }

  const initialize = async (userID?: string) => {
    loading.value = true
    error.value = ''
    try {
      const response = await webThemeApi.catalog() as unknown as ApiEnvelope<WebThemeCatalog>
      catalog.value = response.data
      if (!catalog.value.enabled || catalog.value.items.length === 0) {
        clearRuntime()
        return
      }
      const saved = localStorage.getItem(selectionKey(userID)) || ''
      const selected = catalog.value.allow_user_switch && catalog.value.items.some((item) => item.name === saved)
        ? saved
        : catalog.value.default_style_pack || catalog.value.items[0].name
      await applyPackage(selected, userID)
    } catch (cause: any) {
      clearRuntime()
      error.value = cause?.msg || cause?.message || '系统风格包加载失败'
    } finally {
      loading.value = false
    }
  }

  const select = async (name: string, userID?: string, grants: string[] = []) => {
    if (!catalog.value.enabled || !catalog.value.allow_user_switch) {
      throw new Error('管理员当前未开放用户切换系统风格包')
    }
    if (!catalog.value.items.some((item) => item.name === name)) {
      throw new Error('风格包不在管理员提供的目录中')
    }
    await applyPackage(name, userID, grants)
    localStorage.setItem(selectionKey(userID), name)
    localStorage.setItem(grantsKey(userID, name), JSON.stringify(grants))
  }

  const restoreDefault = async (userID?: string) => {
    localStorage.removeItem(selectionKey(userID))
    const fallback = catalog.value.default_style_pack || catalog.value.items[0]?.name
    if (fallback) await applyPackage(fallback, userID)
  }

  return {
    catalog,
    activeName,
    activePackage,
    activeItem,
    effectiveCapabilities,
    loading,
    error,
    initialize,
    select,
    restoreDefault,
  }
})
