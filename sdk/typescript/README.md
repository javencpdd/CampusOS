# CampusOS TypeScript Plugin SDK

This package exposes the `campusos.ui/v1` runtime types and a constrained Extension Gateway client. It does not expose Element Plus, global Vue Router/Pinia, DOM access, JWT contents, or another plugin's internal store.

```ts
const client = new CampusExtensionClient('demo', {
  token: () => localStorage.getItem('access_token') || undefined,
})
await client.invoke({ id: 'plugin.demo.action.refresh', label: 'Refresh', method: 'POST', path: '/refresh' })
```

The browser host should normally invoke actions rendered from the server Runtime Manifest. A plugin cannot use this client to expand its declared permissions: Core verifies the JWT, declared Action, permission, state, health, request size, timeout, trace, and audit policy.
