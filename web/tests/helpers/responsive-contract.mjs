import assert from 'node:assert/strict'

export const responsiveViewports = [
  { name: 'phone-narrow', width: 320, height: 568 },
  { name: 'phone', width: 360, height: 800 },
  { name: 'phone-large', width: 393, height: 852 },
  { name: 'tablet-portrait', width: 768, height: 1024 },
  { name: 'tablet-landscape', width: 1024, height: 768 },
  { name: 'desktop', width: 1366, height: 768 },
  { name: 'phone-landscape', width: 852, height: 393 },
]

export const responsivePaths = {
  web: [
    '/',
    '/threads',
    '/threads/create',
    '/mutual-aid/create',
    '/secondhand/create',
    '/appearance',
    '/space/settings',
    '/account/security',
    '/schedule',
    '/plugins',
  ],
  admin: [
    '/',
    '/users',
    '/threads',
    '/permissions',
    '/plugins',
    '/plugin-center',
    '/extensions',
    '/features',
    '/appearance',
    '/admin-admission',
    '/mfa-policy',
    '/integrations',
    '/architecture',
  ],
}

export function assertResponsiveRoute(record, origin) {
  const actual = new URL(record.actual_page_url)
  const expectedPath =
    record.surface === 'admin' && record.requested_pathname === '/plugin-center'
      ? '/plugins'
      : record.requested_pathname
  // Only the existing Admin compatibility route may redirect. A login page,
  // fallback route or another origin cannot satisfy a page's layout checks.
  assert.equal(actual.origin, new URL(origin).origin, 'responsive route changed origin')
  assert.equal(actual.pathname, expectedPath, 'responsive route changed pathname')
}

export function assertResponsiveMatrix(records, origins, viewports = responsiveViewports) {
  const expected = new Map(
    viewports.flatMap((viewport) =>
      Object.entries(responsivePaths).flatMap(([surface, paths]) =>
        paths.map((pathname) => [`${surface}:${viewport.name}:${pathname}`, viewport]),
      ),
    ),
  )
  assert.equal(records.length, expected.size, 'responsive matrix has missing or extra routes')
  for (const record of records) {
    const key = `${record.surface}:${record.viewport.name}:${record.requested_pathname}`
    const viewport = expected.get(key)
    assert.ok(viewport, 'responsive matrix has an unexpected or duplicate route')
    assert.equal(record.viewport.width, viewport.width, 'responsive viewport width changed')
    assert.equal(record.viewport.height, viewport.height, 'responsive viewport height changed')
    assert.equal(record.status, 'passed', 'responsive route did not pass')
    assertResponsiveRoute(record, origins[record.surface])
    expected.delete(key)
  }
  assert.equal(expected.size, 0, 'responsive matrix is incomplete')
}
