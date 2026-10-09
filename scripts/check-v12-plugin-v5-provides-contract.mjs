#!/usr/bin/env node
// G1 offline contract acceptance only. No contribution is installed, selected or invoked.
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
const providesPath = 'docs/api/plugin-v5-provides-v1.schema.json';
const catalogPath = 'docs/api/plugin-v5-extension-catalog-v1.schema.json';
const dtoPath = 'docs/api/plugin-v5-extension-dtos-v1.schema.json';
const shapePath = 'docs/api/plugin-v5-package-shape-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json';
const errorsPath = 'docs/api/plugin-v5-provides-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
function compile(path) {
  const schema = read(path);
  assert.equal(ajv.validateSchema(schema), true, path);
  return ajv.compile(schema);
}
const validProvides = compile(providesPath);
const validCatalog = compile(catalogPath);
const validShape = compile(shapePath);
const dtoSchema = read(dtoPath);
assert.equal(ajv.validateSchema(dtoSchema), true);
const catalog = read(fixturePath);
const errorCatalog = read(errorsPath);
assert.equal(errorCatalog.contract, 'campusos.plugin-provides/v1');
assert.deepEqual(errorCatalog.errors.map((e) => [e.code, e.http_status, e.retryable]), [
  ['plugin.provides_invalid', 400, false],
  ['plugin.extension_catalog_unavailable', 503, true],
  ['plugin.extension_point_unsupported', 400, false],
  ['plugin.extension_schema_mismatch', 400, false],
  ['plugin.extension_capability_unsupported', 400, false],
  ['plugin.extension_data_mode_unsupported', 400, false],
  ['plugin.contribution_shape_mismatch', 400, false],
  ['plugin.duplicate_contribution', 400, false],
]);
const known = {
  'preview.renderer': { input_schema: 'campusos.preview.renderer.request/v1',
    output_schema: 'campusos.preview.renderer.response/v1', capabilities: ['pdf'],
    data_modes: ['authorized_resource'], input_key: 'preview_renderer_request', output_key: 'preview_renderer_response' },
  'ai.chat': { input_schema: 'campusos.ai.chat.request/v1',
    output_schema: 'campusos.ai.chat.response/v1', capabilities: ['text'],
    data_modes: ['public_only', 'no_host_private_data'], input_key: 'ai_chat_request', output_key: 'ai_chat_response' },
  'knowledge.source': { input_schema: 'campusos.knowledge.source.request/v1',
    output_schema: 'campusos.knowledge.source.response/v1', capabilities: ['public_text'],
    data_modes: ['public_only'], input_key: 'knowledge_source_request', output_key: 'knowledge_source_response' },
};
for (const [point, def] of Object.entries(known)) {
  assert(dtoSchema.definitions[def.input_key], point + ' input DTO missing');
  assert(dtoSchema.definitions[def.output_key], point + ' output DTO missing');
}
function catalogValid(snapshot) {
  if (!validCatalog(snapshot) || new Set(snapshot.points.map((x) => x.point)).size !== snapshot.points.length)
    return false;
  for (const entry of snapshot.points) {
    const target = known[entry.point];
    if (!target || entry.contract_version !== 'v1' ||
        entry.input_schema !== target.input_schema || entry.output_schema !== target.output_schema ||
        entry.capabilities.length !== target.capabilities.length ||
        entry.capabilities.some((x) => !target.capabilities.includes(x)) ||
        entry.data_modes.length !== target.data_modes.length ||
        entry.data_modes.some((x) => !target.data_modes.includes(x))) return false;
  }
  return true;
}
assert(catalogValid(catalog), 'synthetic extension catalog drifted');
const digest = 'sha256:' + 'b'.repeat(64);
const wasm = { artifact: { kind: 'wasm', path: 'backend/provider.wasm', digest }, transport: 'host-call' };
const provider = (id, point, data_mode, capabilities) => ({
  id, point, contract_version: 'v1', input_schema: known[point].input_schema,
  output_schema: known[point].output_schema, capabilities, data_mode,
});
const preview = provider('pdf-main', 'preview.renderer', 'authorized_resource', ['pdf']);
const chat = provider('chat-main', 'ai.chat', 'public_only', ['text']);
const source = provider('source-main', 'knowledge.source', 'public_only', ['public_text']);
const all = [preview, chat, source];
function shapeFor(items = all) {
  return { api_version: 'campusos.plugin/v5', publisher: 'campusos', key: 'provider-example',
    version: '1.0.0', host_contracts: ['campusos.host/v1'], runtime: 'wasm', backend: wasm,
    provides: items.map(({ id, point, contract_version }) => ({ id, point, contract_version })) };
}
function evaluate(shape, declarations, snapshot) {
  if (!validShape(shape) || !validProvides(declarations)) return 'plugin.provides_invalid';
  if (!catalogValid(snapshot)) return 'plugin.extension_catalog_unavailable';
  if (!shape.host_contracts.includes(snapshot.host_contract)) return 'plugin.extension_point_unsupported';
  if (new Set(declarations.map((x) => x.id)).size !== declarations.length)
    return 'plugin.duplicate_contribution';
  const shapeMap = new Map(shape.provides.map((x) => [x.id, x]));
  if (shapeMap.size !== declarations.length || declarations.some((x) =>
    shapeMap.get(x.id)?.point !== x.point || shapeMap.get(x.id)?.contract_version !== x.contract_version))
    return 'plugin.contribution_shape_mismatch';
  const pointMap = new Map(snapshot.points.map((x) => [x.point, x]));
  for (const declaration of declarations) {
    const point = pointMap.get(declaration.point);
    if (!point || point.contract_version !== declaration.contract_version)
      return 'plugin.extension_point_unsupported';
    if (point.input_schema !== declaration.input_schema || point.output_schema !== declaration.output_schema)
      return 'plugin.extension_schema_mismatch';
    if (declaration.capabilities.some((x) => !point.capabilities.includes(x)))
      return 'plugin.extension_capability_unsupported';
    if (!point.data_modes.includes(declaration.data_mode))
      return 'plugin.extension_data_mode_unsupported';
  }
  return null;
}
const copy = (x) => structuredClone(x);
const modified = (source, mutate) => { const x = copy(source); mutate(x); return x; };
const cases = [
  ['three-points', all, shapeFor(), catalog, null],
  ['preview-only', [preview], shapeFor([preview]), catalog, null],
  ['chat-only', [chat], shapeFor([chat]), catalog, null],
  ['source-only', [source], shapeFor([source]), catalog, null],
  ['chat-no-host-private', [{ ...chat, data_mode: 'no_host_private_data' }], shapeFor([chat]), catalog, null],
  ['unknown-point', [{ ...chat, point: 'embedding' }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['unknown-version', [{ ...chat, contract_version: 'v2' }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['input-schema-replaced', [{ ...chat, input_schema: source.input_schema }], shapeFor([chat]), catalog, 'plugin.extension_schema_mismatch'],
  ['output-schema-replaced', [{ ...chat, output_schema: source.output_schema }], shapeFor([chat]), catalog, 'plugin.extension_schema_mismatch'],
  ['unknown-capability', [{ ...chat, capabilities: ['tools'] }], shapeFor([chat]), catalog, 'plugin.extension_capability_unsupported'],
  ['empty-capabilities', [{ ...chat, capabilities: [] }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['duplicated-capability', [{ ...chat, capabilities: ['text', 'text'] }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['private-chat', [{ ...chat, data_mode: 'authorized_resource' }], shapeFor([chat]), catalog, 'plugin.extension_data_mode_unsupported'],
  ['private-source', [{ ...source, data_mode: 'authorized_resource' }], shapeFor([source]), catalog, 'plugin.extension_data_mode_unsupported'],
  ['duplicate-contribution', [chat, { ...chat }], shapeFor([chat, chat]), catalog, 'plugin.duplicate_contribution'],
  ['missing-shape-contribution', [chat], shapeFor([source]), catalog, 'plugin.contribution_shape_mismatch'],
  ['mismatched-point', [chat], shapeFor([{ ...chat, point: 'knowledge.source' }]), catalog, 'plugin.contribution_shape_mismatch'],
  ['missing-detail', [], shapeFor([chat]), catalog, 'plugin.contribution_shape_mismatch'],
  ['wrong-host', [chat], { ...shapeFor([chat]), host_contracts: ['campusos.host/v2'] }, catalog, 'plugin.extension_point_unsupported'],
  ['catalog-version', [chat], shapeFor([chat]), modified(catalog, (x) => { x.contract = 'campusos.extension-catalog/v2'; }), 'plugin.extension_catalog_unavailable'],
  ['catalog-duplicate-point', [chat], shapeFor([chat]), modified(catalog, (x) => { x.points.push(copy(x.points[0])); }), 'plugin.extension_catalog_unavailable'],
  ['catalog-private-chat', [chat], shapeFor([chat]), modified(catalog, (x) => { x.points[1].data_modes.push('authorized_resource'); }), 'plugin.extension_catalog_unavailable'],
  ['catalog-redefined-schema', [chat], shapeFor([chat]), modified(catalog, (x) => { x.points[1].input_schema = source.input_schema; }), 'plugin.extension_catalog_unavailable'],
  ['config-schema-remote', [{ ...chat, config_schema: { path: 'https://host/schema.json', digest } }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['config-schema-traversal', [{ ...chat, config_schema: { path: '../secret.json', digest } }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
  ['config-schema-relative', [{ ...chat, config_schema: { path: 'schemas/config.json', digest } }], shapeFor([chat]), catalog, null],
  ['extra-owner-field', [{ ...chat, owner_id: 'user-1' }], shapeFor([chat]), catalog, 'plugin.provides_invalid'],
];
const results = [], names = new Set();
for (const [name, declarations, shape, snapshot, expected] of cases) {
  assert(!names.has(name), name); names.add(name);
  const before = JSON.stringify([declarations, shape, snapshot]);
  assert.equal(evaluate(shape, declarations, snapshot), expected, name);
  assert.equal(JSON.stringify([declarations, shape, snapshot]), before, 'mutation: ' + name);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
let combinations = 0;
for (const point of Object.keys(known))
for (const dataMode of ['authorized_resource', 'public_only', 'no_host_private_data'])
for (const cap of ['pdf', 'text', 'public_text', 'tools']) {
  const declaration = provider('probe', point, dataMode, [cap]);
  const expected = known[point].data_modes.includes(dataMode) && known[point].capabilities.includes(cap);
  assert.equal(evaluate(shapeFor([declaration]), [declaration], catalog) === null, expected,
    [point, dataMode, cap].join('/'));
  combinations++;
}
const dtoCases = [
  ['preview_renderer_request', { request_id: 'r', contribution_id: 'pdf-main', instance_generation: 1, deadline_ms: 1000, resource_ref: 'opaque-1', mime_type: 'application/pdf' }, true],
  ['preview_renderer_response', { request_id: 'r', contribution_id: 'pdf-main', instance_generation: 1, ui_slot_id: 'slot', resource_revision: 'rev-1' }, true],
  ['ai_chat_request', { request_id: 'r', contribution_id: 'chat-main', instance_generation: 1, deadline_ms: 1000, prompt: '你好', context_class: 'public' }, true],
  ['ai_chat_response', { request_id: 'r', contribution_id: 'chat-main', instance_generation: 1, text: '答复', input_tokens: 1, output_tokens: 2, finish_reason: 'completed' }, true],
  ['knowledge_source_request', { request_id: 'r', contribution_id: 'source-main', instance_generation: 1, deadline_ms: 1000, binding_id: 'binding', limit: 10 }, true],
  ['knowledge_source_response', { request_id: 'r', contribution_id: 'source-main', instance_generation: 1, items: [{ external_id: 'e', revision: '1', title: '标题', text: '公开文字', source_url: 'https://example.invalid/a' }] }, true],
];
for (const [key, value] of [...dtoCases]) {
  const extra = copy(value);
  extra.owner_id = 'user-1';
  dtoCases.push([key + '-owner-injection', extra, false]);
}
dtoCases.push(
  ['ai_chat_request-private', { ...dtoCases[2][1], context_class: 'private' }, false],
  ['ai_chat_request-user-token', { ...dtoCases[2][1], user_token: 'secret' }, false],
  ['knowledge_source_request-unbounded', { ...dtoCases[4][1], limit: 101 }, false],
  ['knowledge_source_response-insecure-url', { ...dtoCases[5][1], items: [{ ...dtoCases[5][1].items[0], source_url: 'http://example.invalid/a' }] }, false],
);
const dtoResults = [];
for (const [key, value, expected] of dtoCases) {
  const definition = key.split('-')[0];
  const validate = ajv.compile({ $schema: 'http://json-schema.org/draft-07/schema#',
    definitions: dtoSchema.definitions, $ref: '#/definitions/' + definition });
  assert.equal(validate(value), expected, key);
  dtoResults.push({ name: key, expected: expected ? 'accepted' : 'rejected', passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__plugin_v5_provides__.ts');
const positiveTypes = ['PluginV5PreviewRequest', 'PluginV5PreviewResponse', 'PluginV5ChatRequest',
  'PluginV5ChatResponse', 'PluginV5KnowledgeSourceRequest', 'PluginV5KnowledgeSourceResponse'];
const lines = [
  'import type { PluginV5Provide, PluginV5ExtensionCatalog, PluginV5ProvidesErrorCode, ' +
    positiveTypes.join(', ') + ' } from "../src/index";',
  'const catalog: PluginV5ExtensionCatalog = ' + JSON.stringify(catalog) + ';',
  ...[preview, chat, source].map((x, i) => 'const provider' + i + ': PluginV5Provide = ' + JSON.stringify(x) + ';'),
  ...dtoCases.slice(0, 6).map((x, i) => 'const dto' + i + ': ' + positiveTypes[i] + ' = ' + JSON.stringify(x[1]) + ';'),
  '// @ts-expect-error unsupported extension point',
  'const badPoint: PluginV5Provide = { ...provider1, point: "embedding" };',
  '// @ts-expect-error unsupported error code',
  'const badError: PluginV5ProvidesErrorCode = "plugin.allow";',
  '// @ts-expect-error private chat context is not exposed',
  'const badChat: PluginV5ChatRequest = { ...dto2, context_class: "private" };',
  '// @ts-expect-error a source request requires an explicit limit',
  'const badSource: PluginV5KnowledgeSourceRequest = { request_id: "r", contribution_id: "s", instance_generation: 1, deadline_ms: 1, binding_id: "b" };',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['plugin-v5-provides', true]]) {
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
  schema: 'campusos.v12-g1-plugin-v5-provides-acceptance/v1',
  generated_at: new Date().toISOString(), scope: 'G1 three typed extension point declarations and bounded DTOs',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 03a/03c/08a/14c: no package install, dispatcher invocation, real ACL or provider response trust claimed',
  environment: { platform: process.platform, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, point_combinations: combinations, dto_fixtures: dtoResults,
  typescript_positive: 9, typescript_negative: 4,
  files_sha256: Object.fromEntries([providesPath, catalogPath, dtoPath, shapePath, fixturePath, errorsPath,
    'sdk/typescript/src/plugin-v5-provides.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-plugin-v5-provides-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Plugin v5 provides: ' + results.length + ' declarations, ' + combinations +
  ' point combinations, ' + dtoResults.length + ' DTO cases, 9/4 TS positive/negative types passed');
