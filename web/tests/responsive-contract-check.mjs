import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import {
  assertResponsiveMatrix,
  assertResponsiveRoute,
  responsivePaths,
  responsiveViewports,
} from './helpers/responsive-contract.mjs'

const origins = { web: 'http://127.0.0.1:3000', admin: 'http://127.0.0.1:3001' }
function matrix() {
  return responsiveViewports.flatMap((viewport) =>
    Object.entries(responsivePaths).flatMap(([surface, paths]) =>
      paths.map((pathname) => ({
        surface,
        viewport: { ...viewport },
        requested_pathname: pathname,
        actual_page_url: origins[surface] + (pathname === '/plugin-center' ? '/plugins' : pathname),
        status: 'passed',
      })),
    ),
  )
}

test('all seven viewports and 161 routes pass with the existing Admin redirect', () => {
  const records = matrix()
  assert.equal(records.length, 161)
  assertResponsiveMatrix(records, origins)
  const routes = readFileSync(new URL('../../admin/src/modules/plugins/routes.ts', import.meta.url), 'utf8')
  assert.match(routes, /path: "plugin-center",\s*name: "PluginCenter",\s*redirect: "\/plugins"/)
})

test('a creation page redirected to login or another pathname is rejected', () => {
  for (const pathname of ['/login', '/threads', '/']) {
    assert.throws(() =>
      assertResponsiveRoute(
        { surface: 'web', requested_pathname: '/threads/create', actual_page_url: origins.web + pathname },
        origins.web,
      ),
    )
  }
})

test('the Admin compatibility exception does not allow a Web redirect or a foreign origin', () => {
  assert.throws(() =>
    assertResponsiveRoute(
      { surface: 'web', requested_pathname: '/plugin-center', actual_page_url: origins.web + '/plugins' },
      origins.web,
    ),
  )
  const records = matrix()
  records[0].actual_page_url = origins.admin + '/'
  assert.throws(() => assertResponsiveMatrix(records, origins), /origin/)
})

test('missing, duplicate and extra route evidence cannot pass', () => {
  const missing = matrix().slice(1)
  assert.throws(() => assertResponsiveMatrix(missing, origins), /missing or extra/)
  const duplicate = matrix()
  duplicate[1] = duplicate[0]
  assert.throws(() => assertResponsiveMatrix(duplicate, origins), /duplicate/)
  assert.throws(() => assertResponsiveMatrix([...matrix(), matrix()[0]], origins), /missing or extra/)
})

test('a subset of viewports, an unfinished route or changed dimensions cannot pass the release matrix', () => {
  assert.throws(
    () =>
      assertResponsiveMatrix(
        matrix().filter((r) => r.viewport.name === 'desktop'),
        origins,
      ),
    /missing or extra/,
  )
  for (const status of ['running', 'failed']) {
    const records = matrix()
    records[0].status = status
    assert.throws(() => assertResponsiveMatrix(records, origins), /did not pass/)
  }
  for (const dimension of ['width', 'height']) {
    const records = matrix()
    records[0].viewport[dimension] += 1
    assert.throws(() => assertResponsiveMatrix(records, origins), /viewport/)
  }
})
