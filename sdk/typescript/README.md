# CampusOS TypeScript Plugin SDK

This package exposes the `campusos.ui/v1` runtime types and a constrained Extension Gateway client. It does not expose Element Plus, global Vue Router/Pinia, DOM access, JWT contents, or another plugin's internal store.

```ts
const client = new CampusExtensionClient('demo', {
  token: () => localStorage.getItem('access_token') || undefined,
})
await client.invoke({ id: 'plugin.demo.action.refresh', label: 'Refresh', method: 'POST', path: '/refresh' })
```

The browser host should normally invoke actions rendered from the server Runtime Manifest. A plugin cannot use this client to expand its declared permissions: Core verifies the JWT, declared Action, permission, state, health, request size, timeout, trace, and audit policy.

## v1.2 G1 target Principal contract

`PrincipalContextV1`, `PrincipalRef`, `PrincipalContextErrorCode` and
`PRINCIPAL_CONTEXT_VERSION` describe a host-constructed target DTO. They do not
add a login endpoint or make a supplied object an authenticated principal.
See the [contract and migration map](../../docs/api/v1.2主体上下文合同.md)
and [JSON Schema](../../docs/api/principal-context-v1.schema.json).

Install locked development dependencies with `pnpm --dir sdk/typescript install --frozen-lockfile`
from the repository root, then run `make v12-principal-contract-check` and
`pnpm --dir sdk/typescript build`. The check uses the SDK's explicit Ajv development
dependency and TypeScript compiler, requires no network, and is included in
`make contracts-check`. Runtime consumers must validate the Schema: TypeScript
alone does not enforce ID syntax or reject all excess properties in variables.

## G1 public-thread ResourceFacts / Policy decision

`ThreadPublicReadRequestV1` and `ThreadPublicReadDecisionV1` describe the host-side
[public-state predicate contract](../../docs/api/v1.2公开帖子事实与策略合同.md).
A decision is bound to one request/resource/facts version and an allow requires
rechecking current facts. It does not replace authentication, board ACL, plugin
Grant/Consent or machine Scope. No runtime endpoint or production policy engine
is added. `make v12-thread-policy-contract-check` runs schema, SDK and executable
specification checks and is included in `make contracts-check`.

## G1 board governance delegation bounds

`BoardDelegationRequestV1` / `BoardDelegationDecisionV1` freeze the host-side
[bounded delegation contract](../../docs/api/v1.2版块治理委托上限合同.md).
Execution permission does not confer delegation permission. Each candidate must
fit one complete current bound; authorization changes and required audit must be
rechecked in the write transaction. This is a target DTO, not a grant API.
Run `make v12-board-delegation-contract-check` (also in `make contracts-check`).

## G1 plugin v5 package shape and host contract negotiation

The exported PluginV5PackageShape and PluginV5ReleaseEnvelope model the
[offline target shape contract](../../docs/api/v1.2插件v5包形态与版本协商合同.md).
The union distinguishes UI-only, Wasm and container forms; the JSON Schemas
and executable check also enforce paths, digests, duplicate contributions,
host-version intersection and release identity. Run
make v12-plugin-v5-shape-contract-check. This is not an install or signature
verification API; current production packages still use v4.
