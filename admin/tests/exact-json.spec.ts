import { expect, it } from "vitest";
import {
  parseExactJSON,
  stringifyExactJSON,
  ExactJSONNumber,
  compareExactNumbers,
  exactInteger,
  parseV5Envelope,
} from "../../sdk/typescript/src/exact-json";
it("round-trips BIGINT and close decimals without rounding or exponent allocation", () => {
  const raw =
    '{"large":90071992547409931234,"small":0.10000000000000001,"exponent":1e131071,"negative":-1.00}';
  expect(stringifyExactJSON(parseExactJSON(raw))).toBe(raw);
  expect(
    compareExactNumbers(
      new ExactJSONNumber("0.10000000000000001"),
      new ExactJSONNumber("0.1"),
    ),
  ).toBe(1);
  expect(compareExactNumbers("1e131071", "9e131070")).toBe(1);
  expect(compareExactNumbers("-1.0", "-1")).toBe(0);
  expect(exactInteger("100e-2")).toBe(true);
  expect(exactInteger("1.1")).toBe(false);
  expect(
    parseV5Envelope(
      '{"code":0,"data":{"revision":"9007199254740993","values":{"n":9007199254740993}}}',
    ).code,
  ).toBe(0);
});
it.each([
  '{"x":1,"x":2}',
  '{"x":01}',
  "[1,]",
  "{}{}",
  '"bad\nstring"',
  "1e999999",
  "1e-16384",
])("rejects malformed or out-of-range input: %s", (raw) =>
  expect(() => parseExactJSON(raw)).toThrow(),
);
it("does not invoke prototypes or permit unsafe floating integers", () => {
  const obj = parseExactJSON('{"__proto__":{"polluted":true}}');
  expect(({} as any).polluted).toBeUndefined();
  expect(Object.getPrototypeOf(obj)).toBeNull();
  expect(() => stringifyExactJSON({ n: 9007199254740992 })).toThrow();
});
