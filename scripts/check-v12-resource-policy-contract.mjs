#!/usr/bin/env node
// G1 offline pure Policy specification; facts must be loaded by the owning service in production.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const ts = require('typescript');
const read = (path) => JSON.parse(readFileSync(resolve(root, path), 'utf8'));
const requestPath = 'docs/api/resource-policy-v1.schema.json';
const decisionPath = 'docs/api/resource-policy-decision-v1.schema.json';
const principalPath = 'docs/api/principal-context-v1.schema.json';
const errorsPath = 'docs/api/resource-policy-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
ajv.addSchema(read(principalPath));
const validRequest = ajv.compile(read(requestPath));
const validDecision = ajv.compile(read(decisionPath));
const errors = read(errorsPath);
assert.equal(errors.contract, 'campusos.resource-policy/v1');
assert.equal(errors.errors.length, 6);
const user = (id = 'u1', strength = 'password') => ({ contract: 'campusos.principal/v1',
  actor: { kind: 'user', id }, audience: 'user', authentication_strength: strength, credential_id: 'c-' + id });
const admin = (id = 'a1', strength = 'mfa') => ({ contract: 'campusos.principal/v1',
  actor: { kind: 'admin', id }, audience: 'admin', authentication_strength: strength, credential_id: 'c-' + id });
const anonymous = { contract: 'campusos.principal/v1', actor: { kind: 'anonymous' },
  audience: 'public', authentication_strength: 'none' };
const plugin = { contract: 'campusos.principal/v1', actor: { kind: 'plugin_instance', id: 'p1' },
  audience: 'host_api', authentication_strength: 'workload', credential_id: 'c-p1' };
const facts = {
  thread: { kind: 'thread', id: 't1', owner_id: 'u1', board_id: 'b1', status: 'published', publication_status: 'published', version: 'r1' },
  post: { kind: 'post', id: 'p1', owner_id: 'u2', board_id: 'b1', status: 'published', publication_status: 'published', version: 'r1' },
  document: { kind: 'document', id: 'd1', owner_id: 'u1', status: 'active', version: 'r1' },
  source: { kind: 'knowledge_source', id: 's1', collection_id: 'public-a', status: 'published', publication_status: 'published', origin_visible: true, version: 'r1' },
};
const grant = { subject_kind: 'user', subject_id: 'u1', action: 'community.thread.take_down',
  board_id: 'b1', expires_at_ms: 2000, status: 'active', required_strength: 'mfa' };
const request = (action, resourceFacts, principal = user(), grants = []) => ({
  contract: 'campusos.resource-policy/v1', request_id: 'req1', principal,
  action, facts: resourceFacts, grants, evaluated_at_ms: 1000, policy_version: 'policy-1',
});
const actions = {
  'community.thread.edit': 'thread', 'community.thread.take_down': 'thread',
  'community.post.delete': 'post', 'personal.document.read': 'document',
  'knowledge.source.read': 'knowledge_source',
};
const writes = new Set(['community.thread.edit', 'community.thread.take_down', 'community.post.delete']);
function evaluate(input) {
  if (!Object.hasOwn(actions, input?.action)) return { error: 'policy.unknown_action' };
  if (!validRequest(input)) return { error: 'policy.invalid' };
  const f = input.facts, p = input.principal, actor = p.actor;
  const denied = (reason) => ({ request_id: input.request_id, effect: 'deny', reason,
    facts_version: f.version, policy_version: input.policy_version, obligations: [] });
  if (f.kind !== actions[input.action] || f.status === 'archived' || f.status === 'deleted')
    return denied('policy.resource_unavailable');
  let allow = false;
  if (input.action === 'community.thread.edit') {
    if (!f.owner_id || !f.board_id || !f.publication_status) return denied('policy.resource_unavailable');
    if (actor.kind !== 'user') return denied('policy.principal_wrong_domain');
    allow = actor.id === f.owner_id;
  } else if (input.action === 'community.thread.take_down' || input.action === 'community.post.delete') {
    if (!f.owner_id || !f.board_id || !f.publication_status) return denied('policy.resource_unavailable');
    if (!['user', 'admin'].includes(actor.kind)) return denied('policy.principal_wrong_domain');
    allow = input.grants.some((g) => g.subject_kind === actor.kind && g.subject_id === actor.id &&
      g.action === input.action && g.board_id === f.board_id && g.status === 'active' &&
      input.evaluated_at_ms < g.expires_at_ms &&
      (g.required_strength === 'password' || p.authentication_strength === 'mfa'));
    if (actor.kind === 'admin' && p.authentication_strength !== 'mfa') allow = false;
  } else if (input.action === 'personal.document.read') {
    if (!f.owner_id) return denied('policy.resource_unavailable');
    if (actor.kind !== 'user') return denied('policy.principal_wrong_domain');
    allow = actor.id === f.owner_id && f.status === 'active';
  } else {
    if (!f.collection_id || f.origin_visible === undefined || !f.publication_status)
      return denied('policy.resource_unavailable');
    allow = f.status === 'published' && f.publication_status === 'published' && f.origin_visible === true;
  }
  if (!allow) return denied('policy.scope_denied');
  return { request_id: input.request_id, effect: 'allow', reason: 'ALLOW',
    facts_version: f.version, policy_version: input.policy_version,
    obligations: writes.has(input.action) ? ['recheck_facts', 'required_audit'] : ['recheck_facts'] };
}
function consume(initial, decision, current) {
  if (!validDecision(decision) || !validRequest(current) ||
      initial.request_id !== decision.request_id || initial.request_id !== current.request_id ||
      initial.action !== current.action || initial.principal.actor.kind !== current.principal.actor.kind ||
      initial.principal.actor.id !== current.principal.actor.id ||
      decision.facts_version !== current.facts.version ||
      decision.policy_version !== current.policy_version ||
      decision.effect !== 'allow' || !decision.obligations.includes('recheck_facts') ||
      (writes.has(current.action) && !decision.obligations.includes('required_audit')))
    return 'policy.facts_changed';
  return evaluate(current).effect === 'allow' ? 'allow' : 'policy.facts_changed';
}
const clone = (x) => structuredClone(x);
const variant = (source, mutate) => { const x = clone(source); mutate(x); return x; };
const boardAllowed = request('community.thread.take_down', facts.thread, user('u1', 'mfa'), [grant]);
const cases = [
  ['author-edit', request('community.thread.edit', facts.thread), 'allow'],
  ['other-author', request('community.thread.edit', facts.thread, user('u2')), 'policy.scope_denied'],
  ['admin-is-not-author', request('community.thread.edit', facts.thread, admin()), 'policy.principal_wrong_domain'],
  ['board-governance', boardAllowed, 'allow'],
  ['board-wrong-board', variant(boardAllowed, (x) => { x.facts.board_id = 'b2'; }), 'policy.scope_denied'],
  ['board-no-mfa', variant(boardAllowed, (x) => { x.principal = user('u1'); }), 'policy.scope_denied'],
  ['board-revoked', variant(boardAllowed, (x) => { x.grants[0].status = 'revoked'; }), 'policy.scope_denied'],
  ['board-expired', variant(boardAllowed, (x) => { x.evaluated_at_ms = 2000; }), 'policy.scope_denied'],
  ['board-other-actor', variant(boardAllowed, (x) => { x.principal = user('u2', 'mfa'); }), 'policy.scope_denied'],
  ['admin-explicit-grant', request('community.thread.take_down', facts.thread, admin(),
    [{ ...grant, subject_kind: 'admin', subject_id: 'a1' }]), 'allow'],
  ['admin-no-mfa', request('community.thread.take_down', facts.thread, admin('a1', 'password'),
    [{ ...grant, subject_kind: 'admin', subject_id: 'a1' }]), 'policy.scope_denied'],
  ['delete-post', request('community.post.delete', facts.post, user('u1', 'mfa'),
    [{ ...grant, action: 'community.post.delete' }]), 'allow'],
  ['document-owner', request('personal.document.read', facts.document), 'allow'],
  ['document-other', request('personal.document.read', facts.document, user('u2')), 'policy.scope_denied'],
  ['document-admin', request('personal.document.read', facts.document, admin()), 'policy.principal_wrong_domain'],
  ['document-plugin', request('personal.document.read', facts.document, plugin), 'policy.principal_wrong_domain'],
  ['source-public', request('knowledge.source.read', facts.source, anonymous), 'allow'],
  ['source-private', request('knowledge.source.read', { ...facts.source, publication_status: 'private' }, anonymous), 'policy.scope_denied'],
  ['source-origin-hidden', request('knowledge.source.read', { ...facts.source, origin_visible: false }, user()), 'policy.scope_denied'],
  ['source-deleted', request('knowledge.source.read', { ...facts.source, status: 'deleted' }, user()), 'policy.resource_unavailable'],
  ['wrong-resource-kind', request('personal.document.read', facts.thread), 'policy.resource_unavailable'],
  ['unknown-action', { ...request('community.thread.edit', facts.thread), action: 'anything' }, 'policy.unknown_action'],
];
const results = [], names = new Set();
for (const [name, input, expected] of cases) {
  assert(!names.has(name), name); names.add(name);
  const before = JSON.stringify(input), output = evaluate(input);
  assert.equal(output.error ?? (output.effect === 'allow' ? 'allow' : output.reason), expected, name);
  if (!output.error) assert(validDecision(output), name);
  assert.equal(JSON.stringify(input), before, 'mutation ' + name);
  results.push({ name, expected, passed: true });
}
let combinations = 0;
for (const action of Object.keys(actions))
for (const principal of [user(), user('u2'), admin(), anonymous, plugin])
for (const status of ['published', 'private', 'deleted']) {
  const f = clone(action === 'personal.document.read' ? facts.document :
    action === 'community.post.delete' ? facts.post :
    action === 'knowledge.source.read' ? facts.source : facts.thread);
  f.status = status;
  const r = request(action, f, principal, [grant]);
  const result = evaluate(r);
  const expected = status !== 'deleted' &&
    (action === 'community.thread.edit' ? principal.actor.kind === 'user' && principal.actor.id === 'u1' :
     action === 'personal.document.read' ? false :
     action === 'community.thread.take_down' ? principal.actor.kind === 'user' && principal.actor.id === 'u1' && principal.authentication_strength === 'mfa' :
     action === 'community.post.delete' ? false :
     status === 'published');
  // The matrix checks a strict truth table; private documents require status=active, and
  // governance grants in this matrix require MFA (the user() fixture uses password).
  assert.equal(result.effect === 'allow', expected, [action, principal.actor.kind, status].join('/'));
  combinations++;
}
const rechecks = [];
const allowed = evaluate(boardAllowed);
for (const [name, mutate] of [
  ['unchanged', () => {}],
  ['facts-version', (d, r) => { r.facts.version = 'r2'; }],
  ['board-moved', (d, r) => { r.facts.board_id = 'b2'; }],
  ['grant-revoked', (d, r) => { r.grants[0].status = 'revoked'; }],
  ['principal-changed', (d, r) => { r.principal = user('u2', 'mfa'); }],
  ['policy-version', (d, r) => { r.policy_version = 'policy-2'; }],
  ['audit-missing', (d) => { d.obligations = ['recheck_facts']; }],
]) {
  const d = clone(allowed), r = clone(boardAllowed);
  mutate(d, r);
  const expected = name === 'unchanged' ? 'allow' : 'policy.facts_changed';
  assert.equal(consume(boardAllowed, d, r), expected, name);
  rechecks.push({ name, expected, passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__resource_policy__.ts');
const lines = [
  'import type { ResourcePolicyRequestV1, ResourcePolicyDecisionV1, ResourcePolicyActionV1, ResourcePolicyErrorCodeV1 } from "../src/index";',
  'const request: ResourcePolicyRequestV1 = ' + JSON.stringify(boardAllowed) + ';',
  'const decision: ResourcePolicyDecisionV1 = ' + JSON.stringify(allowed) + ';',
  '// @ts-expect-error unregistered action',
  'const badAction: ResourcePolicyActionV1 = "community.thread.view_all";',
  '// @ts-expect-error unknown reason',
  'const badError: ResourcePolicyErrorCodeV1 = "policy.allow";',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['resource-policy', true]]) {
  const options = { noEmit: true, strict: true, exactOptionalPropertyTypes, skipLibCheck: true,
    target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler, types: [] };
  const host = ts.createCompilerHost(options), get = host.getSourceFile.bind(host);
  host.getSourceFile = (path, language, onError, fresh) => path === virtual
    ? ts.createSourceFile(path, lines.join('\n').replaceAll('../src/index', '../src/' + entry), language, true)
    : get(path, language, onError, fresh);
  const diagnostics = ts.getPreEmitDiagnostics(ts.createProgram([virtual], options, host));
  assert.equal(diagnostics.length, 0, ts.formatDiagnosticsWithColorAndContext(diagnostics, {
    getCanonicalFileName: (x) => x, getCurrentDirectory: () => root, getNewLine: () => '\n',
  }));
}
const report = { schema: 'campusos.v12-g1-resource-policy-acceptance/v1',
  generated_at: new Date().toISOString(), scope: 'G1 author/governance/document/public-source pure Policy target',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 02a/02b/02d/09a: no live facts load, DB transaction, ACL or HTTP decision claimed',
  environment: { platform: process.platform, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, principal_state_combinations: combinations, rechecks,
  files_sha256: Object.fromEntries([requestPath, decisionPath, principalPath, errorsPath,
    'sdk/typescript/src/resource-policy.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-resource-policy-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Resource Policy: ' + results.length + ' fixtures, ' + combinations + ' combinations, ' + rechecks.length + ' rechecks, 2/2 TS positive/negative types passed');
