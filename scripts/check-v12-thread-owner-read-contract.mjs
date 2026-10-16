#!/usr/bin/env node
// Independent owner-detail contract oracle; no production authorization imports.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const read = (path) => JSON.parse(readFileSync(resolve(root, path), 'utf8'));
const contract = 'campusos.policy.thread-owner-read/v1';
const policy = 'community.thread.owner_read/v1';
const schemaPath = 'docs/api/policy-thread-owner-read-v1.schema.json';
const decisionSchemaPath = 'docs/api/policy-thread-owner-read-decision-v1.schema.json';
const errorsPath = 'docs/api/policy-thread-owner-read-v1.errors.json';
const schema = read(schemaPath);
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read('docs/api/principal-context-v1.schema.json'));
ajv.addSchema(read('docs/api/policy-thread-public-read-v1.schema.json'));
assert(ajv.validateSchema(schema));
ajv.addSchema(schema);
const decisionSchema = read(decisionSchemaPath);
assert(ajv.validateSchema(decisionSchema));
const requestValid = ajv.compile({ $ref: schema.$id + '#/definitions/request' });
const decisionValid = ajv.compile(decisionSchema);
const factsValid = ajv.compile({ $ref: schema.$id + '#/definitions/facts' });
const principalValid = ajv.getSchema('urn:campusos:principal:v1');
const catalog = read(errorsPath);
assert.equal(catalog.contract, contract);
assert.deepEqual(catalog.errors.map((item) => [item.code, item.http_status, item.retryable]), [
  ['policy.input_invalid', 500, false], ['policy.contract_unsupported', 500, false],
  ['policy.owner_read_denied', 404, false], ['policy.decision_mismatch', 500, false], ['policy.facts_changed', 409, false],
]);

function principal(kind) {
  const p = { contract: 'campusos.principal/v1', actor: { kind, id: 'user-1' },
    audience: 'user', authentication_strength: 'password', credential_id: 'session-1' };
  if (kind === 'anonymous') {
    delete p.actor.id; delete p.credential_id;
    p.audience = 'public'; p.authentication_strength = 'none';
  } else if (kind === 'admin') {
    p.audience = 'admin'; p.authentication_strength = 'mfa';
  } else if (kind === 'integration') {
    p.audience = 'integration_api'; p.authentication_strength = 'credential';
  } else if (kind === 'plugin_instance' || kind === 'worker') {
    p.audience = kind === 'worker' ? 'worker' : 'host_api'; p.authentication_strength = 'workload';
    p.effective_subject = { kind: 'user', id: 'user-1' }; p.delegation_id = 'delegation-1';
  }
  return p;
}

function requestFor(kind = 'user') {
  return { contract, request_id: 'request-1', policy, principal: principal(kind), facts: {
    resource: { kind: 'community.thread', id: 'thread-1' }, owner: { kind: 'user', id: 'user-1' },
    board: { kind: 'community.board', id: 'board-1' }, facts_version: 'version-1',
    publication_status: 'private', moderation_status: 'pending', deletion_status: 'active',
  } };
}

// This expected allow set is a separate contract oracle, not a Go call.
function oracle(request) {
  assert(requestValid(request), JSON.stringify(requestValid.errors));
  const allow = request.principal.actor.kind === 'user' &&
    request.principal.actor.id === request.facts.owner.id && ['active', 'trashed'].includes(request.facts.deletion_status);
  return { contract, request_id: request.request_id, policy,
    principal: structuredClone(request.principal), resource: structuredClone(request.facts.resource),
    facts_version: request.facts.facts_version, effect: allow ? 'allow' : 'deny',
    reason: allow ? 'policy.owner_read' : 'policy.owner_read_denied', obligations: allow ? ['recheck_current_facts'] : [] };
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
  return value;
}
const same = (first, second) => JSON.stringify(canonical(first)) === JSON.stringify(canonical(second));

// Offline consumption specification only. The production service must reload
// facts and session proofs; the model never loads a database or authenticates.
function consume(request, decision, currentFacts, currentPrincipal) {
  if (!requestValid(request)) return 'policy.input_invalid';
  if (!decisionValid(decision) || decision.contract !== request.contract || decision.request_id !== request.request_id ||
    decision.policy !== request.policy || !same(decision.resource, request.facts.resource) ||
    decision.facts_version !== request.facts.facts_version || !same(decision.principal, request.principal)) return 'policy.decision_mismatch';
  if (decision.effect === 'deny') return 'policy.owner_read_denied';
  if (oracle(request).effect !== 'allow') return 'policy.decision_mismatch';
  if (!factsValid(currentFacts) || !principalValid(currentPrincipal) ||
    !same(currentFacts, request.facts) || !same(currentPrincipal, request.principal)) return 'policy.facts_changed';
  return 'allow';
}

const matrixNames = new Set();
let matrixAllows = 0;
for (const kind of ['anonymous', 'user', 'admin', 'integration', 'plugin_instance', 'worker']) {
  for (const publication of ['draft', 'published', 'private']) {
    for (const moderation of ['clear', 'pending', 'rejected', 'taken_down']) {
      for (const deletion of ['active', 'trashed', 'purged']) {
        const request = requestFor(kind);
        Object.assign(request.facts, { publication_status: publication, moderation_status: moderation, deletion_status: deletion });
        const decision = oracle(request);
        assert(decisionValid(decision), JSON.stringify(decisionValid.errors));
        assert.equal(consume(request, decision, request.facts, request.principal), decision.effect === 'allow' ? 'allow' : 'policy.owner_read_denied');
        if (decision.effect === 'allow') matrixAllows++;
        matrixNames.add([kind, publication, moderation, deletion].join('/'));
      }
    }
  }
}
assert.equal(matrixNames.size, 216); assert.equal(matrixAllows, 24);
for (const strength of ['password', 'mfa']) {
  const request = requestFor(); request.principal.authentication_strength = strength;
  assert.equal(oracle(request).effect, 'allow');
  request.principal.actor.id = 'other-user';
  assert.equal(oracle(request).effect, 'deny');
}

const malformed = [];
function invalid(name, target, change) {
  const request = requestFor(), value = target === 'request' ? request : oracle(request);
  change(value);
  const validate = target === 'request' ? requestValid : decisionValid;
  assert.equal(validate(value), false, name);
  malformed.push({ name, target, passed: true });
}
for (const field of schema.definitions.request.required) invalid('request-missing-' + field, 'request', (value) => { delete value[field]; });
for (const field of schema.definitions.facts.required) invalid('facts-missing-' + field, 'request', (value) => { delete value.facts[field]; });
for (const field of schema.definitions.decision.required) invalid('decision-missing-' + field, 'decision', (value) => { delete value[field]; });
for (const target of ['request', 'decision']) {
  invalid(target + '-extra', target, (value) => { value.roles = ['admin']; });
  invalid(target + '-principal-extra', target, (value) => { value.principal.owner_id = 'user-1'; });
  invalid(target + '-principal-null', target, (value) => { value.principal = null; });
  invalid(target + '-credential-missing', target, (value) => { delete value.principal.credential_id; });
  invalid(target + '-credential-null', target, (value) => { value.principal.credential_id = null; });
  invalid(target + '-audience', target, (value) => { value.principal.audience = 'admin'; });
  invalid(target + '-public-contract', target, (value) => { value.contract = 'campusos.policy.thread-public-read/v1'; });
}
invalid('facts-extra', 'request', (value) => { value.facts.allowed = true; });
invalid('facts-null', 'request', (value) => { value.facts = null; });
for (const field of ['publication_status', 'moderation_status', 'deletion_status']) {
  for (const bad of ['', 'unknown', null]) invalid('facts-' + field + '-' + String(bad), 'request', (value) => { value.facts[field] = bad; });
}
for (const field of ['resource', 'owner', 'board']) {
  invalid('facts-' + field + '-extra', 'request', (value) => { value.facts[field].roles = []; });
  invalid('facts-' + field + '-missing-id', 'request', (value) => { delete value.facts[field].id; });
}
for (const bad of [null, [], ['required_audit'], ['recheck_current_facts', 'recheck_current_facts']]) {
  invalid('decision-obligations-' + JSON.stringify(bad), 'decision', (value) => { value.obligations = bad; });
}
invalid('decision-effect-contradiction', 'decision', (value) => { value.effect = 'deny'; });
invalid('decision-reason-contradiction', 'decision', (value) => { value.reason = 'policy.owner_read_denied'; });
invalid('decision-resource-kind', 'decision', (value) => { value.resource.kind = 'community.post'; });

const bindings = [];
function binding(name, change, expected) {
  const request = requestFor(), decision = oracle(request), facts = structuredClone(request.facts), p = structuredClone(request.principal);
  change(request, decision, facts, p);
  assert.equal(consume(request, decision, facts, p), expected, name);
  bindings.push({ name, expected, passed: true });
}
binding('unchanged', () => {}, 'allow');
for (const field of ['request_id', 'policy', 'facts_version']) binding('decision-' + field, (_, d) => { d[field] += '-other'; }, 'policy.decision_mismatch');
binding('decision-resource', (_, d) => { d.resource.id = 'other-thread'; }, 'policy.decision_mismatch');
binding('decision-credential', (_, d) => { d.principal.credential_id = 'other-session'; }, 'policy.decision_mismatch');
binding('decision-principal-strength', (_, d) => { d.principal.authentication_strength = 'mfa'; }, 'policy.decision_mismatch');
for (const field of ['resource', 'owner', 'board']) binding('current-' + field, (_, __, f) => { f[field].id = 'other'; }, 'policy.facts_changed');
for (const [field, value] of [['facts_version', 'version-2'], ['publication_status', 'draft'], ['moderation_status', 'clear'], ['deletion_status', 'trashed']]) {
  binding('current-' + field, (_, __, f) => { f[field] = value; }, 'policy.facts_changed');
}
for (const [field, value] of [['credential_id', 'session-2'], ['authentication_strength', 'mfa'], ['audience', 'public'], ['delegation_id', 'delegation-1']]) {
  binding('current-principal-' + field, (_, __, ___, p) => { p[field] = value; }, 'policy.facts_changed');
}
binding('current-principal-actor', (_, __, ___, p) => { p.actor.id = 'other-user'; }, 'policy.facts_changed');
binding('current-principal-same-id-admin', (_, __, ___, p) => { p.actor.kind = 'admin'; p.audience = 'admin'; }, 'policy.facts_changed');
binding('current-missing-state', (_, __, f) => { delete f.moderation_status; }, 'policy.facts_changed');
binding('forged-allow-nonowner', (r, _, __, p) => { r.principal.actor.id = p.actor.id = 'other-user'; _.principal.actor.id = 'other-user'; }, 'policy.decision_mismatch');
binding('forged-allow-purged', (r, _, f) => { r.facts.deletion_status = f.deletion_status = 'purged'; }, 'policy.decision_mismatch');

const args = process.argv.slice(2);
let goOutput, reportPath;
for (let i = 0; i < args.length; i += 2) {
  assert(args[i + 1] && ['--go-output', '--report'].includes(args[i]), 'usage: [--go-output <JSONL>] [--report <JSON>]');
  if (args[i] === '--go-output') { assert(!goOutput); goOutput = args[i + 1]; }
  else { assert(!reportPath); reportPath = args[i + 1]; }
}
let native = 0;
if (goOutput) {
  const entries = readFileSync(resolve(root, goOutput), 'utf8').trim().split('\n').map((line) => JSON.parse(line));
  assert.equal(entries.length, 216, 'complete native realm/state matrix required');
  const names = new Set();
  for (const entry of entries) {
    assert(matrixNames.has(entry.name), 'unknown native case: ' + entry.name);
    assert(!names.has(entry.name), 'duplicate native case: ' + entry.name); names.add(entry.name);
    assert(requestValid(entry.request), JSON.stringify(requestValid.errors));
    assert(decisionValid(entry.decision), JSON.stringify(decisionValid.errors));
    const expected = oracle(entry.request);
    assert.deepEqual(entry.decision, expected, entry.name);
    assert.equal(entry.check_error ?? '', expected.effect === 'allow' ? '' : 'policy.owner_read_denied', entry.name);
    assert.equal([entry.request.principal.actor.kind, entry.request.facts.publication_status,
      entry.request.facts.moderation_status, entry.request.facts.deletion_status].join('/'), entry.name);
    native++;
  }
  assert.equal(names.size, matrixNames.size);
}
const report = {
  schema: 'campusos.v12-02b-thread-owner-policy-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'Owner-detail internal DTO schemas and independent oracle; no runtime JSON parser claim',
  state_principal_combinations: matrixNames.size, matrix_allows: matrixAllows, malformed_cases: malformed,
  binding_checks: bindings, native_go_decisions: native, passed: true,
  environment: { node: process.version, platform: process.platform, ajv: require('ajv/package.json').version },
  files_sha256: Object.fromEntries([schemaPath, decisionSchemaPath, errorsPath,
    'docs/api/principal-context-v1.schema.json', 'docs/api/policy-thread-public-read-v1.schema.json',
    'internal/modules/core/identity/port/thread_owner_read_policy.go', 'internal/modules/core/identity/policy/thread_owner_read.go',
    'scripts/check-v12-thread-owner-read-contract.mjs',
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
if (reportPath) writeFileSync(resolve(root, reportPath), JSON.stringify(report, null, 2) + '\n');
console.log(`owner policy contract passed: ${matrixNames.size} realm/state combinations, ${malformed.length} malformed cases, ${bindings.length} bindings, ${native} native Go decisions`);
