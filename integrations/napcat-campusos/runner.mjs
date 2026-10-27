import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { validateSettings, OneBotConnection } from './onebot.mjs';
import { validateCatalog, parseEvent } from './core.mjs';
import { ReceiptStore } from './receipts.mjs';
import { CampusBot } from './bot.mjs';

export function startRunner({ configPath, dataPath, WebSocketClass = globalThis.WebSocket }) {
  let lock, connection, bot;
  let locked = false, closed = false;
  const notify = state => {
    if (closed) return;
    const status = { updatedAt: new Date().toISOString(), connection: state, ...bot?.status() };
    const temp = path.join(dataPath, 'status.json.tmp');
    try { fs.writeFileSync(temp, JSON.stringify(status, null, 2) + '\n', { mode: 0o600 }); fs.renameSync(temp, path.join(dataPath, 'status.json')); }
    catch { stop(); throw Error('状态写入失败，已停止'); }
  };
  const stop = () => {
    if (closed) return;
    closed = true; bot?.stop(); connection?.stop();
    if (locked && bot) {
      try {
        fs.writeFileSync(path.join(dataPath, 'status.json.tmp'), JSON.stringify({ updatedAt: new Date().toISOString(), connection: 'stopped', ...bot.status() }, null, 2) + '\n', { mode: 0o600 });
        fs.renameSync(path.join(dataPath, 'status.json.tmp'), path.join(dataPath, 'status.json'));
      } catch { /* stopping remains possible when storage is unavailable */ }
    }
    if (locked) { try { fs.closeSync(lock); fs.unlinkSync(path.join(dataPath, 'runner.lock')); } catch { /* operator may have removed it */ } }
  };
  try {
    const fd = fs.openSync(configPath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
    let raw;
    try {
      const stat = fs.fstatSync(fd);
      if (stat.size > 16384 || stat.mode & 0o077) throw Error('配置文件须仅本人可读写');
      raw = JSON.parse(fs.readFileSync(fd, 'utf8'));
    } finally { fs.closeSync(fd); }
    const settings = validateSettings(raw);
    fs.mkdirSync(dataPath, { recursive: true, mode: 0o700 });
    lock = fs.openSync(path.join(dataPath, 'runner.lock'), 'wx', 0o600); locked = true;
    fs.writeFileSync(lock, String(process.pid) + '\n');
    const catalog = JSON.parse(fs.readFileSync(new URL('./catalog.json', import.meta.url), 'utf8'));
    const store = new ReceiptStore(path.join(dataPath, 'receipts.json'));
    bot = new CampusBot({ config: raw.bot, documents: validateCatalog(catalog), store,
      send: (group, text) => connection.send(group, text),
      warn: () => process.stderr.write('文档助手已停止回复，请检查本地状态文件。\n')
    });
    connection = new OneBotConnection({ settings, WebSocketClass,
      onEvent: async event => {
        if (!parseEvent(event, settings.bot)) return;
        await bot.handle(event);
        notify(connection.connected ? 'connected' : 'disconnected');
      },
      onState: notify
    });
    notify('starting'); connection.start();
    return { stop, status: () => ({ connected: connection.connected, ...bot.status() }) };
  } catch {
    stop();
    throw Error('文档助手启动失败；请检查 Node WebSocket、私有配置、目录权限及单实例锁');
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const { values } = parseArgs({ options: { config: { type: 'string' }, 'data-dir': { type: 'string', default: path.join(os.homedir(), '.local/share/campusos-qqbot') } } });
    if (!values.config) throw Error('missing config');
    const runner = startRunner({ configPath: path.resolve(values.config), dataPath: path.resolve(values['data-dir']) });
    for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, () => { runner.stop(); process.exit(0); });
    process.stdout.write('CampusOS QQ 文档助手已启动；状态保存在插件数据目录。\n');
  } catch { process.stderr.write('启动失败：需要 --config 私有配置文件，以及可写的 --data-dir（不能与其他实例共用）。\n'); process.exitCode = 1; }
}
