import { randomBytes } from 'node:crypto';
import { validateConfig } from './core.mjs';

export function validateSettings(raw) {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw) || Object.keys(raw).some(key => !['bot', 'connection'].includes(key))) throw Error('配置格式无效');
  const connection = raw.connection;
  if (!connection || typeof connection !== 'object' || Object.keys(connection).some(key => !['url', 'accessToken'].includes(key)) ||
      typeof connection.url !== 'string' || connection.url.length > 100 || typeof connection.accessToken !== 'string' ||
      !/^[A-Za-z0-9_-]{32,128}$/.test(connection.accessToken)) throw Error('OneBot 配置格式无效');
  let url;
  try { url = new URL(connection.url); } catch { throw Error('OneBot 地址无效'); }
  if (url.protocol !== 'ws:' || !['127.0.0.1', '[::1]'].includes(url.hostname) || !url.port || url.pathname !== '/' || url.username || url.password || url.search || url.hash) throw Error('OneBot 只允许显式端口的回环 WebSocket 地址');
  return { bot: validateConfig(raw.bot), connection: { url: url.href, accessToken: connection.accessToken } };
}

// The token is added only in memory. Never log URL objects or transport errors.
export class OneBotConnection {
  constructor({ settings, WebSocketClass = globalThis.WebSocket, onEvent, onState = () => {}, actionTimeoutMs = 6000, reconnectMs = 3000, connectTimeoutMs = 10000, idleTimeoutMs = 90000 }) {
    if (typeof WebSocketClass !== 'function') throw Error('需要 Node.js WebSocket；Node 20 请添加 --experimental-websocket');
    Object.assign(this, { settings, WebSocketClass, onEvent, onState, actionTimeoutMs, reconnectMs, connectTimeoutMs, idleTimeoutMs });
    this.pending = new Map();
    this.socket = null;
    this.stopped = true;
    this.connected = false;
    this.generation = 0;
  }

  start() { if (!this.stopped) return; this.stopped = false; this.connect(); }
  connect() {
    if (this.stopped) return;
    const generation = ++this.generation;
    const url = new URL(this.settings.connection.url);
    url.searchParams.set('access_token', this.settings.connection.accessToken);
    let socket;
    try { socket = new this.WebSocketClass(url); }
    catch { this.onState('connection_failed'); this.schedule(); return; }
    this.socket = socket;
    this.handshake = setTimeout(() => { this.onState('connection_timeout'); socket.close(); }, this.connectTimeoutMs);
    const resetIdle = () => {
      clearTimeout(this.idle);
      this.idle = setTimeout(() => { this.onState('connection_idle'); socket.close(); }, this.idleTimeoutMs);
    };
    socket.addEventListener('open', () => {
      if (this.stopped || generation !== this.generation) { socket.close(); return; }
      clearTimeout(this.handshake); resetIdle();
      this.connected = true; this.onState('connected');
    });
    socket.addEventListener('message', event => {
      if (this.stopped || generation !== this.generation || typeof event.data !== 'string' || Buffer.byteLength(event.data) > 65536) return;
      resetIdle();
      let value;
      try { value = JSON.parse(event.data); } catch { return; }
      if (!value || typeof value !== 'object' || Array.isArray(value)) return;
      if (typeof value.echo === 'string' && this.pending.has(value.echo)) {
        const pending = this.pending.get(value.echo);
        this.pending.delete(value.echo); clearTimeout(pending.timer);
        if (value.status === 'ok' && value.retcode === 0 && value.data && typeof value.data === 'object') pending.resolve(value.data);
        else pending.reject(Error('OneBot 动作结果不确定'));
      } else if (value.post_type === 'message') {
        Promise.resolve().then(() => {
          if (this.stopped || generation !== this.generation) return;
          return this.onEvent(value);
        }).catch(() => this.onState('event_failed'));
      }
    });
    socket.addEventListener('error', () => { this.onState('connection_error'); socket.close(); });
    socket.addEventListener('close', () => {
      if (generation !== this.generation) return;
      clearTimeout(this.handshake); clearTimeout(this.idle);
      this.connected = false;
      this.rejectPending();
      this.onState(this.stopped ? 'stopped' : 'disconnected');
      this.schedule();
    });
  }

  schedule() {
    clearTimeout(this.retry);
    if (!this.stopped) this.retry = setTimeout(() => this.connect(), this.reconnectMs);
  }
  rejectPending() {
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(Error('OneBot 连接已断开')); }
    this.pending.clear();
  }
  send(group, text) {
    if (!this.connected || this.stopped || this.pending.size || !this.settings.bot.groups.includes(group) || typeof text !== 'string' || text.length > 2000) return Promise.reject(Error('OneBot 发送不可用'));
    const echo = randomBytes(16).toString('hex');
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(echo); reject(Error('OneBot 动作超时')); }, this.actionTimeoutMs);
      this.pending.set(echo, { resolve, reject, timer });
      try { this.socket.send(JSON.stringify({ action: 'send_group_msg', params: { group_id: group, message: [{ type: 'text', data: { text } }] }, echo })); }
      catch { clearTimeout(timer); this.pending.delete(echo); reject(Error('OneBot 发送失败')); }
    });
  }
  stop() {
    this.stopped = true; this.connected = false; ++this.generation;
    clearTimeout(this.retry); clearTimeout(this.handshake); clearTimeout(this.idle); this.rejectPending();
    try { this.socket?.close(); } catch { /* already closed */ }
    this.onState('stopped');
  }
}
