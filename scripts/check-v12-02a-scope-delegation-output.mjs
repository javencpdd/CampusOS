#!/usr/bin/env node
// Test tooling only: reuse the frozen G1 evaluator as an independent oracle.
// No runtime authorization code imports this file or evaluates JS policies.
import assert from 'node:assert/strict';
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
const schema = read(resolve(root, 'docs/api/scope-delegation-v1.schema.json'));
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
assert(ajv.validateSchema(schema));
const valid = ajv.compile(schema);
const frozen = readFileSync(resolve(root, 'scripts/check-v12-scope-delegation-contract.mjs'), 'utf8');
const start = frozen.indexOf('const base='), end = frozen.indexOf('const results=[];');
assert(start > 0 && end > start, 'G1 oracle layout changed; review the extractor');
const oracle = vm.runInNewContext(frozen.slice(start, end) + '\n({check, cases, base, actionScopes});',
  { valid, structuredClone }, { timeout: 1000 });
const outcome = (input) => clone(oracle.check(clone(input))) ?? 'accepted';
const args = process.argv.slice(2);
if (args.length === 2 && args[0] === '--generate') {
  const cases = oracle.cases.map(([name, input, expected]) => {
    assert.equal(outcome(input), expected ?? 'accepted', 'frozen fixture expectation changed: ' + name);
    return { name: 'g1-' + name, input: clone(input), expected: expected ?? 'accepted' };
  });
  assert.equal(cases.length, 23);
  const base = oracle.base;
  let n = 0;
  function add(name, mutate) {
    const input = structuredClone(base);
    mutate(input);
    cases.push({ name: name + '-' + n++, input: clone(input), expected: outcome(input) });
  }
  // Recipient domain × management action mapping.
  for (const recipient of ['plugin', 'integration', 'user'])
  for (const action of ['identity.role.assign', 'plugin.grant.manage', 'integration.grant.manage'])
    add('recipient-management', (x) => { x.candidate.recipient.kind = recipient; x.management.action = action; });
  // Actor domain and authentication strength.
  for (const kind of ['user', 'admin'])
  for (const strength of ['password', 'mfa'])
    add('actor', (x) => { x.actor.kind = kind; x.actor.authentication_strength = strength; });
  // Bound lifecycle states and delegable flag.
  for (const status of ['active', 'suspended', 'revoked'])
  for (const delegable of [true, false])
    add('bound-state', (x) => { x.bound.status = status; x.bound.delegable = delegable; });
  // Window edges around now and the bound window.
  for (const [now, start, end] of [[999, 1500, 3000], [1000, 1500, 3000], [3999, 1500, 3000], [4000, 1500, 3000],
    [1500, 1500, 4000], [1500, 1500, 1500], [1500, 3001, 3000], [1500, 4000, 4000], [1500, 1000, 1000]])
    add('window', (x) => { x.now_ms = now; x.candidate.not_before_ms = start; x.candidate.expires_at_ms = end; });
  // Scope subset/superset/disjoint and structural array violations.
  for (const ids of [['public-a'], ['public-b'], ['public-a', 'public-b'], ['public-c'], [], ['public-a', 'public-a'],
    Array.from({ length: 33 }, (_, i) => 'c' + i)])
    add('scope-ids', (x) => { x.candidate.scope.ids = structuredClone(ids); });
  // Endpoint candidate shapes with the bound carrying the same ID so only the
  // endpoint rule decides.
  for (const id of ['https://hooks.example.test', 'https://hooks.example.test:8443', 'http://hooks.example.test',
    'https://*.example.test', 'https://localhost', 'https://127.0.0.1', 'https://hooks.example.test/path', 'https://hooks.example.test:0'])
    add('endpoint', (x) => {
      x.candidate.recipient.kind = 'integration'; x.management.action = 'integration.grant.manage';
      x.bound.action = 'integration.webhook.invoke'; x.candidate.action = 'integration.webhook.invoke';
      x.bound.scope = { kind: 'endpoint', ids: [id] }; x.candidate.scope = { kind: 'endpoint', ids: [id] };
    });
  // Authentication strength matrix.
  for (const boundStrength of ['password', 'mfa'])
  for (const candidateStrength of ['password', 'mfa'])
    add('strength', (x) => { x.bound.required_strength = boundStrength; x.candidate.required_strength = candidateStrength; });
  assert.equal(n, 47);
  assert.equal(cases.length, 70);
  assert.equal(new Set(cases.map((c) => c.name)).size, cases.length);
  writeFileSync(args[1], JSON.stringify(cases, null, 2) + '\n');
  console.log(`Generated ${cases.length} G1 oracle cases: 23 fixtures + 47 matrix combinations`);
} else if (args.length === 3 && args[0] === '--verify') {
  const cases = read(args[1]), outputs = read(args[2]);
  assert.equal(cases.length, 70); assert.equal(outputs.length, cases.length);
  let accepted = 0, denied = 0, invalid = 0;
  for (let i = 0; i < cases.length; i++) {
    const c = cases[i], actual = outputs[i];
    assert.equal(actual.name, c.name);
    assert.deepEqual(actual.input, c.input, 'input changed: ' + c.name);
    assert.equal(actual.result, outcome(c.input), c.name);
    assert.equal(actual.result, c.expected, c.name);
    if (actual.result === 'accepted') accepted++;
    else if (actual.result === 'scope.input_invalid') invalid++;
    else denied++;
  }
  console.log(`Native Go/G1 oracle: ${outputs.length} outputs passed (${accepted} accepted, ${denied} denied, ${invalid} input rejections)`);
} else throw new Error('usage: --generate cases.json | --verify cases.json actual.json');
