#!/usr/bin/env node
// G1 offline config specification; no Secret Broker or persisted config is changed.
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
const schemaPath = 'docs/api/plugin-v5-config-v1.schema.json';
const errorsPath = 'docs/api/plugin-v5-config-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
const schema = read(schemaPath);
assert(ajv.validateSchema(schema));
const validInput = ajv.compile(schema);
const errors = read(errorsPath);
assert.equal(errors.contract, 'campusos.plugin-config/v1');
assert.deepEqual(errors.errors.map((x) => x.code), [
  'plugin.config_invalid', 'plugin.config_schema_unsupported', 'plugin.config_defaults_invalid',
  'plugin.config_revision_conflict', 'plugin.config_selector_forbidden', 'plugin.secret_ref_invalid',
]);
const base = {
  definition: { contract: 'campusos.plugin-config/v1', version: 'v1',
    schema: { type: 'object', additionalProperties: false, properties: {
      locale: { type: 'string', title: '语言', enum: ['zh-CN', 'en-US'] },
      profile_id: { type: 'string', title: '受管 Profile', maxLength: 128 },
      collection_id: { type: 'string', title: '公开集合', maxLength: 128 },
      max_results: { type: 'integer', title: '结果数', minimum: 1, maximum: 50 },
    }, required: ['locale', 'profile_id', 'collection_id', 'max_results'] },
    defaults: { locale: 'zh-CN', profile_id: 'profile-public', collection_id: 'collection-public-a', max_results: 10 },
    selectors: [{ field: 'profile_id', kind: 'profile' }, { field: 'collection_id', kind: 'public_collection' }],
    secret_names: ['provider_token'] },
  update: { expected_revision: 3,
    values: { locale: 'zh-CN', profile_id: 'profile-public', collection_id: 'collection-public-a', max_results: 20 },
    secret_refs: { provider_token: 'secret-ref:approved-1' } },
  current_revision: 3, available_profiles: ['profile-public'],
  available_public_collections: ['collection-public-a'],
};
const copy = (x) => structuredClone(x);
const alter = (mutate) => { const x = copy(base); mutate(x); return x; };
function selectorValid(input, values) {
  for (const selector of input.definition.selectors) {
    const field = input.definition.schema.properties[selector.field];
    if (!field || field.type !== 'string') return false;
    const allowed = selector.kind === 'profile' ? input.available_profiles : input.available_public_collections;
    if (!allowed.includes(values[selector.field])) return false;
  }
  return true;
}
function evaluate(input) {
  if (input?.definition?.schema && JSON.stringify(input.definition.schema).length > 16384)
    return 'plugin.config_schema_unsupported';
  const badSecret = input?.update?.secret_refs && typeof input.update.secret_refs === 'object' &&
    Object.values(input.update.secret_refs).some((x) => typeof x !== 'string' ||
      !/^secret-ref:[a-z0-9-]{2,128}$/.test(x));
  if (badSecret) return 'plugin.secret_ref_invalid';
  if (!validInput(input)) {
    const spec = input?.definition?.schema;
    if (spec && typeof spec === 'object' && (JSON.stringify(spec).includes('"$ref"') ||
        JSON.stringify(spec).includes('"x-script"') || JSON.stringify(spec).includes('"items"')))
      return 'plugin.config_schema_unsupported';
    return 'plugin.config_invalid';
  }
  const def = input.definition, properties = def.schema.properties;
  if (def.schema.required.some((name) => !Object.hasOwn(properties, name)) ||
      new Set(def.selectors.map((x) => x.field)).size !== def.selectors.length ||
      def.selectors.some((x) => properties[x.field]?.type !== 'string') ||
      def.secret_names.some((name) => Object.hasOwn(properties, name)) ||
      Object.values(properties).some((f) =>
        (f.type === 'string' && (f.minimum !== undefined || f.maximum !== undefined)) ||
        (f.type !== 'string' && (f.minLength !== undefined || f.maxLength !== undefined)) ||
        (f.minimum !== undefined && f.maximum !== undefined && f.minimum > f.maximum) ||
        (f.minLength !== undefined && f.maxLength !== undefined && f.minLength > f.maxLength)))
    return 'plugin.config_schema_unsupported';
  let validate;
  try { validate = ajv.compile(def.schema); } catch { return 'plugin.config_schema_unsupported'; }
  if (!validate(def.defaults)) return 'plugin.config_defaults_invalid';
  if (!selectorValid(input, def.defaults)) return 'plugin.config_defaults_invalid';
  if (input.update.expected_revision !== input.current_revision) return 'plugin.config_revision_conflict';
  if (!validate(input.update.values)) return 'plugin.config_invalid';
  if (!selectorValid(input, input.update.values)) return 'plugin.config_selector_forbidden';
  if (Object.keys(input.update.secret_refs).some((name) => !def.secret_names.includes(name)))
    return 'plugin.secret_ref_invalid';
  return null;
}
const cases = [
  ['valid', base, null],
  ['version-race', alter((x) => { x.update.expected_revision = 2; }), 'plugin.config_revision_conflict'],
  ['default-out-of-range', alter((x) => { x.definition.defaults.max_results = 99; }), 'plugin.config_defaults_invalid'],
  ['default-profile-unavailable', alter((x) => { x.definition.defaults.profile_id = 'profile-private'; }), 'plugin.config_defaults_invalid'],
  ['update-profile-unavailable', alter((x) => { x.update.values.profile_id = 'profile-private'; }), 'plugin.config_selector_forbidden'],
  ['update-collection-private', alter((x) => { x.update.values.collection_id = 'collection-private'; }), 'plugin.config_selector_forbidden'],
  ['value-out-of-range', alter((x) => { x.update.values.max_results = 99; }), 'plugin.config_invalid'],
  ['unknown-value', alter((x) => { x.update.values.extra = 1; }), 'plugin.config_invalid'],
  ['remote-ref', alter((x) => { x.definition.schema.properties.locale.$ref = 'https://example.test/schema'; }), 'plugin.config_schema_unsupported'],
  ['script', alter((x) => { x.definition.schema.properties.locale['x-script'] = 'eval'; }), 'plugin.config_schema_unsupported'],
  ['nested-array', alter((x) => { x.definition.schema.properties.locale.items = { type: 'string' }; }), 'plugin.config_schema_unsupported'],
  ['secret-field-in-normal-config', alter((x) => { x.definition.schema.properties.provider_token = { type: 'string', title: 'Token' }; }), 'plugin.config_schema_unsupported'],
  ['raw-secret', alter((x) => { x.update.secret_refs.provider_token = 'sk-live-secret'; }), 'plugin.secret_ref_invalid'],
  ['placeholder', alter((x) => { x.update.secret_refs.provider_token = '********'; }), 'plugin.secret_ref_invalid'],
  ['unknown-secret-name', alter((x) => { x.update.secret_refs.other_key = 'secret-ref:approved-1'; }), 'plugin.secret_ref_invalid'],
  ['selector-field-missing', alter((x) => { x.definition.selectors[0].field = 'missing'; }), 'plugin.config_schema_unsupported'],
  ['selector-field-numeric', alter((x) => { x.definition.selectors[0].field = 'max_results'; }), 'plugin.config_schema_unsupported'],
  ['duplicate-selectors', alter((x) => { x.definition.selectors.push(copy(x.definition.selectors[0])); }), 'plugin.config_schema_unsupported'],
  ['unknown-definition-field', alter((x) => { x.definition.host_shell = true; }), 'plugin.config_invalid'],
  ['bad-schema-bounds', alter((x) => { x.definition.schema.properties.max_results.minimum = 100; }), 'plugin.config_schema_unsupported'],
];
const results = [], names = new Set();
for (const [name, input, expected] of cases) {
  assert(!names.has(name), name); names.add(name);
  const before = JSON.stringify(input);
  assert.equal(evaluate(input), expected, name);
  assert.equal(JSON.stringify(input), before, 'mutation ' + name);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
const rechecks = [];
for (const [name, mutate] of [
  ['profile-removed', (x) => { x.available_profiles = []; }],
  ['collection-removed', (x) => { x.available_public_collections = []; }],
]) {
  const changed = alter(mutate);
  assert.equal(evaluate(base), null);
  assert.equal(evaluate(changed), 'plugin.config_defaults_invalid', name);
  rechecks.push({ name, passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__plugin_v5_config__.ts');
const lines = [
  'import type { PluginV5ConfigDecisionInput, PluginV5ConfigErrorCode } from "../src/index";',
  'const input: PluginV5ConfigDecisionInput = ' + JSON.stringify(base) + ';',
  '// @ts-expect-error only supported selector kinds',
  'const badSelector: PluginV5ConfigDecisionInput["definition"]["selectors"][number]["kind"] = "secret";',
  '// @ts-expect-error no free-form schema type',
  'const badField: PluginV5ConfigDecisionInput["definition"]["schema"]["properties"][string]["type"] = "script";',
  '// @ts-expect-error unknown error',
  'const badError: PluginV5ConfigErrorCode = "plugin.config_allow";',
];
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['plugin-v5-config', true]]) {
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
  schema: 'campusos.v12-g1-plugin-config-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'G1 bounded JSON Schema subset, defaults, selectors, Secret refs and revision compare',
  implementation: 'passed', automation: 'passed',
  target_environment: process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance: 'pending 01b/03b/04/07: no config persistence, Secret Broker, UI form or transaction concurrency claimed',
  environment: { platform: process.platform, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  fixtures: results, selector_rechecks: rechecks,
  files_sha256: Object.fromEntries([schemaPath, errorsPath,
    'sdk/typescript/src/plugin-v5-config.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-plugin-v5-config-contract.mjs'
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log('Plugin v5 config: ' + results.length + ' fixtures, ' + rechecks.length + ' selector rechecks, 1/3 TS positive/negative types passed');
