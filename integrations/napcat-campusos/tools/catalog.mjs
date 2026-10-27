import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { validateCatalog } from '../core.mjs';

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(pluginRoot, '../..');
// Explicit public allowlist. Never crawl progress records, configuration or user files.
const sources = [
  ['guide/getting-started', '项目快速入门', 'CampusOS 开发前提和使用入口。'],
  ['guide/module-plugin-resource-boundaries', '插件、模块与资源边界', '了解平台插件的合同、隔离和授权边界。'],
  ['guide/public-knowledge-search', '公开知识搜索', '公开知识集合、来源与检索能力的使用说明。'],
  ['guide/private-knowledge', '私人知识', '个人文档的访问限制及私人知识使用方式。'],
  ['guide/cited-rag', '引用式 RAG 问答', '基于知识来源的引用问答及其条件。'],
  ['guide/managed-ai', '受管 AI', '受管模型配置与使用说明。'],
  ['deployment/development', '开发部署', '本地开发环境、端口和启动方式。'],
  ['plugins/qqbot-napcat-tutorial', 'QQ 文档助手插件教程', '从需求、指令和测试到 NapCat 插件安装。']
];

export function buildCatalog() {
  return { schema: 'campusos.public-docs/v1', documents: sources.map(([source, title, summary]) => {
    const bytes = fs.readFileSync(path.join(repoRoot, 'docs-site', source + '.md'));
    const text = bytes.toString('utf8').replace(/^---\n[\s\S]*?\n---\n/, '').replace(/```[\s\S]*?```/g, ' ').replace(/!\[[^\]]*\]\([^)]*\)/g, ' ').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    return { path: source + '.html', title, summary, text, sha256: createHash('sha256').update(bytes).digest('hex') };
  }) };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const catalog = buildCatalog();
  validateCatalog(catalog);
  const expected = JSON.stringify(catalog, null, 2) + '\n';
  const target = path.join(pluginRoot, 'catalog.json');
  if (process.argv.includes('--check')) {
    if (!fs.existsSync(target) || fs.readFileSync(target, 'utf8') !== expected) { process.stderr.write('公开目录已过期，请运行 node integrations/napcat-campusos/tools/catalog.mjs\n'); process.exitCode = 1; }
    else process.stdout.write(`公开文档目录一致：${catalog.documents.length} 篇\n`);
  } else { fs.writeFileSync(target, expected); process.stdout.write(`已生成 ${catalog.documents.length} 篇公开文档目录\n`); }
}
