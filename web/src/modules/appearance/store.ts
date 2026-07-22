import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { webThemeApi } from '@/modules/appearance/api'

export interface WebThemeItem {
  name: string
  display_name: string
  version: string
  description?: string
  preview_url?: string
  desktop_preview_url?: string
  mobile_preview_url?: string
  delivery_status?: 'valid' | 'legacy-readonly' | 'invalid'
  checksum?: string
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
  manifest: WebThemeItem & {
    capabilities?: string[]
    tokens?: Record<string, string>
    layout?: Record<string, string>
  }
  css?: string
  effect_js?: string
  config_schema?: ThemeConfigSchema
}

export interface ThemeConfigProperty {
  type: 'string' | 'number' | 'integer' | 'boolean'
  title?: string
  description?: string
  format?: 'color'
  enum?: Array<string | number>
  enum_names?: string[]
  minimum?: number
  maximum?: number
  default?: string | number | boolean
  'x-campusos-binding'?: string
}

interface ThemeConfigSchema {
  properties?: Record<string, ThemeConfigProperty>
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
  const configValues = ref<Record<string, string | number | boolean>>({})
  const loading = ref(false)
  const error = ref('')
  let styleElement: HTMLStyleElement | null = null

  const activeItem = computed(() => catalog.value.items.find((item) => item.name === activeName.value) || null)
  const effectiveCapabilities = computed(() => {
    const declared = activePackage.value?.manifest.capabilities || []
    return declared.filter(
      (capability) => publicCapabilities.has(capability) || grantedCapabilities.value.includes(capability),
    )
  })
  const configurationFields = computed(() => activePackage.value?.config_schema?.properties || {})
  const effectiveTokens = computed(() => {
    const tokens = { ...(activePackage.value?.manifest.tokens || {}) }
    for (const [key, field] of Object.entries(configurationFields.value)) {
      const binding = field['x-campusos-binding'] || ''
      if (!binding.startsWith('token.') || configValues.value[key] === undefined) continue
      tokens[binding.slice('token.'.length)] = String(configValues.value[key])
    }
    return tokens
  })
  const effectiveLayout = computed(() => {
    const layout = { ...(activePackage.value?.manifest.layout || {}) }
    for (const [key, field] of Object.entries(configurationFields.value)) {
      const binding = field['x-campusos-binding'] || ''
      if (!binding.startsWith('layout.') || configValues.value[key] === undefined) continue
      layout[binding.slice('layout.'.length)] = String(configValues.value[key])
    }
    return layout
  })

  const userKey = (userID?: string) => userID || 'guest'
  const selectionKey = (userID?: string) => `campusos.web-theme.selection.${userKey(userID)}`
  const grantsKey = (userID?: string, themeName?: string) =>
    `campusos.web-theme.grants.${userKey(userID)}.${themeName || 'none'}`
  const configKey = (userID?: string, themeName?: string) =>
    `campusos.web-theme.config.${userKey(userID)}.${themeName || 'none'}`

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
    configValues.value = {}
  }

  const readConfiguration = (pack: RuntimePackage, userID: string | undefined, themeName: string) => {
    let saved: Record<string, unknown> = {}
    try {
      const parsed = JSON.parse(localStorage.getItem(configKey(userID, themeName)) || '{}')
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) saved = parsed
    } catch {
      saved = {}
    }
    const values: Record<string, string | number | boolean> = {}
    for (const [key, field] of Object.entries(pack.config_schema?.properties || {})) {
      const candidate = saved[key] ?? field.default
      if (isAcceptedValue(field, candidate)) {
        values[key] = candidate
      }
    }
    return values
  }

  const isAcceptedValue = (field: ThemeConfigProperty, value: unknown): value is string | number | boolean => {
    if (field.enum?.length && (typeof value === 'boolean' || !field.enum.includes(value as string | number)))
      return false
    if (field.type === 'boolean') return typeof value === 'boolean'
    if (field.type === 'number' || field.type === 'integer') {
      if (typeof value !== 'number' || !Number.isFinite(value)) return false
      if (field.type === 'integer' && !Number.isInteger(value)) return false
      if (field.minimum !== undefined && value < field.minimum) return false
      if (field.maximum !== undefined && value > field.maximum) return false
      return true
    }
    if (field.type !== 'string' || typeof value !== 'string' || value.length > 120) return false
    if (field.format === 'color') {
      return (
        /^#[0-9a-f]{6}([0-9a-f]{2})?$/i.test(value) ||
        /^rgba?\(\s*[\d.]+\s*,\s*[\d.]+\s*,\s*[\d.]+(?:\s*,\s*[\d.]+)?\s*\)$/i.test(value)
      )
    }
    return !/[;{}<>]/.test(value) && !/url\s*\(|javascript:|data:/i.test(value)
  }

  const applyPackage = async (name: string, userID?: string, grants?: string[]) => {
    const response = (await webThemeApi.package(name)) as unknown as ApiEnvelope<RuntimePackage>
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
    configValues.value = readConfiguration(pack, userID, name)
  }

  const initialize = async (userID?: string) => {
    loading.value = true
    error.value = ''
    try {
      const response = (await webThemeApi.catalog()) as unknown as ApiEnvelope<WebThemeCatalog>
      catalog.value = response.data
      if (!catalog.value.enabled || catalog.value.items.length === 0) {
        clearRuntime()
        return
      }
      const saved = localStorage.getItem(selectionKey(userID)) || ''
      const selected =
        catalog.value.allow_user_switch && catalog.value.items.some((item) => item.name === saved)
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

  const saveConfiguration = (userID: string | undefined, values: Record<string, string | number | boolean>) => {
    if (!activeName.value) return
    const accepted: Record<string, string | number | boolean> = {}
    for (const [key, field] of Object.entries(configurationFields.value)) {
      const value = values[key]
      if (value === undefined) continue
      if (!isAcceptedValue(field, value)) continue
      accepted[key] = value
    }
    configValues.value = accepted
    localStorage.setItem(configKey(userID, activeName.value), JSON.stringify(accepted))
  }

  const resetConfiguration = (userID?: string) => {
    if (!activeName.value || !activePackage.value) return
    localStorage.removeItem(configKey(userID, activeName.value))
    configValues.value = readConfiguration(activePackage.value, userID, activeName.value)
  }

  return {
    catalog,
    activeName,
    activePackage,
    activeItem,
    effectiveCapabilities,
    configurationFields,
    configValues,
    effectiveTokens,
    effectiveLayout,
    loading,
    error,
    initialize,
    select,
    restoreDefault,
    saveConfiguration,
    resetConfiguration,
  }
})
