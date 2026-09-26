#!/usr/bin/env node
// G1 contract examples and reference semantics only; no production Authorizer.
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
const contract = 'campusos.policy.thread-public-read/v1';
const policy = 'community.thread.public_read/v1';
const schemaPath = 'docs/api/policy-thread-public-read-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/policy-thread-public-read-v1.fixtures.json';
const schema = read(schemaPath);
const corpus = read(fixturePath);
const catalog = read('docs/api/policy-thread-public-read-v1.errors.json');
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read('docs/api/principal-context-v1.schema.json'));
assert(ajv.validateSchema(schema));
ajv.addSchema(schema);
const requestValid = ajv.compile({ $ref: schema.$id + '#/definitions/request' });
const decisionValid = ajv.compile({ $ref: schema.$id + '#/definitions/decision' });
assert.equal(corpus.contract, contract);
assert.equal(catalog.contract, contract);
assert.deepEqual(catalog.errors.map((x) => [x.code, x.http_status, x.retryable]), [
  ['policy.input_invalid', 400, false], ['policy.contract_unsupported', 400, false],
  ['policy.resource_not_public', 404, false], ['policy.decision_mismatch', 500, false],
  ['policy.facts_changed', 409, false],
]);
// Executable specification, intentionally local to this development checker.
function referenceDecision(request) {
  if (request && typeof request.contract === 'string' && request.contract !== contract) {
    return { error: 'policy.contract_unsupported' };
  }
  if (!requestValid(request)) return { error: 'policy.input_invalid' };
  const f = request.facts;
  const allow = f.publication_status === 'published' && f.moderation_status === 'clear' && f.deletion_status === 'active';
  return {
    contract, request_id: request.request_id, policy, resource: structuredClone(f.resource), facts_version: f.facts_version,
    effect: allow ? 'allow' : 'deny', reason: allow ? 'policy.public_read' : 'policy.resource_not_public',
    obligations: allow ? ['recheck_current_facts'] : [],
  };
}
// Contract consumer model: an unmatched/stale decision is never a reusable grant.
// Current facts would be reloaded by Community in the production adapter (02b).
function consume(request, decision, currentFacts) {
  if (!requestValid(request)) return 'policy.input_invalid';
  if (!decisionValid(decision) || decision.request_id !== request.request_id || decision.policy !== request.policy ||
      decision.resource.kind !== request.facts.resource.kind || decision.resource.id !== request.facts.resource.id ||
      decision.facts_version !== request.facts.facts_version) return 'policy.decision_mismatch';
  if (decision.effect === 'deny') return 'policy.resource_not_public';
  const expected = referenceDecision(request);
  if (expected.effect !== 'allow') return 'policy.decision_mismatch';
  const current = { ...request, facts: currentFacts };
  if (!requestValid(current)) return 'policy.facts_changed';
  // Compare values as well as version: adapters must not accidentally accept a
  // state change whose version was not advanced. Explicit fields avoid key-order dependence.
  const before = request.facts;
  if (currentFacts.resource.kind !== before.resource.kind || currentFacts.resource.id !== before.resource.id ||
      currentFacts.owner.kind !== before.owner.kind || currentFacts.owner.id !== before.owner.id ||
      currentFacts.board.kind !== before.board.kind || currentFacts.board.id !== before.board.id ||
      currentFacts.facts_version !== before.facts_version || currentFacts.publication_status !== before.publication_status ||
      currentFacts.moderation_status !== before.moderation_status || currentFacts.deletion_status !== before.deletion_status) return 'policy.facts_changed';
  return referenceDecision(current).effect === 'allow' ? 'allow' : 'policy.facts_changed';
}
const results = [];
const names = new Set();
const lines = [
  'import { THREAD_PUBLIC_READ_CONTRACT, THREAD_PUBLIC_READ_POLICY } from "../src/index";',
  'import type { ThreadPublicReadRequestV1, ThreadPublicReadDecisionV1, ThreadPublicReadErrorCode } from "../src/index";',
  `const contract: ${JSON.stringify(contract)} = THREAD_PUBLIC_READ_CONTRACT;`,
  `const policy: ${JSON.stringify(policy)} = THREAD_PUBLIC_READ_POLICY;`,
  ...catalog.errors.map((x, i) => `const error${i}: ThreadPublicReadErrorCode = ${JSON.stringify(x.code)};`),
  '// @ts-expect-error unknown policy errors must not compile',
  'const unknown: ThreadPublicReadErrorCode = "policy.grant_all";',
];
for (const item of corpus.cases) {
  assert(!names.has(item.name), item.name); names.add(item.name);
  assert(['request', 'decision'].includes(item.target));
  const validate = item.target === 'request' ? requestValid : decisionValid;
  const before = JSON.stringify(item.value);
  assert.equal(validate(item.value), item.valid, item.name);
  assert.equal(JSON.stringify(item.value), before, `input mutated: ${item.name}`);
  if (item.target === 'request' && !item.valid) {
    const expected = item.name === 'unsupported-contract' ? 'policy.contract_unsupported' : 'policy.input_invalid';
    assert.equal(referenceDecision(item.value).error, expected, item.name);
  }
  if (item.valid || item.compile_reject) {
    if (!item.valid) lines.push('// @ts-expect-error deliberate malformed contract');
    lines.push(`const sample${lines.length}: ThreadPublicRead${item.target === 'request' ? 'Request' : 'Decision'}V1 = ${before};`);
  }
  results.push({ name: item.name, target: item.target, expected_valid: item.valid, passed: true });
}
const base = structuredClone(corpus.cases.find((x) => x.name === 'public-anonymous').value);
const principals = read('sdk/typescript/tests/principal-context-v1.fixtures.json').cases.filter((x) => x.valid);
let matrix = 0;
const relationships = { actual_owner: 0, admin_same_id: 0, delegated_owner: 0 };
for (const publication of ['draft', 'published', 'private']) {
  for (const moderation of ['clear', 'pending', 'rejected', 'taken_down']) {
    for (const deletion of ['active', 'trashed', 'purged']) {
      for (const principal of principals) {
        const request = structuredClone(base);
        request.principal = principal.context;
        const actor = request.principal.actor;
        if (actor.kind === 'user') {
          request.facts.owner.id = actor.id; relationships.actual_owner++;
        } else if (actor.kind === 'admin') {
          request.facts.owner.id = actor.id; relationships.admin_same_id++;
        } else if (request.principal.effective_subject) {
          request.facts.owner.id = request.principal.effective_subject.id; relationships.delegated_owner++;
        }
        Object.assign(request.facts, { publication_status: publication, moderation_status: moderation, deletion_status: deletion });
        const decision = referenceDecision(request);
        assert(decisionValid(decision));
        // The independent expected allow set has exactly one of the 36 tuples.
        const expected = [publication, moderation, deletion].join('/') === 'published/clear/active';
        assert.equal(decision.effect, expected ? 'allow' : 'deny');
        assert.equal(consume(request, decision, request.facts), expected ? 'allow' : 'policy.resource_not_public');
        matrix++;
      }
    }
  }
}
assert.deepEqual(relationships, { actual_owner: 72, admin_same_id: 72, delegated_owner: 72 });
const bindingResults = [];
function checkBinding(name, mutate, expected) {
  const request = structuredClone(base), decision = referenceDecision(request), current = structuredClone(request.facts);
  mutate(request, decision, current);
  assert.equal(consume(request, decision, current), expected, name);
  bindingResults.push({ name, expected, passed: true });
}
checkBinding('unchanged', () => {}, 'allow');
for (const field of ['request_id', 'facts_version', 'policy']) {
  checkBinding('wrong-decision-' + field, (_, d) => { d[field] += '-other'; }, 'policy.decision_mismatch');
}
checkBinding('wrong-decision-resource', (_, d) => { d.resource.id = 'other-thread'; }, 'policy.decision_mismatch');
checkBinding('missing-recheck', (_, d) => { d.obligations = []; }, 'policy.decision_mismatch');
for (const [field, value] of [['publication_status', 'private'], ['moderation_status', 'taken_down'], ['deletion_status', 'trashed'], ['facts_version', 'facts-2']]) {
  checkBinding('changed-' + field, (_, __, f) => { f[field] = value; }, 'policy.facts_changed');
}
for (const field of ['resource', 'owner', 'board']) {
  checkBinding('changed-' + field, (_, __, f) => { f[field].id = 'different'; }, 'policy.facts_changed');
}
checkBinding('missing-current-fact', (_, __, f) => { delete f.moderation_status; }, 'policy.facts_changed');
checkBinding('forged-allow-on-private', (r, _, f) => { r.facts.publication_status = f.publication_status = 'private'; }, 'policy.decision_mismatch');
checkBinding('fact-key-order-independent', (_, __, f) => { const kind = f.owner.kind; delete f.owner.kind; f.owner.kind = kind; }, 'allow');
const virtual = resolve(root, 'sdk/typescript/tests/__thread_policy_contract__.ts');
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['thread-public-read-policy', true]]) {
  const options = { noEmit: true, strict: true, exactOptionalPropertyTypes, skipLibCheck: true, types: [],
    target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, moduleResolution: ts.ModuleResolutionKind.Bundler };
  const host = ts.createCompilerHost(options), get = host.getSourceFile.bind(host);
  const source = lines.join('\n').replaceAll('../src/index', `../src/${entry}`);
  host.getSourceFile = (path, language, onError, fresh) => path === virtual ? ts.createSourceFile(path, source, language, true) : get(path, language, onError, fresh);
  const diagnostics = ts.getPreEmitDiagnostics(ts.createProgram([virtual], options, host));
  assert.equal(diagnostics.length, 0, ts.formatDiagnosticsWithColorAndContext(diagnostics, {
    getCanonicalFileName: (x) => x, getCurrentDirectory: () => root, getNewLine: () => '\n',
  }));
}
const report = {
  schema: 'campusos.v12-g1-thread-policy-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'G1 public-thread ResourceFacts/Decision schema and executable specification; not production authorization',
  implementation: 'passed', automation: 'passed', target_environment: `${process.platform} offline contract acceptance passed`,
  runtime_acceptance: 'pending V12-02a/02b; no HTTP, PG, credential, grant or concurrency integration claim',
  environment: { platform: process.platform, arch: process.arch, node: process.version, ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, state_principal_combinations: matrix, relationship_combinations: relationships, binding_checks: bindingResults,
  typescript_fixtures: corpus.cases.filter((x) => x.valid || x.compile_reject).length,
  files_sha256: Object.fromEntries([
    schemaPath, fixturePath, 'docs/api/policy-thread-public-read-v1.errors.json', 'docs/api/principal-context-v1.schema.json',
    'sdk/typescript/src/thread-public-read-policy.ts', 'sdk/typescript/src/principal.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/tests/principal-context-v1.fixtures.json', 'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-thread-policy-contract.mjs',
  ].map((p) => [p, createHash('sha256').update(readFileSync(resolve(root, p))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log(`Thread policy: ${results.length} schema fixtures, ${matrix} state/principal combinations, ${bindingResults.length} binding checks, ${report.typescript_fixtures} TypeScript fixtures passed`);
