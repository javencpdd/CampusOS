#!/usr/bin/env node
// Validate actual Go output against the frozen G1 wire schema and allow set.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv');
const read = (p) => JSON.parse(readFileSync(p, 'utf8'));
assert.equal(process.argv.length, 3, 'usage: check-v12-02a-policy-output.mjs <actual-go-corpus.json>');
const ajv = new Ajv({ allErrors: true, strictKeywords: true });
ajv.addSchema(read(resolve(root, 'docs/api/principal-context-v1.schema.json')));
const schema = read(resolve(root, 'docs/api/policy-thread-public-read-v1.schema.json'));
ajv.addSchema(schema);
const requestValid = ajv.compile({ $ref: schema.$id + '#/definitions/request' });
const decisionValid = ajv.compile({ $ref: schema.$id + '#/definitions/decision' });
const outputs = read(process.argv[2]);
assert.equal(outputs.length, 360);
let allowed = 0;
const combinations = new Set();
for (const { request: r, decision: d } of outputs) {
  assert(requestValid(r), JSON.stringify(requestValid.errors));
  assert(decisionValid(d), JSON.stringify(decisionValid.errors));
  const f = r.facts;
  const tuple = [f.publication_status, f.moderation_status, f.deletion_status].join('/');
  const expected = tuple === 'published/clear/active';
  assert.equal(d.effect, expected ? 'allow' : 'deny');
  assert.equal(d.request_id, r.request_id);
  assert.equal(d.policy, r.policy);
  assert.deepEqual(d.resource, f.resource);
  assert.equal(d.facts_version, f.facts_version);
  combinations.add(tuple + '/' + JSON.stringify(r.principal));
  if (expected) allowed++;
}
assert.equal(combinations.size, 360);
assert.equal(allowed, 10);
console.log('Native Go output: 360 schema/semantic cases, 10 allowed, 350 denied; passed');
