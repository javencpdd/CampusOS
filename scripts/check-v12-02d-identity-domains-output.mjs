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
const schema = read(resolve(root, 'docs/api/identity-domains-v1.schema.json'));
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
assert(ajv.validateSchema(schema));
const validate = ajv.compile(schema);
const frozen = readFileSync(resolve(root, 'scripts/check-v12-identity-domains-contract.mjs'), 'utf8');
const start = frozen.indexOf('const domain = (kind) =>'), end = frozen.indexOf('const results = [];');
assert(start > 0 && end > start, 'G1 oracle layout changed; review the extractor');
const oracle = vm.runInNewContext(frozen.slice(start, end) + '\n({evaluate, cases});',
  { validate, structuredClone }, { timeout: 1000 });
const outcome = (input) => oracle.evaluate(clone(input)) ?? 'accepted';
const args = process.argv.slice(2);
if (args.length === 2 && args[0] === '--generate') {
  const cases = oracle.cases.map(([name, input, expected]) => {
    assert.equal(outcome(input), expected ?? 'accepted', 'frozen fixture expectation changed: ' + name);
    return { name: 'g1-' + name, input: clone(input), expected: expected ?? 'accepted' };
  });
  assert.equal(cases.length, 32);
  const base = structuredClone(oracle.cases[0][1]);
  const op = (kind) => ({ kind, actor_kind: 'admin', actor_mfa: true, target_admin_id: 'same-42',
    target_is_security_admin: false, active_security_admins: 2, bootstrap_enabled: false, audit_reason: 'required audit' });
  let n = 0;
  function add(name, mutate) {
    const input = structuredClone(base);
    mutate(input);
    cases.push({ name: name + '-' + n++, input: clone(input), expected: outcome(input) });
  }
  // Lifecycle operations: kind × actor domain × MFA.
  for (const kind of ['create', 'suspend', 'revoke', 'restore'])
  for (const actorKind of ['admin', 'user', 'system'])
  for (const mfa of [true, false])
    add('operation', (x) => { x.operation = op(kind); x.operation.actor_kind = actorKind; x.operation.actor_mfa = mfa; });
  // Security-admin protection matrix for suspend/revoke.
  for (const kind of ['suspend', 'revoke'])
  for (const isSecurity of [true, false])
  for (const active of [0, 1, 2])
    add('last-admin', (x) => { x.operation = op(kind); x.operation.target_is_security_admin = isSecurity; x.operation.active_security_admins = active; });
  // Bootstrap gate: enabled × remaining admins × actor domain.
  for (const enabled of [true, false])
  for (const active of [0, 1])
  for (const actorKind of ['system', 'admin'])
    add('bootstrap', (x) => { x.operation = op('bootstrap'); x.operation.actor_kind = actorKind; x.operation.bootstrap_enabled = enabled; x.operation.active_security_admins = active; });
  // Challenge kind × domain binding.
  for (const challenge of ['access', 'refresh', 'mfa', 'email', 'recovery'])
  for (const domain of ['user', 'admin'])
    add('challenge', (x) => { x.request.challenge_kind = challenge; x.request.challenge_domain = domain; });
  // Account status × activation source.
  for (const status of ['active', 'suspended', 'revoked'])
  for (const source of ['direct', 'bootstrap', 'user_role'])
    add('status-source', (x) => { x.request.status = status; x.request.activation_source = source; });
  assert.equal(n, 63);
  assert.equal(cases.length, 95);
  assert.equal(new Set(cases.map((c) => c.name)).size, cases.length);
  writeFileSync(args[1], JSON.stringify(cases, null, 2) + '\n');
  console.log(`Generated ${cases.length} G1 oracle cases: 32 fixtures + 63 matrix combinations`);
} else if (args.length === 3 && args[0] === '--verify') {
  const cases = read(args[1]), outputs = read(args[2]);
  assert.equal(cases.length, 95); assert.equal(outputs.length, cases.length);
  let accepted = 0, denied = 0, invalid = 0;
  for (let i = 0; i < cases.length; i++) {
    const c = cases[i], actual = outputs[i];
    assert.equal(actual.name, c.name);
    assert.deepEqual(actual.input, c.input, 'input changed: ' + c.name);
    assert.equal(actual.result, outcome(c.input), c.name);
    assert.equal(actual.result, c.expected, c.name);
    if (actual.result === 'accepted') accepted++;
    else if (actual.result === 'identity.domain_invalid') invalid++;
    else denied++;
  }
  console.log(`Native Go/G1 oracle: ${outputs.length} outputs passed (${accepted} accepted, ${denied} denied, ${invalid} input rejections)`);
} else throw new Error('usage: --generate cases.json | --verify cases.json actual.json');
