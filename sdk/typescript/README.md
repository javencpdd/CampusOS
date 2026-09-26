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

## G1 plugin v5 consumes and typed scopes

The exported PluginV5Consumes and PluginV5HostCatalog DTOs describe the
[target declaration and host catalog projection](../../docs/api/v1.2插件v5消费能力与范围合同.md).
The offline check covers public, self, exact public collection IDs and current
plugin non-secret configuration keys. Run make v12-plugin-v5-consumes-contract-check.
Catalog agreement does not grant access; production Admin Grant, User Consent,
resource ACL and runtime isolation remain separate.

## G1 plugin v5 provides

PluginV5Provide and the three extension request/response DTO pairs describe the
[target contribution contract](../../docs/api/v1.2插件v5贡献点与调用DTO合同.md).
Run make v12-plugin-v5-provides-contract-check. The host owns the published
extension catalog and must validate every call and returned resource; SDK types
do not install or authorize a provider.

## G1 host resources and installation boundary

PluginV5ResourceDecisionInput and PluginV5InstallPlan describe the
[target ceilings and non-invasive install invariants](../../docs/api/v1.2插件宿主资源与非侵入式安装合同.md).
Run make v12-plugin-v5-resource-install-contract-check. These DTOs do not
grant network access or install a package.

## G1 bounded plugin configuration

PluginV5ConfigDecisionInput describes the
[target configuration contract](../../docs/api/v1.2插件配置Schema与Secret分流合同.md).
Run make v12-plugin-v5-config-contract-check. The host validates defaults,
selector choices and revision preconditions; Secret Broker writes and UI form
generation remain separate runtime work.

## G1 additional resource Policy

ResourcePolicyRequestV1 and ResourcePolicyDecisionV1 are
[host-constructed target contracts](../../docs/api/v1.2其他资源与动作策略合同.md).
Run make v12-resource-policy-contract-check. The executable specification does
not create a production Authorizer or grant access to a resource.


## G1 complete target contract set

The [G1 exit matrix](../../docs/api/v1.2-G1合同退出矩阵.json) maps each
Schema, SDK DTO, error catalog and Linux offline acceptance report. Newly
exported identity-domain and scope-delegation DTOs describe independent
User/Admin credentials and authority-bound non-board grants. PluginV5Manifest
combines package shape, consumes, provides, resource budgets, bounded config
and private data rules into one target wire format.

PluginV5DispatchDecisionInput describes calls in both directions; its trusted
facts are host-built, not plugin-supplied. PluginV5SafeEvent and
PluginV5LifecycleDecisionInput constrain event metadata, generation and
publication order. See the [independent identity contract](../../docs/api/v1.2独立身份域合同.md),
[full Manifest](../../docs/api/v1.2插件v5完整Manifest目标合同.md) and
[bidirectional call/event contract](../../docs/api/v1.2插件双向调用事件与生命周期合同.md).
Run make v12-g1-exit-check for the DTO/error/evidence matrix or
make contracts-check for every target checker. These declarations do not
activate v5 in the current runtime.
