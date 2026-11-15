import test from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_CONFIG, validateConfig, parseEvent, validateCatalog, reply, search } from '../core.mjs';

const config = validateConfig({ enabled: true, allowedGroups: '123456', siteUrl: 'http://example.test:3002' });
export const event = (text = '/campus ping', changes = {}) => ({ post_type: 'message', message_type: 'group', group_id: 123456, user_id: 222, self_id: 333, message_id: 1, message: [{ type: 'text', data: { text } }], ...changes });
const docs = [{ path: 'plugins/overview.html', title: '插件开发', summary: '公开插件教程', text: '知识 插件 manifest', sha256: 'a'.repeat(64) }, { path: 'guide/introduction.html', title: '项目介绍', summary: '公开知识', text: '知识 插件', sha256: 'b'.repeat(64) }];

test('default off; enabling requires explicit group scope and public URL', () => {
  assert.equal(validateConfig(DEFAULT_CONFIG).enabled, false);
  for (const bad of [{ enabled: true }, { allowedGroups: '01' }, { allowedGroups: '1,1' }, { enabled: 'true' }, { prefix: '/' }, { siteUrl: 'file:///tmp' }, { siteUrl: 'https://secret@example.test/' }, { siteUrl: 'https://example.test/?token=x' }, { surprise: true }]) assert.throws(() => validateConfig(bad));
});
test('only explicit commands in whitelisted incoming group messages', () => {
  assert.equal(parseEvent(event(), config).command, 'ping');
  for (const changes of [{ message_type: 'private' }, { group_id: 789 }, { user_id: 333 }, { post_type: 'message_sent' }, { message_id: null }, { user_id: Number.MAX_SAFE_INTEGER + 1 }]) assert.equal(parseEvent(event(undefined, changes), config), null);
  for (const text of ['hello /campus ping', '/campusfoo', '[CQ:at,qq=333] /campus ping', 'x'.repeat(513)]) assert.equal(parseEvent(event(text), config), null);
  assert.equal(parseEvent(event(), validateConfig(DEFAULT_CONFIG)), null);
});
test('supports one leading self mention, rejects other mentions and media', () => {
  const segments = [{ type: 'at', data: { qq: '333' } }, { type: 'text', data: { text: ' /campus 搜索 插件' } }];
  assert.equal(parseEvent(event('', { message: segments }), config).command, '搜索 插件');
  for (const first of [{ type: 'at', data: { qq: 'all' } }, { type: 'image', data: {} }, { type: 'reply', data: { id: '1' } }]) assert.equal(parseEvent(event('', { message: [first, ...segments.slice(1)] }), config), null);
});
test('ranked AND search has at most three results; no-hit and help are useful', () => {
  assert.equal(validateCatalog({ schema: 'campusos.public-docs/v1', documents: docs }).length, 2);
  assert.equal(search(docs, '插件 知识')[0].title, '插件开发');
  assert.equal(search(docs, 'missing').length, 0);
  assert.match(reply('搜索 插件', config, docs), /http:\/\/example.test:3002\/plugins\/overview.html/);
  assert.match(reply('搜索 不存在', config, docs), /未命中/);
  assert.match(reply('帮助', config, docs), /搜索/);
  assert.match(reply('官网', config, docs), /qqbot-napcat-tutorial/);
  assert.match(reply('ping', config, docs), /不代表平台 API/);
  assert.match(reply('搜索 ' + '字'.repeat(81), config, docs), /1–80/);
  assert.match(reply('搜索 a\nb', config, docs), /用法/);
  assert.throws(() => validateCatalog({ schema: 'campusos.public-docs/v1', documents: [{ ...docs[0], path: '../secret.html' }] }));
});
