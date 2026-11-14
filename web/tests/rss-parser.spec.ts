// @vitest-environment jsdom
import { expect, it } from 'vitest'
import { parseFeed, articleHTML } from '../../plugins/campusos.rss-publisher/frontend/parser.js'
const xml = (value) => {
  const encoded = new TextEncoder().encode(value)
  const buffer = new ArrayBuffer(encoded.byteLength)
  new Uint8Array(buffer).set(encoded)
  return buffer
}
it('parses RSS inside the package and keeps remote images inert', () => {
  const feed = parseFeed(
    xml(
      `<rss><channel><title>News</title><item><guid>one</guid><title>Article</title><link>https://public.example/article</link><description><![CDATA[<p onclick="bad()">正文</p><img src="/photo.png" onerror="bad()"><script>bad()</script><svg onload="bad()"></svg>]]></description></item></channel></rss>`,
    ),
    'https://public.example/feed',
  )
  expect(feed.title).toBe('News')
  expect(feed.entries).toHaveLength(1)
  const entry = feed.entries[0]
  expect(entry.images).toEqual(['https://public.example/photo.png'])
  expect(entry.html).toContain('data-rss-src=')
  expect(entry.html).not.toMatch(/<script|<svg|onclick|onerror|<img src=/)
  expect(articleHTML(entry)).toContain('src="https://public.example/photo.png"')
})
it('supports Atom XHTML and RSS 1.0 without host parsing code', () => {
  const atom = parseFeed(
    xml(
      `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>1</id><title>A</title><link href="https://public.example/a"/><author><name>Writer</name></author><content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>正文</p><img src="https://public.example/a.png"/></div></content></entry></feed>`,
    ),
    'https://public.example/feed',
  )
  expect(atom.entries[0].author).toBe('Writer')
  expect(atom.entries[0].html).toContain('正文')
  const rss = parseFeed(
    xml(
      `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel><title>RSS1</title></channel><item><title>B</title><description>摘要</description></item></rdf:RDF>`,
    ),
    'https://public.example/feed',
  )
  expect(rss.entries).toHaveLength(1)
})
it('rejects entities, invalid XML, oversized and overly deep sources', () => {
  for (const value of [
    '<!DOCTYPE rss [<!ENTITY x "secret">]><rss/>',
    '<rss><bad></rss>',
    '<unknown/>',
    '<rss>' + '<a>'.repeat(50) + '</a>'.repeat(50) + '</rss>',
  ])
    expect(() => parseFeed(xml(value), 'https://public.example/feed')).toThrow()
  expect(() => parseFeed(new ArrayBuffer(1024 * 1024 + 1), 'https://public.example/feed')).toThrow()
})
