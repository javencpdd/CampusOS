import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { validateSettings, OneBotConnection } from '../onebot.mjs';
import { startRunner } from '../runner.mjs';

const raw = () => ({ bot: { enabled: true, allowedGroups: '123456', siteUrl: 'http://example.test/' }, connection: { url: 'ws://127.0.0.1:6700/', accessToken: 't'.repeat(48) } });
class FakeSocket extends EventTarget {
  static sockets = [];
  constructor(url) { super(); this.url = new URL(url); this.sent = []; FakeSocket.sockets.push(this); }
  send(text) { this.sent.push(JSON.parse(text)); }
  close() { this.dispatchEvent(new Event('close')); }
  open() { this.dispatchEvent(new Event('open')); }
  receive(value) { this.dispatchEvent(new MessageEvent('message', { data: typeof value === 'string' ? value : JSON.stringify(value) })); }
}
const tick = () => new Promise(resolve => setImmediate(resolve));
function fixture(t, extra = {}) {
  const events = [], states = [];
  const connection = new OneBotConnection({ settings: validateSettings(raw()), WebSocketClass: FakeSocket, onEvent: event => events.push(event), onState: state => states.push(state), ...extra });
  t.after(() => connection.stop());
  connection.start();
  return { connection, socket: FakeSocket.sockets.at(-1), events, states };
}

test('connection settings enforce loopback, token shape and explicit group configuration', () => {
  for (const url of ['ws://example.test:6700/', 'ws://0.0.0.0:6700/', 'ws://127.0.0.1:6700/?token=x', 'file:///tmp', 'ws://user:pass@127.0.0.1:6700/']) assert.throws(() => validateSettings({ ...raw(), connection: { ...raw().connection, url } }));
  assert.throws(() => validateSettings({ ...raw(), connection: { ...raw().connection, accessToken: 'short' } }));
  assert.throws(() => validateSettings({ ...raw(), unknown: true }));
});
test('OneBot action is scoped pure text, correlated by echo, and malformed frames ignored', async t => {
  const f = fixture(t);
  f.socket.open();
  assert.equal(f.socket.url.searchParams.get('access_token'), raw().connection.accessToken);
  await assert.rejects(f.connection.send('999', 'x'));
  const result = f.connection.send('123456', 'hello [CQ:at,qq=all]');
  const action = f.socket.sent[0];
  assert.equal(action.action, 'send_group_msg');
  assert.deepEqual(action.params.message, [{ type: 'text', data: { text: 'hello [CQ:at,qq=all]' } }]);
  await assert.rejects(f.connection.send('123456', 'second'));
  f.socket.receive('bad json'); f.socket.receive(null); f.socket.receive(' '.repeat(65537));
  f.socket.receive({ status: 'ok', retcode: 0, data: { message_id: 17 }, echo: 'wrong' });
  assert.equal(f.connection.pending.size, 1);
  f.socket.receive({ status: 'ok', retcode: 0, data: { message_id: 17 }, echo: action.echo });
  assert.deepEqual(await result, { message_id: 17 });
  f.socket.receive({ post_type: 'message', message_type: 'group' });
  await tick(); assert.equal(f.events.length, 1);
  assert.doesNotMatch(JSON.stringify(f.states), /tttt|access_token|127.0.0.1/);
});
test('negative response, action timeout and disconnect reject once without resending', async t => {
  for (const mode of ['negative', 'timeout', 'disconnect']) {
    const f = fixture(t, { actionTimeoutMs: 5, reconnectMs: 10000 }); f.socket.open();
    const promise = f.connection.send('123456', 'once');
    const expected = assert.rejects(promise);
    if (mode === 'negative') f.socket.receive({ echo: f.socket.sent[0].echo, status: 'failed', retcode: 100, data: null });
    if (mode === 'disconnect') f.socket.close();
    await expected;
    assert.equal(f.socket.sent.length, 1);
    assert.equal(f.connection.pending.size, 0);
  }
});
test('disconnect reconnects, while stop cancels reconnect and stale events', async t => {
  const f = fixture(t, { reconnectMs: 5 });
  f.socket.open(); f.socket.close();
  await new Promise(resolve => setTimeout(resolve, 15));
  const next = FakeSocket.sockets.at(-1); assert.notEqual(next, f.socket);
  next.open(); f.socket.receive({ post_type: 'message' }); await tick(); assert.equal(f.events.length, 0);
  next.receive({ post_type: 'message' });
  f.connection.stop();
  next.receive({ post_type: 'message' }); await tick(); assert.equal(f.events.length, 0);
});
test('handshake and idle timeouts close stale sockets without sending', async t => {
  for (const opened of [false, true]) {
    const f = fixture(t, { connectTimeoutMs: 5, idleTimeoutMs: 5, reconnectMs: 10000 });
    if (opened) f.socket.open();
    await new Promise(resolve => setTimeout(resolve, 15));
    assert.ok(f.states.includes(opened ? 'connection_idle' : 'connection_timeout'));
    assert.equal(f.connection.connected, false);
    assert.equal(f.socket.sent.length, 0);
  }
});
test('runner enforces config permissions, one instance, real catalog, receipts and graceful stop', async t => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'campusos-onebot-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const configPath = path.join(dir, 'config.json'), dataPath = path.join(dir, 'data');
  fs.writeFileSync(configPath, JSON.stringify(raw()), { mode: 0o600 });
  const runner = startRunner({ configPath, dataPath, WebSocketClass: FakeSocket });
  t.after(() => runner.stop());
  assert.throws(() => startRunner({ configPath, dataPath, WebSocketClass: FakeSocket }));
  const socket = FakeSocket.sockets.at(-1); socket.open();
  const event = { post_type: 'message', message_type: 'group', group_id: 123456, user_id: 222, self_id: 333, message_id: 1, time: Math.floor(Date.now() / 1000), message: [{ type: 'text', data: { text: '/campus 搜索 插件' } }] };
  socket.receive({ ...event, group_id: 999 }); socket.receive({ ...event, message_type: 'private' }); await tick(); assert.equal(socket.sent.length, 0);
  socket.receive(event); await tick();
  assert.equal(socket.sent.length, 1);
  assert.match(socket.sent[0].params.message[0].data.text, /module-plugin-resource-boundaries.html/);
  socket.receive({ echo: socket.sent[0].echo, status: 'ok', retcode: 0, data: { message_id: 23 } }); await tick();
  assert.equal(runner.status().sent, 1);
  const status = fs.readFileSync(path.join(dataPath, 'status.json'), 'utf8'); assert.doesNotMatch(status, /tttt|accessToken|123456|222/);
  runner.stop(); assert.equal(fs.existsSync(path.join(dataPath, 'runner.lock')), false);
  assert.equal(JSON.parse(fs.readFileSync(path.join(dataPath, 'status.json'))).connection, 'stopped');
  assert.equal(JSON.parse(fs.readFileSync(path.join(dataPath, 'status.json'))).enabled, false);
  const reloaded = startRunner({ configPath, dataPath, WebSocketClass: FakeSocket }); t.after(() => reloaded.stop());
  const second = FakeSocket.sockets.at(-1); second.open(); second.receive(event); await tick(); assert.equal(second.sent.length, 0);
  reloaded.stop(); fs.chmodSync(configPath, 0o644);
  assert.throws(() => startRunner({ configPath, dataPath, WebSocketClass: FakeSocket }));
});
test('reproducible package excludes actual credentials and local runtime data', t => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'campusos-pack-')); t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const script = fileURLToPath(new URL('../tools/pack.py', import.meta.url));
  for (const name of ['a.zip', 'b.zip']) assert.equal(spawnSync('python3', [script, '--output', path.join(dir, name)]).status, 0);
  assert.deepEqual(fs.readFileSync(path.join(dir, 'a.zip')), fs.readFileSync(path.join(dir, 'b.zip')));
  const listing = spawnSync('python3', ['-c', 'import zipfile,sys,json;print(json.dumps(zipfile.ZipFile(sys.argv[1]).namelist()))', path.join(dir, 'a.zip')], { encoding: 'utf8' });
  const names = JSON.parse(listing.stdout);
  assert.equal(names.length, 11); assert.ok(names.includes('LICENSE')); assert.ok(names.includes('runner.mjs')); assert.ok(names.includes('config.example.json'));
  assert.ok(names.every(name => !/receipts.json|status.json|config.json|token/i.test(name)));
});
