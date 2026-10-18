// Bounded JSON for configuration editors: decimal lexemes never cross a
// JavaScript floating-point conversion. No eval, custom reviver or $ref code.
const syntax = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$/;
function decimal(raw: string) {
  if (!syntax.test(raw) || raw.length > 65536) throw new Error("数字格式无效");
  const negative = raw.startsWith("-");
  const [mantissa, exponent = "0"] = raw
    .replace(/^-/, "")
    .toLowerCase()
    .split("e");
  let scale = Number(exponent) - (mantissa.split(".")[1]?.length || 0);
  if (!Number.isSafeInteger(scale) || Math.abs(Number(exponent)) > 131072)
    throw new Error("数字超出持久化范围");
  let digits = mantissa.replace(".", "").replace(/^0+/, "");
  if (!digits) return { sign: 0, digits: "0", scale: 0 };
  const trimmed = digits.replace(/0+$/, "");
  scale += digits.length - trimmed.length;
  digits = trimmed;
  if (scale < -16383 || digits.length + scale > 131072)
    throw new Error("数字超出持久化范围");
  return { sign: negative ? -1 : 1, digits, scale };
}
export class ExactJSONNumber {
  readonly literal: string;
  constructor(literal: string) {
    decimal(literal);
    this.literal = literal;
    Object.freeze(this);
  }
  toString() {
    return this.literal;
  }
}
export const exactNumberText = (value: unknown): string => {
  if (value instanceof ExactJSONNumber) return value.literal;
  if (
    typeof value === "number" &&
    Number.isFinite(value) &&
    (!Number.isInteger(value) || Number.isSafeInteger(value))
  )
    return String(value);
  if (typeof value === "string") {
    decimal(value);
    return value;
  }
  throw new Error("数字格式无效");
};
export function compareExactNumbers(a: unknown, b: unknown): number {
  const x = decimal(exactNumberText(a)),
    y = decimal(exactNumberText(b));
  if (x.sign !== y.sign) return Math.sign(x.sign - y.sign);
  if (!x.sign) return 0;
  const position = x.digits.length + x.scale - y.digits.length - y.scale;
  if (position) return Math.sign(position) * x.sign;
  const width = Math.max(x.digits.length, y.digits.length);
  const left = x.digits.padEnd(width, "0"),
    right = y.digits.padEnd(width, "0");
  return left === right ? 0 : (left > right ? 1 : -1) * x.sign;
}
export const exactInteger = (value: unknown) =>
  decimal(exactNumberText(value)).scale >= 0;

export function parseExactJSON(text: string): any {
  if (
    typeof text !== "string" ||
    new TextEncoder().encode(text).length > 8 * 1024 * 1024
  )
    throw new Error("JSON超出大小限制");
  let at = 0;
  const whitespace = () => {
    while (/[\x20\x09\x0a\x0d]/.test(text[at] || "x")) at++;
  };
  function string(): string {
    const start = at++;
    while (at < text.length) {
      const ch = text[at++];
      if (ch === "\\") {
        at++;
        continue;
      }
      if (ch === '"') return JSON.parse(text.slice(start, at));
    }
    throw new Error("JSON字符串未闭合");
  }
  function value(depth: number): any {
    if (depth > 64) throw new Error("JSON嵌套过深");
    whitespace();
    const ch = text[at];
    if (ch === '"') return string();
    if (ch === "{" || ch === "[") {
      at++;
      whitespace();
      const array = ch === "[",
        close = array ? "]" : "}";
      const result: any = array ? [] : Object.create(null);
      if (text[at] === close) {
        at++;
        return result;
      }
      for (;;) {
        whitespace();
        let key = "";
        if (!array) {
          if (text[at] !== '"') throw new Error("JSON字段无效");
          key = string();
          if (Object.prototype.hasOwnProperty.call(result, key))
            throw new Error("JSON字段重复");
          whitespace();
          if (text[at++] !== ":") throw new Error("JSON字段无效");
        }
        const item = value(depth + 1);
        if (array) result.push(item);
        else result[key] = item;
        whitespace();
        if (text[at] === close) {
          at++;
          return result;
        }
        if (text[at++] !== ",") throw new Error("JSON对象无效");
      }
    }
    for (const [literal, item] of [
      ["true", true],
      ["false", false],
      ["null", null],
    ] as const)
      if (text.startsWith(literal, at)) {
        at += literal.length;
        return item;
      }
    const match = text
      .slice(at)
      .match(/^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/);
    if (!match) throw new Error("JSON值无效");
    at += match[0].length;
    return new ExactJSONNumber(match[0]);
  }
  const result = value(0);
  whitespace();
  if (at !== text.length) throw new Error("JSON存在多余内容");
  return result;
}
export function stringifyExactJSON(value: unknown): string {
  const seen = new Set<object>();
  function write(v: any, depth: number): string {
    if (depth > 64) throw new Error("JSON嵌套过深");
    if (v instanceof ExactJSONNumber) return v.literal;
    if (v === null || typeof v === "string" || typeof v === "boolean")
      return JSON.stringify(v);
    if (typeof v === "number") {
      if (
        !Number.isFinite(v) ||
        (Number.isInteger(v) && !Number.isSafeInteger(v))
      )
        throw new Error("数字精度不可验证");
      return JSON.stringify(v);
    }
    if (typeof v !== "object" || seen.has(v)) throw new Error("JSON值无效");
    seen.add(v);
    const out = Array.isArray(v)
      ? `[${Array.from(v, (x) => write(x, depth + 1)).join(",")}]`
      : `{${Object.keys(v)
          .map((k) => `${JSON.stringify(k)}:${write(v[k], depth + 1)}`)
          .join(",")}}`;
    seen.delete(v);
    return out;
  }
  const out = write(value, 0);
  if (new TextEncoder().encode(out).length > 8 * 1024 * 1024)
    throw new Error("JSON超出大小限制");
  return out;
}
export function parseV5Envelope(raw: string): any {
  const out = parseExactJSON(raw);
  if (out && typeof out === "object" && out.code instanceof ExactJSONNumber) {
    const code = Number(out.code.literal);
    if (!Number.isSafeInteger(code)) throw new Error("响应状态无效");
    out.code = code;
  }
  return out;
}
