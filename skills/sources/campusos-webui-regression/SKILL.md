---
name: campusos-webui-regression
description: Reproduce, diagnose, fix, and verify CampusOS Web/Admin UI regressions across Vue routes/components, HTTP handlers, Go services/repositories, PostgreSQL data, permissions, and Docker hot reload. Use when a user reports empty lists, unexpected refreshes, missing notifications or fields, form/price problems, avatar/storage behavior, module navigation issues, or a UI result that disagrees with current data or documentation.
---

# CampusOS WebUI Regression

> 更新时间：2026-08-03

## Outcome

Prove the failing layer before editing, preserve API/data/security contracts, add the narrowest regression test, and verify
the same user-visible path against the running application when possible.

Read [regression matrix](references/regression-matrix.md) when the issue crosses navigation, structured content, identity,
notifications, personal storage, Admin projections, or Docker runtime behavior.

## Workflow

1. Preserve unrelated worktree changes and record the exact page, actor, action, expected result, actual result, and whether
   the issue happens once or consistently.
2. Inspect the frontend route, query parameters, API wrapper, component state, loading/error branch, and route watcher.
3. Replay the exact HTTP request against the running API when available. Compare one working control request and inspect
   the server log/request ID. Do not infer “no data” from an empty UI until the API status/body is known.
4. Trace the request through handler, service/port, repository, migration/schema, permissions, and stored data. Check ID
   types, feature/thread types, visibility states, pagination, category scopes, transactions, cache keys, and ignored local
   configuration.
5. Fix the owning layer. Do not weaken authorization, public projections, sanitization, quotas, transaction boundaries, or
   migration immutability merely to satisfy the UI.
6. Add a regression test at the lowest layer that reproduced the cause, plus a frontend test when rendering or navigation
   logic contributed. Prefer existing test harnesses and fixtures.
7. Verify in this order: focused backend test, focused frontend test, exact HTTP request, then interactive browser path when
   a browser is available. If no browser exists, report that limitation and provide HTTP/runtime evidence instead of
   claiming a visual pass.
8. Update canonical Help/API documentation, official `docs-site`, generated contracts when applicable, and one current
   progress record. State whether Docker hot reload already applied the fix or a rebuild is required.

## Guardrails

- Treat `group` as navigation/aggregation and `board` as the post-owning category; typed Mutual Aid/Secondhand records still
  have a base Community Thread.
- Keep monetary values as integer minor units in persistence and validate conversion/range at UI and API boundaries.
- Keep admin-only email/storage projections protected; do not expose them through public user endpoints.
- Keep notification writes in the reliable transaction and suppress self/duplicate notifications according to the current
  contract.
- Do not rewrite an applied migration or repair user data unless the task explicitly authorizes a forward migration/data fix.
- Do not add `location.reload()` or redundant dynamic routes to hide lifecycle/routing defects.

## Validation

Select tests from the matrix, then run base checks:

```bash
python scripts/check-line-endings.py --include-untracked
python scripts/check-doc-links.py
git diff --check
```

Use workspace-local Go cache on Windows. For Docker development, ordinary Go/Vue source hot reloads; run `rebuild` only
when a true image build input changed.

## Report

Report reproduction, root cause, changed owner, regression tests, exact runtime response, visual-browser status, reload or
rebuild requirement, documentation updates, and remaining risk.
