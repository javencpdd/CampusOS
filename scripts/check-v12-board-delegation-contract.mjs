#!/usr/bin/env node
// Offline G1 executable specification. Never used to grant runtime permissions.
import assert from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv'), ts = require('typescript');
const read = (p) => JSON.parse(readFileSync(resolve(root, p), 'utf8'));
const schemaPath = 'docs/api/board-delegation-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/board-delegation-v1.fixtures.json';
const schema = read(schemaPath), corpus = read(fixturePath), catalog = read('docs/api/board-delegation-v1.errors.json');
const contract = 'campusos.board-delegation/v1';
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read('docs/api/principal-context-v1.schema.json'));
assert(ajv.validateSchema(schema)); ajv.addSchema(schema);
const validRequest = ajv.compile({ $ref: schema.$id + '#/definitions/request' });
const validDecision = ajv.compile({ $ref: schema.$id + '#/definitions/decision' });
assert.equal(corpus.contract, contract); assert.equal(catalog.contract, contract);
const codes = ['delegation.input_invalid', 'delegation.contract_unsupported', 'delegation.actor_denied', 'delegation.mfa_required', 'delegation.management_denied', 'delegation.recipient_denied', 'delegation.board_denied', 'delegation.outside_bounds', 'delegation.decision_mismatch', 'delegation.facts_changed'];
assert.deepEqual(catalog.errors.map((e) => e.code), codes);
assert.deepEqual(catalog.errors.map((e) => e.http_status), [400, 400, 403, 403, 403, 403, 403, 403, 500, 409]);
assert(catalog.errors.every((e) => e.retryable === false));
const sameSubject = (a, b) => a.kind === b.kind && a.id === b.id;
const active = (g, now) => g.status === 'active' && g.not_before <= now && now < g.expires_at;
const unique = (xs) => new Set(xs).size === xs.length;
function evaluate(r) {
  if (r && typeof r.contract === 'string' && r.contract !== contract) return { error: 'delegation.contract_unsupported' };
  if (!validRequest(r)) return { error: 'delegation.input_invalid' };
  const allWindows = [r.management, ...r.bounds, ...r.candidate.grants];
  if (allWindows.some((g) => g.not_before >= g.expires_at) || !unique(r.boards.map((b) => b.id)) ||
      !unique(r.bounds.map((b) => b.id)) || !unique(r.candidate.grants.map((g) => JSON.stringify([g.action, g.board_id])))) return { error: 'delegation.input_invalid' };
  const result = (reason, witnesses = []) => ({ contract, request_id: r.request_id, facts_version: r.facts_version,
    effect: reason === 'delegation.allowed' ? 'allow' : 'deny', reason, witnesses,
    obligations: reason === 'delegation.allowed' ? ['recheck_authority_in_transaction', 'required_audit'] : [] });
  if (r.principal.actor.kind !== 'admin') return result('delegation.actor_denied');
  if (r.principal.authentication_strength !== 'mfa') return result('delegation.mfa_required');
  if (!sameSubject(r.management.actor, r.principal.actor) || !active(r.management, r.evaluated_at)) return result('delegation.management_denied');
  if (!sameSubject(r.recipient.subject, r.candidate.recipient) || r.recipient.status !== 'active') return result('delegation.recipient_denied');
  if (r.candidate.grants.some((g) => !r.boards.some((b) => b.id === g.board_id && b.status === 'active'))) return result('delegation.board_denied');
  const witnesses = [];
  for (const [i, g] of r.candidate.grants.entries()) {
    const bound = [...r.bounds].sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0).find((b) =>
      sameSubject(b.actor, r.principal.actor) && b.delegable && active(b, r.evaluated_at) &&
      g.action === b.action && g.board_id === b.board_id && g.not_before >= r.evaluated_at &&
      g.not_before >= b.not_before && g.expires_at <= b.expires_at &&
      (b.required_strength === 'password' || g.required_strength === 'mfa'));
    if (!bound) return result('delegation.outside_bounds');
    witnesses.push({ candidate_index: i, bound_id: bound.id });
  }
  return result('delegation.allowed', witnesses);
}
// Model the transaction's pre-commit recheck, not a real database transaction.
function consume(original, decision, current) {
  if (!validDecision(decision) || !isDeepStrictEqual(decision, evaluate(original))) return 'delegation.decision_mismatch';
  if (decision.effect === 'deny') return decision.reason;
  if (!validRequest(current) || current.request_id !== original.request_id || current.facts_version !== original.facts_version ||
      !isDeepStrictEqual(current.candidate, original.candidate) ||
      !sameSubject(current.principal.actor, original.principal.actor) || current.principal.credential_id !== original.principal.credential_id ||
      current.evaluated_at < original.evaluated_at) return 'delegation.facts_changed';
  const fresh = evaluate(current);
  return fresh.effect === 'allow' && isDeepStrictEqual(fresh.witnesses, decision.witnesses) ? 'allow' : 'delegation.facts_changed';
}
const lines = [
  'import { BOARD_DELEGATION_CONTRACT } from "../src/index";',
  'import type { BoardDelegationRequestV1, BoardDelegationDecisionV1, BoardDelegationErrorCode } from "../src/index";',
  `const version: ${JSON.stringify(contract)} = BOARD_DELEGATION_CONTRACT;`,
  ...codes.map((c, i) => `const error${i}: BoardDelegationErrorCode = ${JSON.stringify(c)};`),
  '// @ts-expect-error unknown error must not compile',
  'const errorUnknown: BoardDelegationErrorCode = "delegation.super_admin";',
];
const results = [], names = new Set();
for (const [i, c] of corpus.cases.entries()) {
  assert(!names.has(c.name), c.name); names.add(c.name);
  const before = JSON.stringify(c.request);
  assert.equal(validRequest(c.request), c.schema_valid, c.name);
  const d = evaluate(c.request);
  assert.equal(d.error ?? d.reason, c.expected, c.name);
  assert.equal(JSON.stringify(c.request), before, 'input mutation: ' + c.name);
  if (!d.error) {
    assert(validDecision(d), c.name);
    if (d.effect === 'deny') assert.equal(d.witnesses.length, 0, 'no partial authorization');
    lines.push(`const decision${i}: BoardDelegationDecisionV1 = ${JSON.stringify(d)};`);
  }
  if (c.schema_valid || c.compile_reject) {
    if (c.compile_reject) lines.push('// @ts-expect-error deliberate contract violation');
    lines.push(`const request${i}: BoardDelegationRequestV1 = ${before};`);
  }
  results.push({ name: c.name, schema_valid: c.schema_valid, expected: c.expected, passed: true });
}
const base = structuredClone(corpus.cases[0].request);
// Independent subset truth table: action/board cross product and time/strength.
let combinations = 0;
for (const action of ['community.thread.take_down', 'community.post.delete'])
for (const board of ['board-A', 'board-B'])
for (const start of [999, 1000, 1100])
for (const end of [1800, 2000, 2001])
for (const strength of ['password', 'mfa']) {
  const r = structuredClone(base);
  Object.assign(r.candidate.grants[0], { action, board_id: board, not_before: start, expires_at: end, required_strength: strength });
  const expected = action === 'community.thread.take_down' && board === 'board-A' && [1000, 1100].includes(start) && [1800, 2000].includes(end) && strength === 'mfa';
  assert.equal(evaluate(r).effect, expected ? 'allow' : 'deny'); combinations++;
}
const rechecks = [];
function check(name, mutate, expected) {
  const original = structuredClone(base), decision = evaluate(original), current = structuredClone(original);
  mutate(decision, current);
  assert.equal(consume(original, decision, current), expected, name);
  rechecks.push({ name, expected, passed: true });
}
check('unchanged', () => {}, 'allow');
check('object-key-order-independent', (d, r) => { const id = d.request_id; delete d.request_id; d.request_id = id; const kind = r.candidate.recipient.kind; delete r.candidate.recipient.kind; r.candidate.recipient.kind = kind; }, 'allow');
for (const [name, mutate] of [
  ['authority-version', (_, r) => { r.facts_version = 'facts-2'; }],
  ['bound-revoked', (_, r) => { r.bounds[0].status = 'revoked'; }],
  ['management-suspended', (_, r) => { r.management.status = 'suspended'; }],
  ['recipient-suspended', (_, r) => { r.recipient.status = 'suspended'; }],
  ['board-archived', (_, r) => { r.boards[0].status = 'archived'; }],
  ['clock-expired', (_, r) => { r.evaluated_at = 2000; }],
  ['clock-backwards', (_, r) => { r.evaluated_at = 999; }],
  ['lost-mfa', (_, r) => { r.principal.authentication_strength = 'password'; }],
  ['credential-changed', (_, r) => { r.principal.credential_id = 'c-2'; }],
  ['candidate-changed', (_, r) => { r.candidate.recipient.id = 'user-2'; }],
]) check(name, mutate, 'delegation.facts_changed');
for (const [name, mutate] of [
  ['wrong-request', (d) => { d.request_id = 'request-2'; }],
  ['missing-audit', (d) => { d.obligations.pop(); }],
  ['wrong-witness', (d) => { d.witnesses[0].bound_id = 'invented'; }],
  ['missing-witness', (d) => { d.witnesses = []; }],
  ['duplicate-witness', (d) => { d.witnesses.push(structuredClone(d.witnesses[0])); }],
]) check(name, mutate, 'delegation.decision_mismatch');
const virtual = resolve(root, 'sdk/typescript/tests/__board_delegation__.ts');
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['board-delegation', true]]) {
  const options = { noEmit: true, strict: true, exactOptionalPropertyTypes, skipLibCheck: true, types: [], target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, moduleResolution: ts.ModuleResolutionKind.Bundler };
  const host = ts.createCompilerHost(options), get = host.getSourceFile.bind(host);
  host.getSourceFile = (path, lang, onError, fresh) => path === virtual ? ts.createSourceFile(path, lines.join('\n').replaceAll('../src/index', `../src/${entry}`), lang, true) : get(path, lang, onError, fresh);
  const errors = ts.getPreEmitDiagnostics(ts.createProgram([virtual], options, host));
  assert.equal(errors.length, 0, ts.formatDiagnosticsWithColorAndContext(errors, { getCanonicalFileName: (x) => x, getCurrentDirectory: () => root, getNewLine: () => '\n' }));
}
const report = {
  schema: 'campusos.v12-g1-board-delegation-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'G1 board governance delegation bounds DTO/schema and executable specification only',
  implementation: 'passed', automation: 'passed', target_environment: `${process.platform} offline contract acceptance passed`,
  runtime_acceptance: 'pending 02a/02d/02b: no DB writes, revocation races, audit transaction or UI acceptance claimed',
  environment: { platform: process.platform, node: process.version, ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, subset_combinations: combinations, recheck_cases: rechecks,
  typescript_request_fixtures: corpus.cases.filter((c) => c.schema_valid || c.compile_reject).length,
  typescript_decision_fixtures: corpus.cases.filter((c) => !evaluate(c.request).error).length,
  files_sha256: Object.fromEntries([schemaPath, fixturePath, 'docs/api/board-delegation-v1.errors.json', 'docs/api/principal-context-v1.schema.json',
    'sdk/typescript/src/board-delegation.ts', 'sdk/typescript/src/principal.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml', 'scripts/check-v12-board-delegation-contract.mjs'
  ].map((p) => [p, createHash('sha256').update(readFileSync(resolve(root, p))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log(`Board delegation: ${results.length} fixtures, ${combinations} subset combinations, ${rechecks.length} rechecks, ${report.typescript_request_fixtures}/${report.typescript_decision_fixtures} TS request/decision fixtures passed`);
