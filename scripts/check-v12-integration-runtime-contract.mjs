#!/usr/bin/env node
// Independent test oracle for the host-only whitelist contract. Production Go
// never imports this checker, its fixtures, or the resulting JSON decisions.
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const read = (path) => JSON.parse(readFileSync(resolve(root, path), 'utf8'));
const schemaPath = 'docs/api/integration-access-v2.schema.json';
const fixturePath = 'sdk/typescript/tests/integration-access-v2.fixtures.json';
const schema = read(schemaPath);
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read('docs/api/principal-context-v1.schema.json'));
assert(ajv.validateSchema(schema));
const validate = ajv.compile(schema);
const prefix = (reason) => 'integration.' + reason;

// This evaluator uses ordered independent predicates and a relational witness
// query. No policy source extraction or expected Go output is used as input.
function oracle(input) {
  if (!validate(input)) return { reason: prefix('input_invalid') };
  const { facts: f, target: t, principal: p, now_ms: now } = input;
  if (f.expires_at_ms <= f.not_before_ms ||
      new Set(f.rules.map((rule) => rule.id)).size !== f.rules.length ||
      f.rules.some((rule) => rule.expires_at_ms <= rule.not_before_ms)) {
    return { reason: prefix('input_invalid') };
  }
  const gates = [
    ['actor_denied', p.actor.kind === 'integration' && p.actor.id === f.integration_id && p.credential_id === f.credential_id],
    ['principal_inactive', f.principal_status === 'active'],
    ['credential_denied', f.credential_status === 'active' && f.credential_audience === 'integration_api' && now >= f.not_before_ms && now < f.expires_at_ms],
    ['channel_denied', new Set(['integration_http', 'integration_mcp']).has(t.channel)],
    ['scope_denied', (t.action === 'community.thread.read' && t.resource.kind === 'community.thread' || t.action === 'knowledge.source.read' && t.resource.kind === 'public_collection')],
    ['endpoint_denied', f.endpoint.enabled && f.endpoint.integration_allowed &&
      f.endpoint.channel === t.channel && f.endpoint.id === t.endpoint &&
      f.endpoint.action === t.action && f.endpoint.resource_kind === t.resource.kind],
  ];
  const failed = gates.find(([, allowed]) => !allowed);
  if (failed) return { effect: 'deny', reason: prefix(failed[0]) };
  const witnesses = f.rules.filter((rule) => rule.status === 'active' &&
    rule.not_before_ms <= now && now < rule.expires_at_ms &&
    [rule.channel, rule.endpoint, rule.action, rule.resource_kind].every((v, i) =>
      v === [t.channel, t.endpoint, t.action, t.resource.kind][i]) &&
    rule.resource_ids.includes(t.resource.id)).map((rule) => rule.id).sort();
  return witnesses.length
    ? { effect: 'allow', reason: prefix('allowed'), rule_id: witnesses[0] }
    : { effect: 'deny', reason: prefix('whitelist_denied') };
}

const fixtures = read(fixturePath);
assert.equal(fixtures.length, 192, 'complete reviewed domain and lifecycle corpus');
assert.equal(new Set(fixtures.map((item) => item.name)).size, fixtures.length);
const counts = {};
for (const fixture of fixtures) {
  const expected = oracle(fixture.input);
  assert.equal(expected.reason, fixture.expected, fixture.name);
  counts[expected.reason] = (counts[expected.reason] ?? 0) + 1;
}
const errors = read('docs/api/integration-access-v1.errors.json');
const expectedCodes = ['input_invalid', 'decision_mismatch', 'facts_changed', 'actor_denied',
  'principal_inactive', 'credential_denied', 'channel_denied', 'scope_denied', 'endpoint_denied', 'whitelist_denied'].map(prefix);
assert.deepEqual(errors.errors.map((item) => item.code).sort(), expectedCodes.sort());
assert.equal(errors.allow_reason, prefix('allowed'));
const sdk = readFileSync(resolve(root, 'sdk/typescript/src/integration-runtime.ts'), 'utf8');
for (const code of [...expectedCodes, errors.allow_reason, schema.properties.contract.const]) {
  assert((sdk + readFileSync(resolve(root, "sdk/typescript/src/integration-access.ts"), "utf8")).includes(JSON.stringify(code)), 'SDK is missing ' + code);
}
assert(readFileSync(resolve(root, 'sdk/typescript/src/index.ts'), 'utf8').includes('"./integration-runtime.js"'));

const args = process.argv.slice(2);
let output, reportPath;
for (let i = 0; i < args.length; i += 2) {
  assert(args[i + 1] && ['--output', '--report'].includes(args[i]), 'usage: [--output actual.json] [--report report.json]');
  if (args[i] === '--output') { assert(!output); output = args[i + 1]; }
  else { assert(!reportPath); reportPath = args[i + 1]; }
}
let actualCount = 0;
if (output) {
  const actual = JSON.parse(readFileSync(output, 'utf8'));
  assert.equal(actual.length, fixtures.length);
  for (let i = 0; i < fixtures.length; i++) {
    const expected = oracle(fixtures[i].input), row = actual[i];
    assert.equal(row.name, fixtures[i].name);
    assert.deepEqual(row.input, fixtures[i].input, row.name + ': changed input');
    assert.equal(row.result, expected.reason, row.name);
    if (expected.reason === prefix('input_invalid')) assert.equal(row.decision, null, row.name);
    else assert.deepEqual(row.decision, { request: row.input, ...expected }, row.name + ': unbound decision');
    actualCount++;
  }
}
const inputs = [schemaPath, fixturePath, 'docs/api/principal-context-v1.schema.json',
  'docs/api/integration-access-v1.errors.json', 'sdk/typescript/src/integration-runtime.ts',
  'sdk/typescript/src/index.ts', 'scripts/check-v12-integration-runtime-contract.mjs'];
const report = {
  schema: 'campusos.integration-access-oracle/v2', status: 'passed', fixture_count: fixtures.length,
  matrix_combinations: 144, reason_counts: counts, actual_native_go_decisions: actualCount,
  input_sha256: Object.fromEntries(inputs.map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
if (reportPath) writeFileSync(reportPath, JSON.stringify(report, null, 2) + '\n');
console.log(`Integration contract passed: ${fixtures.length} fixtures, 144 domain/status/credential/channel combinations, ${actualCount} actual Go decisions`);
