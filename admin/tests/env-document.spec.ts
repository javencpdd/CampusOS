import { describe, expect, it } from 'vitest';
import { parseEnvDocument, patchEnvDocument, quoteEnvValue } from '../src/modules/operations/env-document';

describe('literal env document editor', () => {
  it('preserves comments and unrelated values while synchronizing form changes', () => {
    const raw = "# retained\nexport AI_ENABLED=false # setting\nOTHER='a$literal#value'\n";
    const patched = patchEnvDocument(raw, { AI_ENABLED: 'true', AI_TIMEOUT: '17s' });
    expect(patched).toContain("AI_ENABLED='true' # setting");
    expect(patched).toContain("OTHER='a$literal#value'");
    expect(parseEnvDocument(patched)).toMatchObject({ AI_ENABLED: 'true', AI_TIMEOUT: '17s', OTHER: 'a$literal#value' });
  });
  it('round trips literal credentials without interpolation or execution', () => {
    for (const value of ["$literal'ending\\", '中文值', "quote'and\\slash", '', 'normal$literal#value']) {
      expect(parseEnvDocument('VALUE=' + quoteEnvValue(value)).VALUE).toBe(value);
    }
  });
  it('rejects ambiguous syntax with a line number and no submitted values', () => {
    for (const raw of ['KEY=one\nKEY=two', 'KEY=${SHELL}', 'KEY="$SHELL"', 'echo oops', "KEY='unclosed", 'KEY="line\\nnext"']) {
      expect(() => parseEnvDocument(raw)).toThrow(/配置文件第 \d+ 行/);
    }
    expect(parseEnvDocument('__proto__=literal').__proto__).toBe('literal');
  });
});
