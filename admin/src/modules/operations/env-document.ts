// Literal dotenv parsing only; this module never evaluates substitutions.
export function parseEnvDocument(text: string): Record<string, string> {
  if (new TextEncoder().encode(text).length > 256 * 1024 || text.includes('\0')) throw new Error('配置文件不能超过 256 KiB，且不能包含空字符');
  const values: Record<string, string> = Object.create(null);
  for (const [index, raw] of text.split('\n').entries()) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const assignment = line.replace(/^export /, '');
    const equal = assignment.indexOf('=');
    const key = assignment.slice(0, equal).trim();
    try {
      if (equal < 0 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || Object.prototype.hasOwnProperty.call(values, key)) throw new Error();
      values[key] = literal(assignment.slice(equal + 1).trim());
      if (/[\r\n\0]/.test(values[key])) throw new Error();
    } catch { throw new Error(`配置文件第 ${index + 1} 行格式不正确，请使用单行 KEY=value，避免重复项和变量插值`); }
  }
  return values;
}
function literal(value: string): string {
  const quote = value[0];
  if (quote === "'" || quote === '"') {
    for (let i = 1; i < value.length; i++) {
      if (value[i] === '\\' && (quote === '"' || value[i + 1] === "'")) { i++; continue; }
      if (value[i] !== quote) continue;
      const tail = value.slice(i + 1).trim();
      if (tail && !tail.startsWith('#')) throw new Error();
      if (quote === "'") return value.slice(1, i).replace(/\\'/g, "'");
      const decoded: string = JSON.parse(value.slice(0, i + 1));
      if (/\$(?!\$)/.test(decoded.replace(/\$\$/g, ''))) throw new Error();
      return decoded.replace(/\$\$/g, '$');
    }
    throw new Error();
  }
  if (/[$`\\"]/.test(value)) throw new Error();
  const comment = value.indexOf(' #');
  return comment < 0 ? value : value.slice(0, comment).trim();
}
export function quoteEnvValue(value: string): string {
  return value.endsWith('\\') ? JSON.stringify(value).replace(/\$/g, '$$$$') : "'" + value.replace(/'/g, "\\'") + "'";
}
export function patchEnvDocument(text: string, changes: Record<string, string>): string {
  parseEnvDocument(text);
  const remaining = new Map(Object.entries(changes));
  const lines = text.split('\n').map(line => {
    const match = line.match(/^\s*(?:export )?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$/);
    if (!match || !remaining.has(match[1])) return line;
    const value = remaining.get(match[1])!; remaining.delete(match[1]);
    let quote = '', comment = '';
    for (let i = 0; i < match[2].length; i++) {
      const char = match[2][i];
      if (quote) { if (char === '\\') i++; else if (char === quote) quote = ''; }
      else if (char === "'" || char === '"') quote = char;
      else if (char === '#' && (i === 0 || /[\s'"]/.test(match[2][i - 1]))) { comment = ' ' + match[2].slice(i); break; }
    }
    return match[1] + '=' + quoteEnvValue(value) + comment;
  });
  if (remaining.size) { if (lines[lines.length - 1] === '') lines.pop(); for (const [key, value] of remaining) lines.push(key + '=' + quoteEnvValue(value)); }
  return lines.join('\n') + (remaining.size ? '\n' : '');
}
export const booleanEnvValue = (value: string) => ['true', '1', 'yes', 'on'].includes(value) ? 'true' : ['false', '0', 'no', 'off'].includes(value) ? 'false' : value;
