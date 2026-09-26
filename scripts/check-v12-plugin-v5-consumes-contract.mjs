#!/usr/bin/env node
// G1 offline executable contract. No plugin package, grant, user consent or Host API call is authorized here.
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
const consumesPath = 'docs/api/plugin-v5-consumes-v1.schema.json';
const catalogPath = 'docs/api/plugin-v5-host-catalog-v1.schema.json';
const shapePath = 'docs/api/plugin-v5-package-shape-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/plugin-v5-host-catalog.fixture.json';
const errorsPath = 'docs/api/plugin-v5-consumes-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
const compile = (path) => {
  const schema = read(path);
  assert.equal(ajv.validateSchema(schema), true, path + ' schema invalid');
  return ajv.compile(schema);
};
const validConsumes = compile(consumesPath);
const validCatalog = compile(catalogPath);
const validShape = compile(shapePath);
const catalog = read(fixturePath);
const errors = read(errorsPath);
assert.equal(errors.contract, 'campusos.plugin-consumes/v1');
assert.deepEqual(errors.errors.map((x) => [x.code, x.http_status, x.retryable]), [
  ['plugin.consumes_invalid', 400, false],
  ['plugin.host_contract_unsupported', 400, false],
  ['plugin.catalog_unavailable', 503, true],
  ['plugin.capability_unpublished', 400, false],
  ['plugin.scope_unsupported', 400, false],
  ['plugin.scope_target_unavailable', 400, false],
  ['plugin.duplicate_capability', 400, false],
]);
const digest = 'sha256:' + 'a'.repeat(64);
const manifest = {
  api_version: 'campusos.plugin/v5', publisher: 'campusos', key: 'consumer-plugin',
  version: '1.0.0', host_contracts: ['campusos.host/v1'], runtime: 'none',
  ui: [{ audience: 'user', entrypoint: 'ui/index.html', digest }], provides: [],
};
assert(validShape(manifest), 'G1 shape projection drifted');
assert(validCatalog(catalog), 'synthetic host catalog fixture invalid');
const declaration = (capability, scope, required = true) =>
  ({ capability, scope, purpose: '用于受管功能', required });
const publicRead = declaration('community.thread.public.read', { kind: 'public' });
const selfRead = declaration('plugin.records.self.read', { kind: 'self' });
const collectionSearch = declaration('knowledge.collection.public.search',
  { kind: 'collection', collection_ids: ['collection-public-a'] });
const configRead = declaration('plugin.config.system.read',
  { kind: 'system_config', keys: ['display.locale'] });
function evaluate(shape, consumes, hostCatalog) {
  if (!validShape(shape) || !validConsumes(consumes)) return 'plugin.consumes_invalid';
  if (!validCatalog(hostCatalog) ||
      new Set(hostCatalog.items.map((x) => x.code)).size !== hostCatalog.items.length)
    return 'plugin.catalog_unavailable';
  if (!shape.host_contracts.includes(hostCatalog.host_contract)) return 'plugin.host_contract_unsupported';
  if (new Set(consumes.map((x) => x.capability)).size !== consumes.length)
    return 'plugin.duplicate_capability';
  const byCode = new Map(hostCatalog.items.map((x) => [x.code, x]));
  for (const requested of consumes) {
    const descriptor = byCode.get(requested.capability);
    if (!descriptor) return 'plugin.capability_unpublished';
    if (descriptor.scope_kind !== requested.scope.kind) return 'plugin.scope_unsupported';
    if (requested.scope.kind === 'collection' &&
        !requested.scope.collection_ids.every((id) => descriptor.available_public_collection_ids.includes(id)))
      return 'plugin.scope_target_unavailable';
    if (requested.scope.kind === 'system_config' &&
        !requested.scope.keys.every((key) => descriptor.available_config_keys.includes(key)))
      return 'plugin.scope_target_unavailable';
  }
  return null;
}
const copy = (x) => structuredClone(x);
const variant = (mutate, source = catalog) => { const value = copy(source); mutate(value); return value; };
const cases = [
  ['empty-ui-only', [], catalog, manifest, null],
  ['public-read', [publicRead], catalog, manifest, null],
  ['self-managed-record', [selfRead], catalog, manifest, null],
  ['public-collection', [collectionSearch], catalog, manifest, null],
  ['plugin-config', [configRead], catalog, manifest, null],
  ['all-four', [publicRead, selfRead, collectionSearch, configRead], catalog, manifest, null],
  ['unknown-required', [declaration('host.db.connect', { kind: 'public' })], catalog, manifest, 'plugin.capability_unpublished'],
  ['unknown-optional', [declaration('host.db.connect', { kind: 'public' }, false)], catalog, manifest, 'plugin.capability_unpublished'],
  ['public-as-self', [declaration(publicRead.capability, { kind: 'self' })], catalog, manifest, 'plugin.scope_unsupported'],
  ['self-as-public', [declaration(selfRead.capability, { kind: 'public' })], catalog, manifest, 'plugin.scope_unsupported'],
  ['collection-as-public', [declaration(collectionSearch.capability, { kind: 'public' })], catalog, manifest, 'plugin.scope_unsupported'],
  ['config-as-collection', [declaration(configRead.capability, { kind: 'collection', collection_ids: ['collection-public-a'] })], catalog, manifest, 'plugin.scope_unsupported'],
  ['private-collection', [declaration(collectionSearch.capability, { kind: 'collection', collection_ids: ['collection-private'] })], catalog, manifest, 'plugin.scope_target_unavailable'],
  ['mixed-public-private', [declaration(collectionSearch.capability, { kind: 'collection', collection_ids: ['collection-public-a', 'collection-private'] })], catalog, manifest, 'plugin.scope_target_unavailable'],
  ['secret-config-key', [declaration(configRead.capability, { kind: 'system_config', keys: ['secret.token'] })], catalog, manifest, 'plugin.scope_target_unavailable'],
  ['duplicate-capability', [publicRead, { ...publicRead, required: false }], catalog, manifest, 'plugin.duplicate_capability'],
  ['duplicate-collection-id', [declaration(collectionSearch.capability, { kind: 'collection', collection_ids: ['collection-public-a', 'collection-public-a'] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['duplicate-config-key', [declaration(configRead.capability, { kind: 'system_config', keys: ['display.locale', 'display.locale'] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['self-claims-user-id', [declaration(selfRead.capability, { kind: 'self', user_id: 'other' })], catalog, manifest, 'plugin.consumes_invalid'],
  ['public-claims-target', [declaration(publicRead.capability, { kind: 'public', collection_ids: ['collection-public-a'] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['wildcard-collection', [declaration(collectionSearch.capability, { kind: 'collection', collection_ids: ['*'] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['wildcard-config', [declaration(configRead.capability, { kind: 'system_config', keys: ['*'] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['empty-collection', [declaration(collectionSearch.capability, { kind: 'collection', collection_ids: [] })], catalog, manifest, 'plugin.consumes_invalid'],
  ['empty-purpose', [{ ...publicRead, purpose: '   ' }], catalog, manifest, 'plugin.consumes_invalid'],
  ['missing-required', [{ capability: publicRead.capability, scope: publicRead.scope, purpose: publicRead.purpose }], catalog, manifest, 'plugin.consumes_invalid'],
  ['extra-grant', [{ ...publicRead, admin_grant: true }], catalog, manifest, 'plugin.consumes_invalid'],
  ['unknown-scope', [declaration(publicRead.capability, { kind: 'system' })], catalog, manifest, 'plugin.consumes_invalid'],
  ['wrong-host-version', [publicRead], catalog, { ...manifest, host_contracts: ['campusos.host/v2'] }, 'plugin.host_contract_unsupported'],
  ['catalog-bad-version', [publicRead], variant((c) => { c.contract = 'campusos.host-capabilities/v2'; }), manifest, 'plugin.catalog_unavailable'],
  ['catalog-duplicate-code', [publicRead], variant((c) => { c.items.push(copy(c.items[0])); }), manifest, 'plugin.catalog_unavailable'],
  ['catalog-private-collection', [collectionSearch], variant((c) => { c.items[2].data_classification = 'restricted'; }), manifest, 'plugin.catalog_unavailable'],
  ['catalog-self-without-consent', [selfRead], variant((c) => { c.items[1].consent_required = false; }), manifest, 'plugin.catalog_unavailable'],
  ['catalog-public-with-consent', [publicRead], variant((c) => { c.items[0].consent_required = true; }), manifest, 'plugin.catalog_unavailable'],
  ['catalog-config-with-secret-class', [configRead], variant((c) => { c.items[3].data_classification = 'restricted'; }), manifest, 'plugin.catalog_unavailable'],
];
const results = [], names = new Set();
for (const [name, consumes, hostCatalog, shape, expected] of cases) {
  assert(!names.has(name), name); names.add(name);
  const before = JSON.stringify([consumes, hostCatalog, shape]);
  assert.equal(evaluate(shape, consumes, hostCatalog), expected, name);
  assert.equal(JSON.stringify([consumes, hostCatalog, shape]), before, 'input mutation: ' + name);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
const scopeByKind = {
  public: { kind: 'public' },
  self: { kind: 'self' },
  collection: { kind: 'collection', collection_ids: ['collection-public-a'] },
  system_config: { kind: 'system_config', keys: ['display.locale'] },
};
const codeByKind = {
  public: publicRead.capability, self: selfRead.capability,
  collection: collectionSearch.capability, system_config: configRead.capability,
};
let combinations = 0;
for (const capKind of Object.keys(codeByKind))
for (const scopeKind of Object.keys(scopeByKind))
for (const validTarget of [false, true])
for (const validHost of [false, true]) {
  const scope = copy(scopeByKind[scopeKind]);
  if (!validTarget && scopeKind === 'collection') scope.collection_ids = ['collection-private'];
  if (!validTarget && scopeKind === 'system_config') scope.keys = ['secret.token'];
  if (!validTarget && scopeKind === 'public') scope.collection_ids = ['collection-public-a'];
  if (!validTarget && scopeKind === 'self') scope.user_id = 'other';
  const shape = validHost ? manifest : { ...manifest, host_contracts: ['campusos.host/v2'] };
  const actual = evaluate(shape, [declaration(codeByKind[capKind], scope)], catalog);
  const expectedAccept = validHost && validTarget && capKind === scopeKind;
  assert.equal(actual === null, expectedAccept,
    [capKind, scopeKind, validTarget, validHost].join('/'));
  combinations++;
}
const rechecks = [];
for (const [name, request, mutate, expected] of [
  ['collection-removed', [collectionSearch], (c) => { c.items[2].available_public_collection_ids = ['collection-public-b']; }, 'plugin.scope_target_unavailable'],
  ['config-key-removed', [configRead], (c) => { c.items[3].available_config_keys = ['display.theme']; }, 'plugin.scope_target_unavailable'],
  ['capability-unpublished', [publicRead], (c) => { c.items.shift(); }, 'plugin.capability_unpublished'],
]) {
  assert.equal(evaluate(manifest, request, catalog), null, name + ' precondition');
  const changed = variant(mutate);
  assert.equal(evaluate(manifest, request, changed), expected, name);
  rechecks.push({ name, expected, passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__plugin_v5_consumes__.ts');
const lines = [
  'import type { PluginV5Consume, PluginV5ConsumeScope, PluginV5HostCatalog, PluginV5ConsumesErrorCode } from "../src/index";',
  ...[publicRead, selfRead, collectionSearch, configRead].map((x, i) =>
    'const accepted' + i + ': PluginV5Consume = ' + JSON.stringify(x) + ';'),
  'const catalog: PluginV5HostCatalog = ' + JSON.stringify(catalog) + ';',
  '// @ts-expect-error self scope cannot claim another user',
  'const badSelf: PluginV5ConsumeScope = { kind: "self", user_id: "other" };',
  '// @ts-expect-error collection scope requires explicit IDs',
  'const badCollection: PluginV5ConsumeScope = { kind: "collection" };',
  '// @ts-expect-error unknown scope kind',
  'const badScope: PluginV5ConsumeScope = { kind: "system" };',
  '// @ts-expect-error self catalog capability requires user consent',
  'const badCatalog: PluginV5HostCatalog = { contract: "campusos.host-capabilities/v1", host_contract: "campusos.host/v1", items: [{ code: "x.y.z", scope_kind: "self", data_classification: "sensitive", consent_required: false }] };',
  '// @ts-expect-error unknown error code',
  'const badError: PluginV5ConsumesErrorCode = "plugin.granted";',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['plugin-v5-consumes', true]]) {
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
  schema: 'campusos.v12-g1-plugin-v5-consumes-acceptance/v1',
  generated_at: new Date().toISOString(),
  scope: 'G1 v5 consumes projection and host-owned catalog scope negotiation',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 03a/03b/14c: no real catalog publication, Admin Grant, User Consent, Host API call or private source acceptance claimed',
  environment: { platform: process.platform, arch: process.arch, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, scope_combinations: combinations, catalog_rechecks: rechecks,
  typescript_positive_declarations: 4, typescript_negative_types: 5,
  files_sha256: Object.fromEntries([consumesPath, catalogPath, shapePath, fixturePath, errorsPath,
    'sdk/typescript/src/plugin-v5-consumes.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'docs/api/v1.2插件v5消费能力与范围合同.md', 'Makefile',
    'scripts/check-v12-plugin-v5-consumes-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Plugin v5 consumes: ' + results.length + ' fixtures, ' + combinations +
  ' scope combinations, ' + rechecks.length + ' catalog rechecks, 4/5 TS positive/negative types passed');
