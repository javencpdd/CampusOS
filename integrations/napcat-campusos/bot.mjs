import { parseEvent, reply, validateConfig } from './core.mjs';
import { receiptKey } from './receipts.mjs';

export class CampusBot {
  constructor({ config, documents, store, send, now = Date.now, timeoutMs = 8000, warn = () => {} }) {
    Object.assign(this, { documents, store, send, now, timeoutMs, warn });
    this.config = validateConfig(config);
    this.cooldowns = new Map();
    this.stopped = false;
    this.busy = false;
    this.fault = null;
    this.counts = { sent: 0, duplicate: 0, limited: 0, failed: 0 };
  }

  update(raw) { this.config = validateConfig(raw); }
  stop() { this.stopped = true; }
  status() { return { enabled: this.config.enabled && !this.stopped && !this.fault, fault: this.fault, catalogEntries: this.documents.length, ...this.counts }; }

  async handle(event) {
    if (this.stopped || this.fault) return;
    const config = this.config;
    const input = parseEvent(event, config);
    if (!input || !Number.isSafeInteger(event.time) || event.time * 1000 < this.now() - 300000 || event.time * 1000 > this.now() + 60000) return;
    const key = receiptKey(input);
    if (this.store.has(key)) { this.counts.duplicate++; return; }
    const keys = [`g:${input.group}`, `u:${input.group}:${input.user}`];
    if (this.busy || keys.some(item => (this.cooldowns.get(item) || 0) > this.now())) { this.counts.limited++; return; }
    // Pruning + a hard bound prevents group-member churn from growing memory.
    for (const [item, expires] of this.cooldowns) if (expires <= this.now()) this.cooldowns.delete(item);
    if (this.cooldowns.size >= 1024) { this.counts.limited++; return; }
    const text = reply(input.command, this.config, this.documents);
    if (text.length > 2000) { this.fault = 'reply_too_large'; return; }
    try {
      if (!this.store.claim(key)) { this.counts.duplicate++; return; }
    } catch { this.fault = 'receipt_unavailable'; this.warn('消息回执不可用，已停止回复'); return; }
    this.cooldowns.set(keys[0], this.now() + 1500);
    this.cooldowns.set(keys[1], this.now() + 5000);
    this.busy = true;
    let timer;
    try {
      const result = await Promise.race([
        Promise.resolve().then(() => {
          // The configuration may have been revoked before this microtask starts.
          if (this.stopped || this.config !== config || !this.config.enabled) throw Error('stopped');
          return this.send(input.group, text);
        }),
        new Promise((_, reject) => { timer = setTimeout(() => reject(Error('timeout')), this.timeoutMs); })
      ]);
      if (!result || result.retcode !== undefined && result.retcode !== 0 || !/^-?[1-9]\d{0,19}$/.test(String(result.message_id ?? result.data?.message_id ?? ''))) throw Error('uncertain');
      this.store.finish(key, 'sent');
      this.counts.sent++;
    } catch {
      this.counts.failed++;
      this.fault = 'send_uncertain';
      try { this.store.finish(key, 'uncertain'); } catch { this.fault = 'receipt_unavailable'; }
      this.warn('发送结果不确定或回执不可用，已停止回复；不自动重发');
    } finally { clearTimeout(timer); this.busy = false; }
  }
}
