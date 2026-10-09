#!/usr/bin/env node
// Offline contract acceptance only. Never verifies a credential or grants access.
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
const schemaPath = 'docs/api/principal-context-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/principal-context-v1.fixtures.json';
const schema = read(schemaPath);
const catalog = read('docs/api/principal-context-v1.errors.json');
const corpus = read(fixturePath);
// No defaults, coercion, property removal or remote schema loader.
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
assert.equal(ajv.validateSchema(schema), true, 'invalid draft-07 schema');
const validate = ajv.compile(schema);
const version = 'campusos.principal/v1';
assert.equal(corpus.contract, version);
assert.equal(catalog.contract, version);
assert.deepEqual(catalog.errors.map((e) => [e.code, e.http_status, e.retryable]), [
  ['principal.context_invalid', 400, false], ['principal.contract_unsupported', 400, false],
]);
function errorCode(value) {
  if (value && typeof value.contract === 'string' && value.contract !== version) {
    return 'principal.contract_unsupported';
  }
  return validate(value) ? null : 'principal.context_invalid';
}
const names = new Set();
const fixtureResults = [];
const typeLines = [
  'import { PRINCIPAL_CONTEXT_VERSION } from "../src/index";',
  'import type { PrincipalContextV1, PrincipalContextErrorCode } from "../src/index";',
  'const version: "campusos.principal/v1" = PRINCIPAL_CONTEXT_VERSION;',
  ...catalog.errors.map((e, i) => `const error${i}: PrincipalContextErrorCode = ${JSON.stringify(e.code)};`),
  '// @ts-expect-error unknown reasons must not compile',
  'const unknownError: PrincipalContextErrorCode = "principal.allow";',
];
for (const item of corpus.cases) {
  assert(!names.has(item.name), `duplicate fixture ${item.name}`);
  names.add(item.name);
  const before = JSON.stringify(item.context);
  assert.equal(validate(item.context), item.valid, item.name);
  assert.equal(errorCode(item.context), item.valid ? null : item.error_code, item.name);
  assert.equal(JSON.stringify(item.context), before, `validator mutated ${item.name}`);
  if (item.valid || item.compile_reject) {
    if (!item.valid) typeLines.push('// @ts-expect-error deliberate domain/shape violation');
    typeLines.push(`const fixture${typeLines.length}: PrincipalContextV1 = ${before};`);
  }
  fixtureResults.push({ name: item.name, expected: item.valid ? 'valid' : item.error_code, passed: true });
}
// Independently enumerate all kind/audience/strength combinations; do not derive
// the expected matrix from the schema under test.
const matrix = {
  anonymous: ['public', ['none']], user: ['user', ['password', 'mfa']],
  admin: ['admin', ['password', 'mfa']], integration: ['integration_api', ['credential']],
  plugin_instance: ['host_api', ['workload']], worker: ['worker', ['workload']],
};
let combinations = 0;
for (const kind of Object.keys(matrix)) {
  for (const audience of ['public', 'user', 'admin', 'integration_api', 'host_api', 'worker']) {
    for (const strength of ['none', 'password', 'mfa', 'credential', 'workload']) {
      const value = { contract: version, actor: { kind }, audience, authentication_strength: strength };
      if (kind !== 'anonymous') { value.actor.id = '42'; value.credential_id = 'credential-42'; }
      const expected = audience === matrix[kind][0] && matrix[kind][1].includes(strength);
      assert.equal(validate(value), expected, `${kind}/${audience}/${strength}`);
      combinations++;
    }
  }
}
// Compile the same positive fixtures and shape/domain negatives against exported
// SDK types, using an in-memory file without generated test artifacts.
const virtualPath = resolve(root, 'sdk/typescript/tests/__principal_contract__.ts');
const source = typeLines.join('\n');
// The existing SDK uses strict=true without exactOptionalPropertyTypes. Check
// its public export with those settings, and the new isolated DTO additionally
// with exact optional properties, without changing unrelated SDK client code.
for (const [entry, exactOptionalPropertyTypes] of [['index', false], ['principal', true]]) {
  const options = { noEmit: true, strict: true, exactOptionalPropertyTypes,
    skipLibCheck: true, target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler, types: [] };
  const host = ts.createCompilerHost(options);
  const originalGetSourceFile = host.getSourceFile.bind(host);
  host.getSourceFile = (path, language, onError, fresh) => path === virtualPath
    ? ts.createSourceFile(path, source.replaceAll('../src/index', `../src/${entry}`), language, true)
    : originalGetSourceFile(path, language, onError, fresh);
  const program = ts.createProgram([virtualPath], options, host);
  const diagnostics = ts.getPreEmitDiagnostics(program);
  assert.equal(diagnostics.length, 0, ts.formatDiagnosticsWithColorAndContext(diagnostics, {
    getCanonicalFileName: (x) => x, getCurrentDirectory: () => root, getNewLine: () => '\n',
  }));
}
const report = {
  schema: 'campusos.v12-g1-principal-acceptance/v1', generated_at: new Date().toISOString(),
  scope: 'G1 target DTO/schema only; no credential, Policy, HTTP or PostgreSQL isolation acceptance',
  environment: { platform: process.platform, arch: process.arch, node: process.version,
    ajv: require('ajv/package.json').version, typescript: ts.version },
  implementation: 'passed', automation: 'passed', target_environment: 'offline contract checks passed',
  typescript_modes: ['public SDK: strict', 'isolated DTO: strict + exactOptionalPropertyTypes'],
  runtime_acceptance: 'not applicable to this slice; V12-02a/02c/02d/03c remain pending',
  fixtures: fixtureResults, domain_combinations: combinations,
  typescript_fixtures: corpus.cases.filter((x) => x.valid || x.compile_reject).length,
  files_sha256: Object.fromEntries([
    schemaPath, fixturePath, 'docs/api/principal-context-v1.errors.json',
    'sdk/typescript/src/principal.ts', 'sdk/typescript/src/index.ts',
    'sdk/typescript/package.json', 'sdk/typescript/pnpm-lock.yaml',
    'scripts/check-v12-principal-contract.mjs',
  ].map((path) => [path, createHash('sha256').update(readFileSync(resolve(root, path))).digest('hex')])),
};
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === '--report'), 'usage: [--report <path>]');
if (args.length) writeFileSync(resolve(args[1]), JSON.stringify(report, null, 2) + '\n');
console.log(`Principal v1: ${fixtureResults.length} fixtures, ${combinations} domain combinations, ${report.typescript_fixtures} TypeScript fixtures passed`);
