export const DEFAULT_CONFIG = Object.freeze({ enabled: false, allowedGroups: '', siteUrl: '', prefix: '/campus' });

export function id(value, signed = false) {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) return null;
  if (!['string', 'number'].includes(typeof value)) return null;
  const text = String(value);
  return (signed ? /^-?[1-9]\d{0,19}$/ : /^[1-9]\d{0,19}$/).test(text) ? text : null;
}

export function validateConfig(raw) {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw Error('配置必须为对象');
  if (Object.keys(raw).some(key => !Object.hasOwn(DEFAULT_CONFIG, key))) throw Error('存在未知配置项');
  const config = { ...DEFAULT_CONFIG, ...raw };
  if (typeof config.enabled !== 'boolean' || typeof config.allowedGroups !== 'string' ||
      config.allowedGroups.length > 220 || typeof config.siteUrl !== 'string' || config.siteUrl.length > 200 ||
      typeof config.prefix !== 'string' || !/^[!/][A-Za-z][A-Za-z0-9_-]{0,15}$/.test(config.prefix)) throw Error('配置格式无效');
  const groups = config.allowedGroups.trim() ? config.allowedGroups.trim().split(/[\s,，]+/) : [];
  if (groups.length > 10 || groups.some(group => !id(group)) || new Set(groups).size !== groups.length) throw Error('群白名单无效');
  let base = '';
  if (config.siteUrl) {
    let url;
    try { url = new URL(config.siteUrl); } catch { throw Error('官网地址无效'); }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash ||
        /[\s\[\]]/.test(config.siteUrl)) throw Error('官网地址只允许无凭据的 HTTP(S) 地址');
    base = url.href.replace(/\/?$/, '/');
  }
  if (config.enabled && (!groups.length || !base)) throw Error('启用前须设置群白名单和官网地址');
  return Object.freeze({ ...config, siteUrl: base, groups: Object.freeze(groups) });
}

export function parseEvent(event, config) {
  if (!config.enabled || !event || event.post_type !== 'message' || event.message_type !== 'group') return null;
  const group = id(event.group_id), user = id(event.user_id), self = id(event.self_id), messageId = id(event.message_id, true);
  if (!group || !user || !self || !messageId || user === self || !config.groups.includes(group)) return null;
  if (!Array.isArray(event.message) || event.message.length > 16) return null;
  let text = '', mentioned = false;
  for (const segment of event.message) {
    if (segment?.type === 'at' && !mentioned && !text.trim() && String(segment.data?.qq) === self) mentioned = true;
    else if (segment?.type === 'text' && typeof segment.data?.text === 'string') text += segment.data.text;
    else return null;
    if (text.length > 512) return null;
  }
  text = text.trim();
  if (text !== config.prefix && !text.startsWith(config.prefix + ' ') && !text.startsWith(config.prefix + '\n')) return null;
  const command = text.slice(config.prefix.length).trim();
  return { group, user, self, messageId, command };
}

export function validateCatalog(catalog) {
  if (catalog?.schema !== 'campusos.public-docs/v1' || !Array.isArray(catalog.documents) ||
      catalog.documents.length < 1 || catalog.documents.length > 32) throw Error('公开文档目录无效');
  const seen = new Set();
  for (const doc of catalog.documents) {
    if (!doc || !/^(guide|plugins|project|deployment)\/[a-z0-9-]+\.html$/.test(doc.path) || seen.has(doc.path) ||
        typeof doc.title !== 'string' || doc.title.length > 80 || typeof doc.summary !== 'string' || doc.summary.length > 160 ||
        typeof doc.text !== 'string' || doc.text.length > 40000 || !/^[a-f0-9]{64}$/.test(doc.sha256)) throw Error('公开文档条目无效');
    if (/[\r\n\[\]]/.test(doc.title + doc.summary)) throw Error('公开文档展示字段无效');
    seen.add(doc.path);
  }
  return catalog.documents;
}

export function search(documents, query) {
  const normalized = query.normalize('NFKC').toLocaleLowerCase('en-US').trim();
  const terms = normalized.split(/\s+/u).filter(Boolean);
  // Explicit AND terms keep results explainable; no fuzzy guesses or model output.
  return documents.map(doc => {
    const title = doc.title.normalize('NFKC').toLocaleLowerCase('en-US');
    const body = (doc.summary + ' ' + doc.text).normalize('NFKC').toLocaleLowerCase('en-US');
    const score = terms.length && terms.every(term => title.includes(term) || body.includes(term))
      ? terms.reduce((sum, term) => sum + (title.includes(term) ? 10 : 1), 0) : 0;
    return { ...doc, score };
  }).filter(doc => doc.score).sort((a, b) => b.score - a.score || a.path.localeCompare(b.path)).slice(0, 3);
}

export function reply(command, config, documents) {
  const prefix = config.prefix;
  if (!command || ['help', '帮助'].includes(command)) return `CampusOS 文档助手\n${prefix} ping — 检查插件\n${prefix} 官网 — 项目入口\n${prefix} 搜索 关键词 — 搜索随包公开文档\n仅查询公开项目文档，不读取群聊历史或私人知识。`;
  if (command === 'ping') return 'CampusOS 文档助手在线（文档索引就绪；不代表平台 API 或模型在线）。';
  if (['官网', 'site'].includes(command)) return `CampusOS 官网\n${config.siteUrl}\n插件教程\n${config.siteUrl}plugins/qqbot-napcat-tutorial.html`;
  const match = /^(?:搜索|search)\s+(.+)$/su.exec(command);
  if (!match || /[\u0000-\u001f\u007f]/u.test(match[1]) || match[1].trim().length > 80) return `用法：${prefix} 搜索 关键词（1–80 字）；${prefix} 帮助`;
  const results = search(documents, match[1]);
  if (!results.length) return `公开文档目录未命中。可尝试“插件”“知识”或“部署”；${prefix} 官网 查看完整文档。`;
  return ['CampusOS 公开文档（随包关键词索引）', ...results.map((doc, index) => `${index + 1}. ${doc.title}\n${doc.summary}\n${config.siteUrl}${doc.path}`)].join('\n\n');
}
