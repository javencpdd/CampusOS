#!/usr/bin/env node
// G1 offline target matrix; runtime identity split belongs to V12-02d.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(resolve(root, 'sdk/typescript/package.json'));
const Ajv = require('ajv'), ts = require('typescript');
const read = (p) => JSON.parse(readFileSync(resolve(root, p), 'utf8'));
const schemaPath = 'docs/api/identity-domains-v1.schema.json';
const errorsPath = 'docs/api/identity-domains-v1.errors.json';
const ajv = new Ajv({ allErrors: true, schemaId: 'auto', strictKeywords: true });
const schema = read(schemaPath);
assert(ajv.validateSchema(schema));
const validate = ajv.compile(schema), errors = read(errorsPath);
assert.equal(errors.contract, 'campusos.identity-domains/v1');
const codes = new Set(errors.errors.map((x) => x.code));
assert.equal(codes.size, 10);
const domain = (kind) => ({
  issuer: 'campusos-' + kind, audience: kind + '-api', key_id: kind + '-key',
  credential_store: kind + '-credentials', session_store: kind + '-sessions',
  recovery_store: kind + '-recovery', role_store: kind + '-roles',
});
const base = { contract: 'campusos.identity-domains/v1',
  domains: { user: domain('user'), admin: domain('admin') },
  request: { kind: 'admin', account_id: 'same-42', ...domain('admin'),
    challenge_kind: 'access', challenge_domain: 'admin', target_account_id: 'same-42',
    status: 'active', activation_source: 'direct' } };
delete base.request.recovery_store;
delete base.request.role_store;
const copy = (x) => structuredClone(x);
function evaluate(x) {
  if (!validate(x)) return 'identity.domain_invalid';
  const u = x.domains.user, a = x.domains.admin;
  if (Object.keys(u).some((key) => u[key] === a[key])) return 'identity.domain_not_isolated';
  const r = x.request, target = x.domains[r.kind];
  if (r.kind === 'admin' && r.activation_source === 'user_role')
    return 'identity.admin_auto_activation_forbidden';
  if (r.status !== 'active') return 'identity.account_inactive';
  if (r.issuer !== target.issuer || r.audience !== target.audience ||
      r.key_id !== target.key_id || r.credential_store !== target.credential_store ||
      r.session_store !== target.session_store) return 'identity.cross_domain_credential';
  if (r.challenge_domain !== r.kind || r.target_account_id !== r.account_id)
    return 'identity.challenge_wrong_domain';
  if (x.operation) {
    const op = x.operation;
    if (op.kind === 'bootstrap') {
      if (op.actor_kind !== 'system' || !op.bootstrap_enabled ||
          op.active_security_admins !== 0) return 'identity.admin_bootstrap_forbidden';
    } else {
      if (op.actor_kind !== 'admin') return 'identity.admin_actor_required';
      if (!op.actor_mfa) return 'identity.admin_mfa_required';
      if (['suspend','revoke'].includes(op.kind) && op.target_is_security_admin &&
          op.active_security_admins <= 1) return 'identity.last_security_admin';
    }
  }
  return null;
}
const cases = [['admin-valid', base, null]];
const altered = (name, mutate, code) => { const x = copy(base); mutate(x); cases.push([name, x, code]); };
altered('user-valid-same-id', (x) => { x.request.kind='user'; Object.assign(x.request, domain('user')); delete x.request.recovery_store; delete x.request.role_store; x.request.challenge_domain='user'; }, null);
for (const field of ['issuer','audience','key_id','credential_store','session_store'])
  altered('admin-with-user-' + field, (x) => { x.request[field]=x.domains.user[field]; }, 'identity.cross_domain_credential');
for (const field of ['issuer','audience','key_id','credential_store','session_store','recovery_store','role_store'])
  altered('shared-' + field, (x) => { x.domains.admin[field]=x.domains.user[field]; }, 'identity.domain_not_isolated');
for (const challenge of ['access','refresh','mfa','email','recovery'])
  altered('user-challenge-for-admin-' + challenge, (x) => { x.request.challenge_kind=challenge; x.request.challenge_domain='user'; }, 'identity.challenge_wrong_domain');
altered('other-account-recovery', (x) => { x.request.challenge_kind='recovery'; x.request.target_account_id='other'; }, 'identity.challenge_wrong_domain');
altered('role-auto-admin', (x) => { x.request.activation_source='user_role'; }, 'identity.admin_auto_activation_forbidden');
for (const status of ['suspended','revoked'])
  altered(status, (x) => { x.request.status=status; }, 'identity.account_inactive');
const operation = (kind) => ({ kind, actor_kind:'admin', actor_mfa:true, target_admin_id:'same-42',
  target_is_security_admin:false, active_security_admins:2, bootstrap_enabled:false, audit_reason:'required audit' });
altered('admin-create', (x) => { x.operation=operation('create'); }, null);
altered('admin-create-by-user', (x) => { x.operation=operation('create'); x.operation.actor_kind='user'; }, 'identity.admin_actor_required');
altered('admin-create-no-mfa', (x) => { x.operation=operation('create'); x.operation.actor_mfa=false; }, 'identity.admin_mfa_required');
for (const kind of ['suspend','revoke'])
  altered('last-admin-' + kind, (x) => { x.operation=operation(kind); x.operation.target_is_security_admin=true; x.operation.active_security_admins=1; }, 'identity.last_security_admin');
altered('bootstrap-only-first', (x) => { x.operation=operation('bootstrap'); x.operation.actor_kind='system'; x.operation.active_security_admins=0; x.operation.bootstrap_enabled=true; }, null);
altered('bootstrap-with-admin', (x) => { x.operation=operation('bootstrap'); x.operation.actor_kind='system'; x.operation.active_security_admins=1; x.operation.bootstrap_enabled=true; }, 'identity.admin_bootstrap_forbidden');
altered('bootstrap-by-user', (x) => { x.operation=operation('bootstrap'); x.operation.actor_kind='user'; x.operation.active_security_admins=0; x.operation.bootstrap_enabled=true; }, 'identity.admin_bootstrap_forbidden');
altered('unknown-challenge', (x) => { x.request.challenge_kind='password_reset_any'; }, 'identity.domain_invalid');
const results = [];
for (const [name, input, expected] of cases) {
  const before = JSON.stringify(input);
  assert.equal(evaluate(input), expected, name);
  assert.equal(JSON.stringify(input), before, name + ' mutated');
  assert(expected === null || codes.has(expected), name);
  results.push({ name, expected: expected ?? 'accepted', passed: true });
}
const virtual = resolve(root, 'sdk/typescript/tests/__identity_domains__.ts');
const source = [
  'import type { IdentityDomainDecisionInput, IdentityDomainErrorCode } from "../src/index";',
  'const good: IdentityDomainDecisionInput = ' + JSON.stringify(base) + ';',
  '// @ts-expect-error unsupported principal kind',
  'const badKind: IdentityDomainDecisionInput["request"]["kind"] = "integration";',
  '// @ts-expect-error unsupported activation source',
  'const badActivation: IdentityDomainDecisionInput["request"]["activation_source"] = "user_admin_role";',
  '// @ts-expect-error unsupported operation',
  'const badOp: NonNullable<IdentityDomainDecisionInput["operation"]>["kind"] = "assign_user_role";',
  '// @ts-expect-error unknown error',
  'const badError: IdentityDomainErrorCode = "identity.allow_cross_domain";',
].join('\n');
for (const entry of ['index','identity-domains']) {
  const options = { noEmit:true, strict:true, exactOptionalPropertyTypes:entry!=='index',
    skipLibCheck:true, target:ts.ScriptTarget.ES2022, module:ts.ModuleKind.ESNext,
    moduleResolution:ts.ModuleResolutionKind.Bundler, types:[] };
  const host=ts.createCompilerHost(options), get=host.getSourceFile.bind(host);
  host.getSourceFile=(path, language, onError, fresh) => path===virtual
    ? ts.createSourceFile(path, source.replace('../src/index','../src/' + entry), language, true)
    : get(path,language,onError,fresh);
  const diagnostics=ts.getPreEmitDiagnostics(ts.createProgram([virtual],options,host));
  assert.equal(diagnostics.length,0,ts.formatDiagnosticsWithColorAndContext(diagnostics,{
    getCanonicalFileName:(x)=>x,getCurrentDirectory:()=>root,getNewLine:()=>'\n'}));
}
const report={ schema:'campusos.v12-g1-identity-domains-acceptance/v1',
  generated_at:new Date().toISOString(),scope:'G1 independent User/Admin credentials, session, challenge, bootstrap and last-admin target',
  implementation:'passed',automation:'passed',target_environment:process.platform + ' offline Node/Ajv/TypeScript contract acceptance passed',
  runtime_acceptance:'pending V12-02d PostgreSQL/Memory account, token, refresh, MFA, recovery and audit',
  environment:{platform:process.platform,node:process.version,ajv:require('ajv/package.json').version,typescript:ts.version},
  fixtures:results,
  files_sha256:Object.fromEntries([schemaPath,errorsPath,'sdk/typescript/src/identity-domains.ts','sdk/typescript/src/index.ts','scripts/check-v12-identity-domains-contract.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')])) };
const args=process.argv.slice(2);
assert(args.length===0 || (args.length===2 && args[0]==='--report'),'usage: [--report <path>]');
if(args.length) writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('Identity domains: ' + results.length + ' fixtures, 1/4 TS positive/negative types passed');
