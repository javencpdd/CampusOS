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
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read(resolve(root, 'docs/api/principal-context-v1.schema.json')));
const validRequest = ajv.compile(read(resolve(root, 'docs/api/resource-policy-v1.schema.json')));
const validDecision = ajv.compile(read(resolve(root, 'docs/api/resource-policy-decision-v1.schema.json')));
const frozen = readFileSync(resolve(root, 'scripts/check-v12-resource-policy-contract.mjs'), 'utf8');
const start = frozen.indexOf('const user = '), end = frozen.indexOf('const results = [], names = new Set();');
assert(start > 0 && end > start, 'G1 oracle layout changed; review the extractor');
const oracle = vm.runInNewContext(frozen.slice(start, end) + '\n({cases, evaluate, request, facts, grant, user, admin, anonymous, plugin, actions});',
  { validRequest, validDecision, structuredClone }, { timeout: 1000 });
const args = process.argv.slice(2);
if (args.length === 2 && args[0] === '--generate') {
  const cases = oracle.cases.map(([name, input, expected]) => ({ name: 'g1-' + name, input: clone(input), expected }));
  assert.equal(cases.length, 22);
  function add(name, input) {
    const output = oracle.evaluate(input);
    assert(!output.error, name);
    cases.push({ name, input: clone(input), expected: output.effect === 'allow' ? 'allow' : output.reason });
  }
  function factsFor(action) {
    return clone(action === 'personal.document.read' ? oracle.facts.document : action === 'community.post.delete' ? oracle.facts.post :
      action === 'knowledge.source.read' ? oracle.facts.source : oracle.facts.thread);
  }
  let n = 0;
  for (const action of Object.keys(oracle.actions))
  for (const principal of [oracle.user(), oracle.user('u2'), oracle.admin(), oracle.anonymous, oracle.plugin])
  for (const status of ['published', 'private', 'deleted']) {
    const facts = factsFor(action); facts.status = status;
    add('g1-matrix-' + n++, oracle.request(action, facts, principal, [oracle.grant]));
  }
  assert.equal(n, 75);
  const principals = read(resolve(root, 'sdk/typescript/tests/principal-context-v1.fixtures.json')).cases.filter(c => c.valid).map(c => c.context);
  n = 0;
  for (const action of Object.keys(oracle.actions))
  for (const principal of principals)
  for (const status of ['active', 'draft', 'published', 'private', 'archived', 'deleted']) {
    const facts = factsFor(action); facts.status = status;
    if (principal.actor.kind === 'user' || principal.actor.kind === 'admin') facts.owner_id = principal.actor.id;
    else if (principal.effective_subject) facts.owner_id = principal.effective_subject.id;
    const grants = [{ ...clone(oracle.grant), subject_kind: principal.actor.kind === 'admin' ? 'admin' : 'user',
      subject_id: principal.actor.id ?? 'u1', action: action === 'community.post.delete' ? action : 'community.thread.take_down' }];
    add('domains-status-' + n++, oracle.request(action, facts, principal, grants));
  }
  assert.equal(n, 300);
  assert.equal(new Set(cases.map(c => c.name)).size, cases.length);
  writeFileSync(args[1], JSON.stringify(cases, null, 2) + '\n');
  console.log(`Generated ${cases.length} G1 oracle cases: 22 fixtures + 75 original matrix + 300 domain/status combinations`);
} else if (args.length === 3 && args[0] === '--verify') {
  const cases = read(args[1]), outputs = read(args[2]);
  assert.equal(cases.length, 397); assert.equal(outputs.length, cases.length);
  let allow = 0, deny = 0, errors = 0;
  for (let i = 0; i < cases.length; i++) {
    const c = cases[i], actual = outputs[i], expected = clone(oracle.evaluate(c.input));
    assert.equal(actual.name, c.name);
    assert.deepEqual(actual.input, c.input, 'input changed: ' + c.name);
    if (expected.error) { assert.equal(actual.error, expected.error, c.name); assert(!actual.decision, c.name); errors++; }
    else {
      assert(!actual.error, c.name); assert(validDecision(actual.decision), c.name);
      assert.deepEqual(actual.decision, expected, c.name);
      if (actual.decision.effect === 'allow') allow++; else deny++;
    }
  }
  console.log(`Native Go/G1 oracle: ${outputs.length} outputs passed (${allow} allow, ${deny} deny, ${errors} input rejection)`);
} else throw new Error('usage: --generate cases.json | --verify cases.json actual.json');
