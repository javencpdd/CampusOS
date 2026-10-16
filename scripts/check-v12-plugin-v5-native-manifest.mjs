#!/usr/bin/env node
// Independent G1/Ajv oracle and actual built CLI acceptance; no Go validation code is imported.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve, dirname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const read = (p) => JSON.parse(readFileSync(resolve(root, p), 'utf8'));
const schemaPath = 'docs/api/plugin-v5-manifest-v1.schema.json';
const fixturePath = 'sdk/typescript/tests/plugin-v5-manifest-native.fixtures.json';
const hostPath = 'sdk/typescript/tests/plugin-v5-host-catalog.fixture.json';
const extPath = 'sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
const valid = ajv.compile(read(schemaPath));
const host = read(hostPath), ext = read(extPath), fixtures = read(fixturePath);

// Frozen G1 decision order. Codec hardening and current-catalog fault cases
// have separate native tests; the wire-schema semantics remain independent.
function oracle(m, r) {
  if (!valid(m)) return 'plugin.manifest_invalid';
  if (m.runtime !== 'none' && (m.backend.artifact.kind !== m.runtime || !m.provides.length) ||
      m.runtime === 'none' && m.provides.length) return 'plugin.manifest_shape_mismatch';
  let defaults;
  try { defaults = ajv.compile(m.configuration.schema); } catch { return 'plugin.manifest_invalid'; }
  if (!defaults(m.configuration.defaults) || m.configuration.secret_names.some((s) => Object.hasOwn(m.configuration.schema.properties, s))) return 'plugin.manifest_invalid';
  if (!m.host_contracts.includes(host.host_contract) || !m.host_contracts.includes(ext.host_contract)) return 'plugin.manifest_schema_mismatch';
  for (const c of m.consumes) {
    const item = host.items.find((v) => v.code === c.capability);
    if (!item || item.scope_kind !== c.scope.kind ||
        c.scope.kind === 'collection' && !c.scope.collection_ids.every((v) => item.available_public_collection_ids.includes(v)) ||
        c.scope.kind === 'system_config' && !c.scope.keys.every((v) => item.available_config_keys.includes(v))) return 'plugin.manifest_capability_unpublished';
  }
  const ids = new Set();
  for (const p of m.provides) {
    const item = ext.points.find((v) => v.point === p.point && v.contract_version === p.contract_version);
    if (!item || ids.has(p.id)) return 'plugin.manifest_extension_unpublished';
    ids.add(p.id);
    if (item.input_schema !== p.input_schema || item.output_schema !== p.output_schema ||
        !p.capabilities.every((v) => item.capabilities.includes(v)) || !item.data_modes.includes(p.data_mode)) return 'plugin.manifest_schema_mismatch';
  }
  if (m.host_resources && m.data.private_kv_bytes + m.data.private_object_bytes > m.host_resources.storage_bytes) return 'plugin.manifest_resource_invalid';
  if (m.data.upgrade.mode === 'migrate' && m.data.upgrade.rollback === 'snapshot' && m.data.private_kv_bytes + m.data.private_object_bytes > 0) return 'plugin.manifest_upgrade_unrecoverable';
  if (['publisher', 'key', 'version'].some((v) => m[v] !== r[v])) return 'plugin.manifest_release_mismatch';
  return 'accepted';
}

const args = process.argv.slice(2);
assert.equal(args.length, 6, 'usage: --native <decisions> --cli <built-binary> --work <owned-cache-dir>');
assert.deepEqual([args[0], args[2], args[4]], ['--native', '--cli', '--work']);
const native = JSON.parse(readFileSync(resolve(args[1]), 'utf8'));
const binary = resolve(args[3]), work = resolve(args[5]);
assert(work.startsWith(resolve(root, '.cache') + sep));
assert.equal(native.length, fixtures.length);
assert.equal(new Set(native.map((v) => v.name)).size, fixtures.length);
mkdirSync(resolve(work, 'inputs'), { recursive: true });
const checks = [];
for (const f of fixtures) {
  assert(/^[a-z0-9-]+$/.test(f.name));
  const expected = oracle(f.manifest, f.release);
  assert.equal(f.expected, expected, 'frozen fixture ' + f.name);
  assert.deepEqual(native.find((v) => v.name === f.name), { name: f.name, actual: expected }, 'native ' + f.name);
  const m = resolve(work, 'inputs', f.name + '.manifest.json');
  const r = resolve(work, 'inputs', f.name + '.release.json');
  writeFileSync(m, JSON.stringify(f.manifest)); writeFileSync(r, JSON.stringify(f.release));
  const child = spawnSync(binary, ['--manifest', m, '--release', r, '--host-catalog', resolve(root, hostPath), '--extension-catalog', resolve(root, extPath)], {
    cwd: work, encoding: 'utf8', timeout: 5000, env: { PATH: process.env.PATH, TMPDIR: work },
  });
  assert.equal(child.error, undefined, f.name);
  assert.equal(child.stderr, '', f.name);
  assert.equal(child.status, expected === 'accepted' ? 0 : 1, f.name + ': ' + child.stdout);
  const out = JSON.parse(child.stdout);
  assert.equal(out.manifest_contract_valid, expected === 'accepted');
  assert.equal(out.code ?? 'accepted', expected);
  for (const flag of ['installable', 'package_bytes_verified', 'signature_verified']) assert.equal(out[flag], false);
  assert.equal(out.scope, 'declaration_and_explicit_catalog_snapshots');
  checks.push({ name: f.name, native: expected, built_cli: 'passed' });
}
const digest = (p) => createHash('sha256').update(readFileSync(p)).digest('hex');
const report = {
  schema: 'campusos.v12-03a-native-manifest-target/v1', generated_at: new Date().toISOString(),
  status: 'passed', platform: process.platform, arch: process.arch, node: process.version,
  actual_go_decisions: native.length, actual_cli_processes: checks.length, independent_oracle: 'Ajv and frozen G1 catalog semantics',
  catalogs: 'explicit synthetic G1 fixture snapshots; not current production publications or authority',
  cli_binary_sha256: digest(binary), checks,
  contract_input_sha256: Object.fromEntries([schemaPath, fixturePath, hostPath, extPath, 'scripts/check-v12-plugin-v5-native-manifest.mjs'].map((p) => [p, digest(resolve(root, p))])),
  boundaries: ['No package bytes, digest/signature authenticity, installation or execution.', 'No DB, network, plugin code execution or production catalog publication.'],
};
writeFileSync(resolve(work, 'target-result.json'), JSON.stringify(report, null, 2) + '\n');
console.log('v5 native Manifest: ' + native.length + ' actual Go decisions and built CLI processes match independent G1 oracle');
