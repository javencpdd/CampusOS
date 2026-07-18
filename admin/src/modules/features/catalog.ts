export interface FeatureRow {
  id: string
  label: string
  description: string
  representative: string
  parentId?: string
  configSources: Array<{ name: string; label: string }>
  state?: Record<string, any>
}

export const builtinFeatureDefinitions: FeatureRow[] = [
  {
    id: 'personal-space',
    label: '个人空间',
    description: '个人主页、内容同步与个人文件入口。',
    representative: 'personal-space',
    configSources: [{ name: 'personal-space', label: '空间配置' }],
  },
  {
    id: 'controlled-richtext-article',
    label: '图文文章',
    description: '文章草稿、发布、清洗、资产和模板。',
    representative: 'controlled-richtext-article',
    configSources: [{ name: 'controlled-richtext-article', label: '文章配置' }],
  },
  {
    id: 'mutual-aid',
    label: '校园互助',
    description: '复用安全图文正文和用户图片资产，保留独立的互助类型与状态。',
    representative: 'mutual-aid',
    parentId: 'controlled-richtext-article',
    configSources: [],
  },
  {
    id: 'secondhand',
    label: '校园二手',
    description: '复用安全图文正文和用户图片资产，保留独立的价格与交易状态。',
    representative: 'secondhand',
    parentId: 'controlled-richtext-article',
    configSources: [],
  },
  {
    id: 'personal-schedule',
    label: '个人课表',
    description: '学期课表、Excel 导入、周视图和日历。',
    representative: 'personal-schedule',
    configSources: [{ name: 'personal-schedule', label: '课表配置' }],
  },
  {
    id: 'appearance',
    label: '界面与风格',
    description: '系统主题、首页布局和风格资源包。',
    representative: 'web-theme',
    configSources: [
      { name: 'web-theme', label: '主题配置' },
      { name: 'homepage-customizer', label: '首页配置' },
    ],
  },
]

export const mapBuiltinFeatures = (items: any[]): FeatureRow[] => {
  const byIdentifier = new Map<string, any>()
  items.forEach((item) => {
    if (item?.id) byIdentifier.set(String(item.id), item)
    if (item?.name) byIdentifier.set(String(item.name), item)
  })

  return builtinFeatureDefinitions.map((definition) => {
    const aliases = [definition.id, definition.representative, ...definition.configSources.map((source) => source.name)]
    const state = aliases.map((identifier) => byIdentifier.get(identifier)).find(Boolean)
    return {
      ...definition,
      parentId: state?.presentation_parent || definition.parentId,
      configSources: (state?.config_sources || definition.configSources).map((source: any) => ({
        name: source.id || source.name,
        label: source.label,
      })),
      state,
    }
  })
}
