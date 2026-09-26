#!/usr/bin/env node
// G1 offline executable specification. It does not validate package bytes, signatures or permissions.
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
const shapePath = 'docs/api/plugin-v5-package-shape-v1.schema.json';
const releasePath = 'docs/api/plugin-v5-release-v1.schema.json';
const errorPath = 'docs/api/plugin-v5-package-shape-v1.errors.json';
const shapeSchema = read(shapePath);
const releaseSchema = read(releasePath);
const errorCatalog = read(errorPath);
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
assert.equal(ajv.validateSchema(shapeSchema), true);
assert.equal(ajv.validateSchema(releaseSchema), true);
const validShape = ajv.compile(shapeSchema);
const validRelease = ajv.compile(releaseSchema);
assert.equal(errorCatalog.contract, 'campusos.plugin-shape/v1');
assert.deepEqual(errorCatalog.errors.map((e) => e.code), [
  'plugin.shape_invalid', 'plugin.host_contract_unsupported', 'plugin.artifact_mismatch',
  'plugin.contribution_empty', 'plugin.duplicate_contribution',
  'plugin.release_identity_mismatch', 'plugin.release_digest_conflict',
]);
const digest = (char) => 'sha256:' + char.repeat(64);
const ui = { audience: 'user', entrypoint: 'ui/index.html', digest: digest('a') };
const wasm = { artifact: { kind: 'wasm', path: 'backend/module.wasm', digest: digest('b') }, transport: 'host-call' };
const container = { artifact: { kind: 'container', image: 'registry.example/plugin@' + digest('c') }, transport: 'grpc' };
const provider = { id: 'chat-main', point: 'ai.chat', contract_version: 'v1' };
const base = { api_version: 'campusos.plugin/v5', publisher: 'campusos', key: 'example-plugin',
  version: '1.0.0', host_contracts: ['campusos.host/v1'], runtime: 'none', ui: [ui], provides: [] };
const release = { publisher: base.publisher, key: base.key, version: base.version,
  package_digest: digest('d'), signature: { key_id: 'test-key', value: 'test-signature' } };
const supportedHost = ['campusos.host/v1'];
const registry = new Map();
function check(manifest, envelope = release, host = supportedHost, existing = registry) {
  if (!validShape(manifest) || !validRelease(envelope)) return 'plugin.shape_invalid';
  if (!manifest.host_contracts.some((v) => host.includes(v))) return 'plugin.host_contract_unsupported';
  if (manifest.runtime !== 'none' && manifest.backend.artifact.kind !== manifest.runtime) return 'plugin.artifact_mismatch';
  if (manifest.runtime === 'none' && manifest.provides.length) return 'plugin.artifact_mismatch';
  if (manifest.runtime !== 'none' && !manifest.provides.length) return 'plugin.contribution_empty';
  if (!manifest.ui?.length && !manifest.provides.length) return 'plugin.contribution_empty';
  const audiences = manifest.ui?.map((item) => item.audience) ?? [];
  const ids = manifest.provides.map((item) => item.id);
  if (new Set(audiences).size !== audiences.length || new Set(ids).size !== ids.length)
    return 'plugin.duplicate_contribution';
  if (['publisher', 'key', 'version'].some((field) => manifest[field] !== envelope[field]))
    return 'plugin.release_identity_mismatch';
  const identity = envelope.publisher + '/' + envelope.key + '@' + envelope.version;
  if (existing.has(identity) && existing.get(identity) !== envelope.package_digest)
    return 'plugin.release_digest_conflict';
  return null;
}
function variant(mutate, source = base) {
  const value = structuredClone(source);
  mutate(value);
  return value;
}
const headless = { ...structuredClone(base), runtime: 'wasm', backend: wasm, provides: [provider] };
delete headless.ui;
const combined = { ...structuredClone(headless), ui: [ui] };
const containerOnly = { ...structuredClone(headless), runtime: 'container', backend: container };
const cases = [
  ['ui-only', base, release, supportedHost, new Map(), null],
  ['wasm-headless', headless, release, supportedHost, new Map(), null],
  ['container-headless', containerOnly, release, supportedHost, new Map(), null],
  ['combined', combined, release, supportedHost, new Map(), null],
  ['unknown-package-version', variant((m) => { m.api_version = 'campusos.plugin/v4'; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['no-ui-no-backend', variant((m) => { delete m.ui; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['backend-missing', variant((m) => { m.runtime = 'wasm'; delete m.ui; m.provides = [provider]; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['none-with-backend', variant((m) => { m.backend = wasm; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['headless-empty', variant((m) => { m.runtime = 'wasm'; m.backend = wasm; delete m.ui; }), release, supportedHost, new Map(), 'plugin.contribution_empty'],
  ['ui-only-provider', variant((m) => { m.provides = [provider]; }), release, supportedHost, new Map(), 'plugin.artifact_mismatch'],
  ['runtime-artifact-mismatch', { ...headless, runtime: 'container' }, release, supportedHost, new Map(), 'plugin.artifact_mismatch'],
  ['unsupported-host', base, release, ['campusos.host/v2'], new Map(), 'plugin.host_contract_unsupported'],
  ['unknown-host-version', variant((m) => { m.host_contracts = ['campusos.host/v0']; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['duplicate-host-version', variant((m) => { m.host_contracts.push('campusos.host/v1'); }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['duplicate-ui-audience', variant((m) => { m.ui.push({ ...ui }); }), release, supportedHost, new Map(), 'plugin.duplicate_contribution'],
  ['duplicate-provider-id', variant((m) => { m.runtime = 'wasm'; m.backend = wasm; m.provides = [provider, { ...provider }]; }), release, supportedHost, new Map(), 'plugin.duplicate_contribution'],
  ['unknown-point', variant((m) => { m.runtime = 'wasm'; m.backend = wasm; m.provides = [{ ...provider, point: 'embedding' }]; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['unsupported-point-version', variant((m) => { m.runtime = 'wasm'; m.backend = wasm; m.provides = [{ ...provider, contract_version: 'v2' }]; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['path-traversal', variant((m) => { m.ui[0].entrypoint = '../host.js'; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['absolute-path', variant((m) => { m.ui[0].entrypoint = '/host.js'; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['invalid-digest', variant((m) => { m.ui[0].digest = 'sha256:short'; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['floating-image', { ...containerOnly, backend: { ...container, artifact: { kind: 'container', image: 'registry.example/plugin:latest' } } }, release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['install-hook', variant((m) => { m.install_script = 'shell'; }), release, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['release-unsigned', base, { ...release, signature: undefined }, supportedHost, new Map(), 'plugin.shape_invalid'],
  ['release-identity', base, { ...release, key: 'different-plugin' }, supportedHost, new Map(), 'plugin.release_identity_mismatch'],
  ['same-version-different-digest', base, { ...release, package_digest: digest('e') }, supportedHost,
    new Map([['campusos/example-plugin@1.0.0', digest('d')]]), 'plugin.release_digest_conflict'],
  ['same-version-same-digest', base, release, supportedHost,
    new Map([['campusos/example-plugin@1.0.0', digest('d')]]), null],
];
const fixtureResults = [];
const names = new Set();
for (const [name, manifest, envelope, host, existing, expected] of cases) {
  assert(!names.has(name), name); names.add(name);
  const before = JSON.stringify([manifest, envelope, host, [...existing]]);
  assert.equal(check(manifest, envelope, host, existing), expected, name);
  assert.equal(JSON.stringify([manifest, envelope, host, [...existing]]), before, 'input mutation ' + name);
  fixtureResults.push({ name, expected: expected ?? 'accepted', passed: true });
}
let combinations = 0;
for (const runtime of ['none', 'wasm', 'container'])
for (const hasUi of [false, true])
for (const hasProvider of [false, true])
for (const artifact of ['wasm', 'container'])
for (const hostSupported of [false, true]) {
  const m = structuredClone(base);
  m.runtime = runtime;
  if (!hasUi) delete m.ui;
  if (hasProvider) m.provides = [provider];
  if (runtime !== 'none') m.backend = artifact === 'wasm' ? wasm : container;
  const actual = check(m, release, hostSupported ? supportedHost : ['campusos.host/v2'], new Map());
  const expectedAccept = hostSupported && (
    runtime === 'none' ? hasUi && !hasProvider : hasProvider && runtime === artifact);
  assert.equal(actual === null, expectedAccept, [runtime, hasUi, hasProvider, artifact, hostSupported].join('/'));
  combinations++;
}
const virtual = resolve(root, 'sdk/typescript/tests/__plugin_v5_shape__.ts');
const lines = [
  'import { PLUGIN_V5_API_VERSION } from "../src/index";',
  'import type { PluginV5PackageShape, PluginV5ReleaseEnvelope, PluginV5Runtime, PluginV5ExtensionPoint, PluginV5ShapeErrorCode } from "../src/index";',
  'const version: "campusos.plugin/v5" = PLUGIN_V5_API_VERSION;',
  'const envelope: PluginV5ReleaseEnvelope = ' + JSON.stringify(release) + ';',
  ...[base, headless, containerOnly, combined].map((m, i) =>
    'const valid' + i + ': PluginV5PackageShape = ' + JSON.stringify(m) + ';'),
  '// @ts-expect-error transport is not a deployment runtime',
  'const badRuntime: PluginV5Runtime = "grpc";',
  '// @ts-expect-error unpublished point is not supported',
  'const badPoint: PluginV5ExtensionPoint = "embedding";',
  '// @ts-expect-error unknown error code',
  'const badError: PluginV5ShapeErrorCode = "plugin.authorized";',
  '// @ts-expect-error backend required for headless runtime',
  'const badHeadless: PluginV5PackageShape = { ...valid0, runtime: "wasm", ui: undefined };',
  '// @ts-expect-error runtime and artifact kind must match',
  'const badPair: PluginV5PackageShape = { ...valid1, runtime: "container" };',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['plugin-v5-package-shape', true]]) {
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
const report = {
  schema: 'campusos.v12-g1-plugin-v5-shape-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'G1 package shape, immutable release identity and host version negotiation projection',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 03a/03b/05/06/08a: no package bytes, signature trust, install, runtime, host resources or UI claimed',
  environment: { platform: process.platform, arch: process.arch, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: fixtureResults, shape_combinations: combinations, typescript_positive_shapes: 4,
  typescript_negative_types: 5, typescript_modes: ['public strict', 'isolated exactOptionalPropertyTypes'],
  files_sha256: Object.fromEntries([shapePath, releasePath, errorPath,
    'sdk/typescript/src/plugin-v5-package-shape.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'docs/api/v1.2插件v5包形态与版本协商合同.md', 'Makefile',
    'scripts/check-v12-plugin-v5-shape-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Plugin v5 shape: ' + fixtureResults.length + ' fixtures, ' + combinations +
  ' shape combinations, 4/5 TS positive/negative types passed');
