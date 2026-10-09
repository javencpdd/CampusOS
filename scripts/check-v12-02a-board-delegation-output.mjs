#!/usr/bin/env node
// Test tooling only: reuse the frozen G1 evaluator as an independent oracle.
// No runtime authorization code imports this file or evaluates JS policies.
import assert from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const read = (p) => JSON.parse(readFileSync(p, 'utf8'));
const clone = (v) => JSON.parse(JSON.stringify(v));
const contract = 'campusos.board-delegation/v1';
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read(resolve(root, 'docs/api/principal-context-v1.schema.json')));
const schema = read(resolve(root, 'docs/api/board-delegation-v1.schema.json'));
ajv.addSchema(schema);
const validRequest = ajv.compile({ $ref: schema.$id + '#/definitions/request' });
const validDecision = ajv.compile({ $ref: schema.$id + '#/definitions/decision' });
const frozen = readFileSync(resolve(root, 'scripts/check-v12-board-delegation-contract.mjs'), 'utf8');
const start = frozen.indexOf('const sameSubject = '), end = frozen.indexOf('const lines = [');
assert(start > 0 && end > start, 'G1 oracle layout changed; review the extractor');
const oracle = vm.runInNewContext(frozen.slice(start, end) + '\n({evaluate, consume});',
  { validRequest, validDecision, contract, isDeepStrictEqual, structuredClone }, { timeout: 1000 });
const corpus = read(resolve(root, 'sdk/typescript/tests/board-delegation-v1.fixtures.json'));
const args = process.argv.slice(2);
if (args.length === 2 && args[0] === '--generate') {
  const cases = corpus.cases.map((c) => {
    const out = oracle.evaluate(clone(c.request));
    assert.equal(out.error ?? out.reason, c.expected, 'frozen fixture expectation changed: ' + c.name);
    return { name: 'g1-' + c.name, input: clone(c.request), expected: out.error ?? (out.effect === 'allow' ? 'allow' : out.reason) };
  });
  assert.equal(cases.length, 54);
  const base = structuredClone(corpus.cases[0].request);
  function add(name, input) {
    const out = oracle.evaluate(clone(input));
    const expected = out.error ?? (out.effect === 'allow' ? 'allow' : out.reason);
    cases.push({ name, input: clone(input), expected });
  }
  // Independent subset truth table: action/board cross product and window/strength.
  let n = 0;
  for (const action of ['community.thread.take_down', 'community.post.delete'])
  for (const board of ['board-A', 'board-B'])
  for (const start of [999, 1000, 1100])
  for (const end of [1800, 2000, 2001])
  for (const strength of ['password', 'mfa']) {
    const r = structuredClone(base);
    Object.assign(r.candidate.grants[0], { action, board_id: board, not_before: start, expires_at: end, required_strength: strength });
    add('subset-' + n++, r);
  }
  assert.equal(n, 72);
  // Principal domain matrix: management and recipient states across every
  // structurally valid G1 principal context, including delegated plugin actors.
  const principals = read(resolve(root, 'sdk/typescript/tests/principal-context-v1.fixtures.json')).cases.filter((c) => c.valid).map((c) => c.context);
  assert.equal(principals.length, 10);
  n = 0;
  for (const principal of principals)
  for (const managementStatus of ['active', 'suspended', 'revoked'])
  for (const recipientStatus of ['active', 'suspended', 'deleted']) {
    const r = structuredClone(base);
    r.principal = clone(principal);
    const adminID = principal.actor.kind === 'admin' ? principal.actor.id : 'admin-1';
    r.management.actor = { kind: 'admin', id: adminID };
    r.management.status = managementStatus;
    r.bounds[0].actor = { kind: 'admin', id: adminID };
    r.recipient.status = recipientStatus;
    add('domain-' + n++, r);
  }
  assert.equal(n, 90);
  assert.equal(cases.length, 216);
  assert.equal(new Set(cases.map((c) => c.name)).size, cases.length);
  writeFileSync(args[1], JSON.stringify(cases, null, 2) + '\n');
  console.log(`Generated ${cases.length} G1 oracle cases: 54 fixtures + 72 subset combinations + 90 domain/status combinations`);
} else if (args.length === 3 && args[0] === '--verify') {
  const cases = read(args[1]), outputs = read(args[2]);
  assert.equal(cases.length, 216); assert.equal(outputs.length, cases.length);
  let allow = 0, deny = 0, errors = 0;
  for (let i = 0; i < cases.length; i++) {
    const c = cases[i], actual = outputs[i], expected = clone(oracle.evaluate(clone(c.input)));
    assert.equal(actual.name, c.name);
    assert.deepEqual(actual.input, c.input, 'input changed: ' + c.name);
    if (expected.error) { assert.equal(actual.error, expected.error, c.name); assert(!actual.decision, c.name); errors++; }
    else {
      assert(!actual.error, c.name); assert(validDecision(actual.decision), c.name);
      assert.deepEqual(actual.decision, expected, c.name);
      if (actual.decision.effect === 'allow') allow++; else deny++;
    }
  }
  console.log(`Native Go/G1 oracle: ${outputs.length} outputs passed (${allow} allow, ${deny} deny, ${errors} input rejections)`);
} else throw new Error('usage: --generate cases.json | --verify cases.json actual.json');
