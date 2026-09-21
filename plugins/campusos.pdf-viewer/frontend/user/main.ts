import { getDocument, GlobalWorkerOptions } from 'pdfjs-dist/build/pdf.mjs'
import workerSource from 'pdfjs-dist/build/pdf.worker.mjs?url'
import type { PDFPageProxy } from 'pdfjs-dist'
import { PluginBridgeClient } from '../shared/bridge'
import { BridgeRangeTransport, type PDFResourceDescription } from '../shared/pdf-range-transport'

const app = document.querySelector<HTMLElement>('#app')
const query = new URLSearchParams(window.location.search)
const hostOrigin = query.get('host_origin') || ''
const resourceHandle = query.get('resource_handle') || ''

function show(message: string) {
  if (app) app.textContent = message
}

function renderCanvas(canvas: HTMLCanvasElement, page: PDFPageProxy) {
  const viewport = page.getViewport({ scale: 1.25 })
  canvas.width = Math.ceil(viewport.width)
  canvas.height = Math.ceil(viewport.height)
  return page.render({ canvasContext: canvas.getContext('2d')!, viewport }).promise
}

async function renderPDF(client: PluginBridgeClient, handle: string) {
  const description = (await client.request('resource.describe', { handle })) as PDFResourceDescription
  if (!Number.isSafeInteger(description.size) || description.size < 5) throw new Error('宿主未返回有效的 PDF 文件大小。')
  const initialLength = Math.min(description.size, 1024 * 1024)
  const first = (await client.request('resource.readRange', { handle, offset: 0, length: initialLength })) as {
    offset: number
    bytes: ArrayBuffer | Uint8Array
  }
  const initialData = first.bytes instanceof Uint8Array ? first.bytes : new Uint8Array(first.bytes)
  if (first.offset !== 0 || initialData.byteLength !== initialLength) throw new Error('宿主返回的 PDF 首段数据无效。')

  GlobalWorkerOptions.workerSrc = workerSource
  const transport = new BridgeRangeTransport(client, description, initialData, handle)
  const loadingTask = getDocument({
    length: description.size,
    range: transport,
    disableAutoFetch: true,
    disableStream: true,
  })
  const pdf = await loadingTask.promise
  const page = await pdf.getPage(1)
  const canvas = window.document.createElement('canvas')
  await renderCanvas(canvas, page)
  app?.replaceChildren(canvas)
  await page.cleanup()
  await pdf.destroy()
  await loadingTask.destroy()
}

if (!hostOrigin || new URL(hostOrigin).origin !== hostOrigin || window.parent === window) {
  show('预览必须由 CampusOS 宿主在隔离页面中打开。')
} else {
  const client = new PluginBridgeClient(
    {
      pluginKey: 'campusos.pdf-viewer',
      pluginVersion: '2.0.0-dev.1',
      surfaceID: 'preview',
      audience: 'user',
    },
    hostOrigin,
  )
  client
    .connect()
    .then(async () => {
      if (!resourceHandle) {
        show('PDF 阅读器已就绪。请选择一个由宿主授权的 PDF 文件。')
        return
      }
      await renderPDF(client, resourceHandle)
    })
    .catch((error: Error) => show(error.message))
  window.addEventListener('beforeunload', () => client.close())
}
