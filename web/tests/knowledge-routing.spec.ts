// @vitest-environment jsdom
import { expect, it } from 'vitest'
import router, { webAuthGuard } from '../src/router'
import { knowledgeRoutes } from '../src/modules/knowledge/routes'

it('registers an anonymous public Knowledge route with a mobile navigation title', async () => {
  const route = router.resolve('/knowledge')
  expect(route.name).toBe('KnowledgeSearch')
  expect(route.meta.title).toBe('知识检索')
  expect(route.meta.requiresAuth).toBeUndefined()
  expect(knowledgeRoutes).toHaveLength(2)
  expect(await webAuthGuard(route, {} as never, (() => {}) as never)).toBe(true)
})

it('requires a current User login for private knowledge', () => {
  expect(router.resolve('/knowledge/private').meta.requiresAuth).toBe(true)
})
