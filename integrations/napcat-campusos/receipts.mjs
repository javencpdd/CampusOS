import fs from 'node:fs';
import path from 'node:path';
import { createHash, randomBytes } from 'node:crypto';

const TTL = 24 * 60 * 60 * 1000;
const MAX_ENTRIES = 10000;
const MAX_BYTES = 2 * 1024 * 1024;
export const receiptKey = input => createHash('sha256').update([input.self, input.group, input.messageId].join(':')).digest('hex');

// Synchronous claims make checking + persisting indivisible in this Node process.
// A pending/uncertain claim is deliberately never retried.
export class ReceiptStore {
  constructor(file, { now = Date.now, maxEntries = MAX_ENTRIES } = {}) {
    this.file = file;
    this.now = now;
    this.maxEntries = maxEntries;
    this.entries = new Map();
    this.failed = false;
    try {
      const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
      try {
        if (fs.fstatSync(fd).size > MAX_BYTES) throw Error('size');
        const data = JSON.parse(fs.readFileSync(fd, 'utf8'));
        if (data.schema !== 'campusos.qq-receipts/v1' || !Array.isArray(data.entries) || data.entries.length > maxEntries) throw Error('schema');
        for (const item of data.entries) {
          if (!/^[a-f0-9]{64}$/.test(item.key) || !Number.isSafeInteger(item.at) || item.at < 0 ||
              item.at > now() + 60000 || !['pending', 'sent', 'uncertain'].includes(item.status) || this.entries.has(item.key)) throw Error('entry');
          this.entries.set(item.key, { at: item.at, status: item.status });
        }
      } finally { fs.closeSync(fd); }
    } catch (error) {
      if (error.code !== 'ENOENT') { this.failed = true; throw Error('消息回执损坏或不可读；已停止回复'); }
    }
  }

  has(key) { return this.entries.has(key) && this.entries.get(key).at >= this.now() - TTL; }

  persist() {
    const temp = `${this.file}.${randomBytes(8).toString('hex')}.tmp`;
    let fd;
    try {
      fs.mkdirSync(path.dirname(this.file), { recursive: true, mode: 0o700 });
      fd = fs.openSync(temp, 'wx', 0o600);
      fs.writeFileSync(fd, JSON.stringify({ schema: 'campusos.qq-receipts/v1', entries: [...this.entries].map(([key, value]) => ({ key, ...value })) }));
      fs.fsyncSync(fd);
      fs.closeSync(fd); fd = undefined;
      fs.renameSync(temp, this.file);
      const directory = fs.openSync(path.dirname(this.file), 'r');
      try { fs.fsyncSync(directory); } finally { fs.closeSync(directory); }
    } catch {
      this.failed = true;
      throw Error('消息回执写入失败；已停止回复');
    } finally {
      if (fd !== undefined) fs.closeSync(fd);
      try { fs.unlinkSync(temp); } catch { /* already renamed or never created */ }
    }
  }

  claim(key) {
    if (this.failed) throw Error('消息回执不可用');
    if (this.has(key)) return false;
    for (const [existing, entry] of this.entries) if (entry.at < this.now() - TTL) this.entries.delete(existing);
    if (this.entries.size >= this.maxEntries) throw Error('消息回执已满；已停止回复');
    this.entries.set(key, { at: this.now(), status: 'pending' });
    this.persist();
    return true;
  }

  finish(key, status) {
    if (this.failed || !this.entries.has(key) || !['sent', 'uncertain'].includes(status)) throw Error('消息回执不可用');
    this.entries.get(key).status = status;
    this.persist();
  }
}
