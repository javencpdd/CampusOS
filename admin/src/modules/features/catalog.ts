export interface FeatureRow {
  id: string;
  label: string;
  description: string;
  representative: string;
  parentId?: string;
  management?: { path: string; label: string };
  configSources: Array<{ name: string; label: string }>;
  state?: Record<string, any>;
}

export const builtinFeatureDefinitions: FeatureRow[] = [
  {
    id: "personal-space",
    label: "个人空间",
    description: "个人主页、内容同步与个人文件入口。",
    representative: "personal-space",
    configSources: [{ name: "personal-space", label: "空间配置" }],
  },
  {
    id: "controlled-richtext-article",
    label: "图文文章",
    description: "文章草稿、发布、清洗、资产和模板。",
    representative: "controlled-richtext-article",
    configSources: [{ name: "controlled-richtext-article", label: "文章配置" }],
  },
  {
    id: "mutual-aid",
    label: "校园互助",
    description: "复用安全图文正文和用户图片资产，保留独立的互助类型与状态。",
    representative: "mutual-aid",
    parentId: "controlled-richtext-article",
    configSources: [],
  },
  {
    id: "secondhand",
    label: "校园二手",
    description: "复用安全图文正文和用户图片资产，保留独立的价格与交易状态。",
    representative: "secondhand",
    parentId: "controlled-richtext-article",
    configSources: [],
  },
  {
    id: "personal-schedule",
    label: "个人课表",
    description: "学期课表、Excel 导入、周视图和日历。",
    representative: "personal-schedule",
    configSources: [{ name: "personal-schedule", label: "课表配置" }],
  },
  {
    id: "knowledge",
    label: "知识检索",
    description: "公开知识集合、来源管理、索引与引用检索；私人来源仍需本人逐项同意。",
    representative: "knowledge",
    configSources: [],
    management: { path: "/knowledge", label: "知识来源管理" },
  },
  {
    id: "personal-documents",
    label: "个人文档",
    description: "个人文档、版本与附件，沿用用户存储和本人访问权限。",
    representative: "personal-documents",
    configSources: [],
  },
  {
    id: "ai-gateway",
    label: "AI 网关",
    description: "受管模型、调用预算与执行记录。",
    representative: "ai-gateway",
    configSources: [],
  },
  {
    id: "appearance",
    label: "界面与风格",
    description: "系统主题、首页布局和风格资源包。",
    representative: "web-theme",
    configSources: [
      { name: "web-theme", label: "主题配置" },
      { name: "homepage-customizer", label: "首页配置" },
    ],
  },
];

export const mapBuiltinFeatures = (items: any[]): FeatureRow[] => {
  const identifiers = (definition: FeatureRow) => [definition.id, definition.representative, ...definition.configSources.map(source => source.name)];
  const knownIdentifiers = new Set(builtinFeatureDefinitions.flatMap(identifiers));
  const registered = items.filter(item => {
    if (!item || typeof item !== "object" || item.kind === "core" || item.capability_class === "core") return false;
    if (item.kind && item.kind !== "builtin-feature") return false;
    if (item.capability_class === "external") return false;
    const id = item.id || item.name;
    return typeof id === "string" && /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/.test(id) &&
      (item.kind === "builtin-feature" || item.capability_class === "builtin-feature" || item.capability_class === "legacy-builtin" || knownIdentifiers.has(id));
  });
  const byIdentifier = new Map<string, any>();
  registered.forEach((item) => {
    if (item?.id) byIdentifier.set(String(item.id), item);
    if (item?.name) byIdentifier.set(String(item.name), item);
  });

  const projected = new Set<any>();
  const rows: FeatureRow[] = [];
  const project = (definition: FeatureRow, state: any): FeatureRow => ({
    ...definition,
    parentId: state.presentation_parent || definition.parentId,
    configSources: (state.config_sources || definition.configSources).map((source: any) => ({ name: source.id || source.name, label: source.label })),
    state,
  });
  // Local entries provide presentation and compatibility aliases, never an
  // allowlist. Only registered features receive lifecycle controls.
  builtinFeatureDefinitions.forEach((definition) => {
    const aliases = identifiers(definition);
    const state = aliases
      .map((identifier) => byIdentifier.get(identifier))
      .find(Boolean);
    if (!state) return;
    rows.push(project(definition, state));
    registered.filter(item => aliases.includes(item.id) || aliases.includes(item.name)).forEach(item => projected.add(item));
  });
  registered.forEach((state) => {
    if (projected.has(state) || rows.some(row => row.id === state.id)) return;
    const id = String(state.id || state.name);
    rows.push(project({ id, label: state.display_name || state.name || id, description: state.description || "内置功能", representative: id, configSources: [] }, state));
  });
  return rows;
};
