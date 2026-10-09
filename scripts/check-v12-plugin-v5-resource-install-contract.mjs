#!/usr/bin/env node
// G1 offline executable specification; no network, container or host mutation occurs.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { isIP } from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const ts = require('typescript');
const read = (path) => JSON.parse(readFileSync(resolve(root, path), 'utf8'));
const resourcePath = 'docs/api/plugin-v5-host-resources-v1.schema.json';
const installPath = 'docs/api/plugin-v5-install-plan-v1.schema.json';
const errorsPath = 'docs/api/plugin-v5-resource-install-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
function compile(path) { const schema = read(path); assert(ajv.validateSchema(schema), path); return ajv.compile(schema); }
const validResources = compile(resourcePath), validInstall = compile(installPath);
const errors = read(errorsPath);
assert.equal(errors.contract, 'campusos.plugin-resource-install/v1');
assert.equal(errors.errors.length, 7);
const limits = { network_targets: ['https://api.example.test', 'https://models.example.test'],
  storage_bytes: 1000, cpu_millis: 500, memory_mb: 512, max_concurrency: 8, timeout_ms: 10000 };
const resource = { contract: 'campusos.plugin-host-resources/v1', requested: limits,
  host_ceiling: { ...limits, storage_bytes: 800, cpu_millis: 400, max_concurrency: 4 },
  admin_grant: { ...limits, storage_bytes: 600, memory_mb: 256, network_targets: ['https://api.example.test'] },
  grant_status: 'granted', grant_expires_at_ms: 2000, now_ms: 1000 };
const digest = 'sha256:' + 'a'.repeat(64);
const fingerprint = { source: digest, dependencies: digest, binary: digest, frontends: digest, schema: digest };
const install = { contract: 'campusos.plugin-install-plan/v1', plugin_key: 'example-plugin',
  mutations: ['plugin_package', 'plugin_config', 'plugin_data', 'installation_record', 'grant_record', 'operation_record', 'audit_record']
    .map((kind) => ({ kind, namespace: 'example-plugin' })),
  host_before: { ...fingerprint }, host_after: { ...fingerprint } };
function safeNetwork(target) {
  try {
    const url = new URL(target);
    const host = url.hostname;
    return url.protocol === 'https:' && url.origin === target &&
      !url.username && !url.password && !isIP(host) && host.includes('.') &&
      host !== 'localhost' && !host.endsWith('.localhost') &&
      /^[a-z0-9-]+(?:\.[a-z0-9-]+)+$/.test(host) && !host.includes('--');
  } catch { return false; }
}
const metrics = ['storage_bytes', 'cpu_millis', 'memory_mb', 'max_concurrency', 'timeout_ms'];
function decideResources(input) {
  if (!validResources(input)) return { error: 'plugin.resource_request_invalid' };
  if (input.host_ceiling.network_targets.some((x) => !safeNetwork(x)))
    return { error: 'plugin.resource_policy_unavailable' };
  if (input.requested.network_targets.some((x) => !safeNetwork(x)) ||
      input.admin_grant.network_targets.some((x) => !safeNetwork(x)))
    return { error: 'plugin.network_target_forbidden' };
  if (input.grant_status !== 'granted') return { error: 'plugin.resource_grant_missing' };
  if (input.now_ms >= input.grant_expires_at_ms) return { error: 'plugin.resource_grant_expired' };
  const effective = Object.fromEntries(metrics.map((key) =>
    [key, Math.min(input.requested[key], input.host_ceiling[key], input.admin_grant[key])]));
  effective.network_targets = input.requested.network_targets.filter((target) =>
    input.host_ceiling.network_targets.includes(target) && input.admin_grant.network_targets.includes(target));
  return { effective };
}
function decideInstall(plan) {
  if (!validInstall(plan)) return 'plugin.install_mutation_forbidden';
  if (plan.mutations.some((item) => item.namespace !== plan.plugin_key))
    return 'plugin.install_mutation_forbidden';
  if (Object.keys(plan.host_before).some((key) => plan.host_before[key] !== plan.host_after[key]))
    return 'plugin.host_fingerprint_changed';
  return null;
}
const clone = (x) => structuredClone(x);
const altered = (base, mutate) => { const x = clone(base); mutate(x); return x; };
const cases = [
  ['resource-intersection', resource, null],
  ['grant-revoked', altered(resource, (x) => { x.grant_status = 'revoked'; }), 'plugin.resource_grant_missing'],
  ['grant-expired', altered(resource, (x) => { x.now_ms = 2000; }), 'plugin.resource_grant_expired'],
  ['grant-over-policy', altered(resource, (x) => { x.admin_grant.memory_mb = 1024; }), null],
  ['requested-over-policy', altered(resource, (x) => { x.requested.storage_bytes = 2000; }), null],
  ['network-subset', altered(resource, (x) => { x.admin_grant.network_targets = []; }), null],
  ['network-http', altered(resource, (x) => { x.requested.network_targets = ['http://api.example.test']; }), 'plugin.network_target_forbidden'],
  ['network-wildcard', altered(resource, (x) => { x.requested.network_targets = ['https://*.example.test']; }), 'plugin.network_target_forbidden'],
  ['network-loopback', altered(resource, (x) => { x.requested.network_targets = ['https://127.0.0.1']; }), 'plugin.network_target_forbidden'],
  ['network-localhost', altered(resource, (x) => { x.requested.network_targets = ['https://localhost']; }), 'plugin.network_target_forbidden'],
  ['network-credentials', altered(resource, (x) => { x.requested.network_targets = ['https://u:p@api.example.test']; }), 'plugin.network_target_forbidden'],
  ['network-path', altered(resource, (x) => { x.requested.network_targets = ['https://api.example.test/private']; }), 'plugin.network_target_forbidden'],
  ['ceiling-bad-target', altered(resource, (x) => { x.host_ceiling.network_targets = ['https://localhost']; }), 'plugin.resource_policy_unavailable'],
  ['privileged-field', altered(resource, (x) => { x.requested.privileged = true; }), 'plugin.resource_request_invalid'],
  ['db-connection-field', altered(resource, (x) => { x.requested.database_url = 'postgres://host/db'; }), 'plugin.resource_request_invalid'],
  ['zero-memory', altered(resource, (x) => { x.requested.memory_mb = 0; }), 'plugin.resource_request_invalid'],
];
const results = [];
for (const [name, input, expected] of cases) {
  const before = JSON.stringify(input), result = decideResources(input);
  assert.equal(result.error ?? null, expected, name);
  assert.equal(JSON.stringify(input), before, 'mutation: ' + name);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
assert.deepEqual(decideResources(resource).effective, {
  storage_bytes: 600, cpu_millis: 400, memory_mb: 256, max_concurrency: 4,
  timeout_ms: 10000, network_targets: ['https://api.example.test'],
});
let limitCombinations = 0;
for (const key of metrics)
for (const low of [5, 10, 20])
for (const [request, ceiling, grant] of [[low, 50, 70], [70, low, 50], [70, 50, low]]) {
  const input = clone(resource);
  input.requested[key] = request; input.host_ceiling[key] = ceiling; input.admin_grant[key] = grant;
  assert.equal(decideResources(input).effective[key], low, key);
  limitCombinations++;
}
const installCases = [
  ['allowed', install, null],
  ['host-source', altered(install, (x) => { x.mutations.push({ kind: 'host_source', namespace: x.plugin_key }); }), 'plugin.install_mutation_forbidden'],
  ['host-lockfile', altered(install, (x) => { x.mutations.push({ kind: 'dependency_lockfile', namespace: x.plugin_key }); }), 'plugin.install_mutation_forbidden'],
  ['core-table', altered(install, (x) => { x.mutations.push({ kind: 'core_schema', namespace: x.plugin_key }); }), 'plugin.install_mutation_forbidden'],
  ['shell-hook', altered(install, (x) => { x.install_hook = 'npm install'; }), 'plugin.install_mutation_forbidden'],
  ['other-namespace', altered(install, (x) => { x.mutations[0].namespace = 'other-plugin'; }), 'plugin.install_mutation_forbidden'],
  ['changed-source', altered(install, (x) => { x.host_after.source = 'sha256:' + 'b'.repeat(64); }), 'plugin.host_fingerprint_changed'],
  ['changed-binary', altered(install, (x) => { x.host_after.binary = 'sha256:' + 'b'.repeat(64); }), 'plugin.host_fingerprint_changed'],
  ['changed-schema', altered(install, (x) => { x.host_after.schema = 'sha256:' + 'b'.repeat(64); }), 'plugin.host_fingerprint_changed'],
];
for (const [name, plan, expected] of installCases) {
  const before = JSON.stringify(plan);
  assert.equal(decideInstall(plan), expected, name);
  assert.equal(JSON.stringify(plan), before);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__plugin_v5_resources__.ts');
const lines = [
  'import type { PluginV5ResourceDecisionInput, PluginV5InstallPlan, PluginV5InstallMutationKind, PluginV5ResourceInstallErrorCode } from "../src/index";',
  'const resource: PluginV5ResourceDecisionInput = ' + JSON.stringify(resource) + ';',
  'const install: PluginV5InstallPlan = ' + JSON.stringify(install) + ';',
  '// @ts-expect-error host source is forbidden',
  'const badKind: PluginV5InstallMutationKind = "host_source";',
  '// @ts-expect-error resource permission is not an install grant',
  'const badError: PluginV5ResourceInstallErrorCode = "plugin.allow";',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['plugin-v5-resource-install', true]]) {
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
const report = { schema: 'campusos.v12-g1-plugin-resource-install-acceptance/v1',
  generated_at: new Date().toISOString(), scope: 'G1 resource ceiling and non-invasive install target contracts',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 01b/03a/03b/05/06/08a: no real Egress, runner isolation, filesystem mutation or host fingerprint accepted',
  environment: { platform: process.platform, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, numeric_limit_combinations: limitCombinations,
  files_sha256: Object.fromEntries([resourcePath, installPath, errorsPath,
    'sdk/typescript/src/plugin-v5-resource-install.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-plugin-v5-resource-install-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Plugin v5 resource/install: ' + results.length + ' fixtures, ' + limitCombinations + ' numeric intersections, 2/2 TS positive/negative types passed');
