import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { CampusBot } from '../bot.mjs';
import { ReceiptStore, receiptKey } from '../receipts.mjs';

function fixture(t, overrides = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'campusos-qq-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  let clock = 1800000000000;
  const now = () => clock;
  const file = path.join(dir, 'receipts.json');
  const store = new ReceiptStore(file, { now });
  const sent = [];
  const config = { enabled: true, allowedGroups: '123456', siteUrl: 'https://example.test/' };
  const options = { config, documents: [], store, now, send: async (group, text) => { sent.push({ group, text }); return { message_id: 999 }; }, ...overrides };
  const bot = new CampusBot(options);
  const event = (messageId = 1, changes = {}) => ({ post_type: 'message', message_type: 'group', group_id: 123456, user_id: 222, self_id: 333, message_id: messageId, time: Math.floor(clock / 1000), message: [{ type: 'text', data: { text: '/campus ping' } }], ...changes });
  return { bot, store, sent, file, now, options, event, advance: value => { clock += value; } };
}

test('successful send persists before dispatch and deduplicates across a reload', async t => {
  const f = fixture(t);
  await Promise.all([f.bot.handle(f.event()), f.bot.handle(f.event())]);
  assert.equal(f.sent.length, 1);
  assert.equal(f.sent[0].group, '123456');
  const raw = fs.readFileSync(f.file, 'utf8');
  assert.doesNotMatch(raw, /123456|\/campus|222|333/);
  assert.equal(JSON.parse(raw).entries[0].status, 'sent');
  const reloaded = new CampusBot({ ...f.options, store: new ReceiptStore(f.file, { now: f.now }) });
  await reloaded.handle(f.event());
  assert.equal(f.sent.length, 1);
  assert.equal(fs.statSync(f.file).mode & 0o777, 0o600);
});
test('group/user rate limits and age checks drop work without a send', async t => {
  const f = fixture(t);
  await f.bot.handle(f.event());
  await f.bot.handle(f.event(2));
  f.advance(2000);
  await f.bot.handle(f.event(3));
  await f.bot.handle(f.event(4, { user_id: 444 }));
  f.advance(5000);
  await f.bot.handle(f.event(5));
  await f.bot.handle(f.event(6, { time: Math.floor(f.now() / 1000) - 301 }));
  await f.bot.handle(f.event(7, { time: undefined }));
  assert.equal(f.sent.length, 3);
  assert.equal(f.bot.counts.limited, 2);
});
test('failed, invalid or timed-out sends are never retried, including after reload', async t => {
  for (const send of [async () => { throw Error('secret'); }, async () => ({ retcode: 1 }), () => new Promise(() => {})]) {
    const f = fixture(t, { send, timeoutMs: 5 });
    await f.bot.handle(f.event());
    assert.equal(JSON.parse(fs.readFileSync(f.file)).entries[0].status, 'uncertain');
    assert.equal(f.bot.status().fault, 'send_uncertain');
    const resumed = new CampusBot({ ...f.options, store: new ReceiptStore(f.file, { now: f.now }) });
    await resumed.handle(f.event());
    assert.equal(resumed.counts.duplicate, 1);
  }
});
test('stop or revoked config before dispatch prevents new sends', async t => {
  for (const revoke of [bot => bot.stop(), bot => bot.update({ enabled: false }), bot => bot.update({ enabled: true, allowedGroups: '123456', siteUrl: 'https://new.example.test/' })]) {
    const f = fixture(t);
    const pending = f.bot.handle(f.event());
    revoke(f.bot);
    await pending;
    assert.equal(f.sent.length, 0);
  }
});
test('corrupt, oversized, duplicate-key and symlink receipts fail closed', t => {
  const f = fixture(t);
  for (const content of ['bad json', ' '.repeat(2 * 1024 * 1024 + 1), JSON.stringify({ schema: 'campusos.qq-receipts/v1', entries: [{ key: 'a'.repeat(64), at: f.now(), status: 'sent' }, { key: 'a'.repeat(64), at: f.now(), status: 'sent' }] })]) {
    fs.writeFileSync(f.file, content);
    assert.throws(() => new ReceiptStore(f.file, { now: f.now }));
  }
  fs.unlinkSync(f.file);
  fs.symlinkSync(path.join(path.dirname(f.file), 'missing.json'), f.file);
  assert.throws(() => new ReceiptStore(f.file));
});
test('full store or persistence failure prevents sends; expired receipts can be pruned', async t => {
  const f = fixture(t);
  const small = new ReceiptStore(f.file, { now: f.now, maxEntries: 1 });
  small.claim(receiptKey({ self: '3', group: '1', messageId: '1' }));
  const bot = new CampusBot({ ...f.options, store: small });
  await bot.handle(f.event());
  assert.equal(bot.fault, 'receipt_unavailable');
  assert.equal(f.sent.length, 0);
  f.advance(24 * 60 * 60 * 1000 + 1);
  assert.equal(small.claim('b'.repeat(64)), true);
  assert.equal(small.entries.size, 1);
  const parent = path.join(path.dirname(f.file), 'block');
  const broken = new ReceiptStore(path.join(parent, 'receipts.json'), { now: f.now });
  fs.writeFileSync(parent, 'not a directory');
  const failing = new CampusBot({ ...f.options, store: broken });
  await failing.handle(f.event(2));
  assert.equal(failing.fault, 'receipt_unavailable');
  assert.equal(f.sent.length, 0);
});
