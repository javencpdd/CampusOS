import { PDFDataRangeTransport } from 'pdfjs-dist/build/pdf.mjs'
import { PluginBridgeClient } from './bridge'

export interface PDFResourceDescription {
  size: number
  name?: string
}

type RangeResult = {
  offset: number
  bytes: ArrayBuffer | Uint8Array
}

// BridgeRangeTransport deliberately contains no URL or fetch implementation.
// Every byte range must be obtained from the authenticated CampusOS host over
// the MessagePort, so the iframe never receives a user token or file path.
export class BridgeRangeTransport extends PDFDataRangeTransport {
  private aborted = false

  constructor(
    private readonly client: PluginBridgeClient,
    description: PDFResourceDescription,
    initialData: Uint8Array,
    private readonly resourceHandle: string,
  ) {
    super(description.size, initialData, false, description.name || '')
  }

  override requestDataRange(begin: number, end: number): void {
    if (this.aborted || begin < 0 || end <= begin || end - begin > 1024 * 1024) return
    void this.client
      .request('resource.readRange', { handle: this.resourceHandle, offset: begin, length: end - begin })
      .then((value) => {
        if (this.aborted) return
        const result = value as RangeResult
        const bytes = result.bytes instanceof Uint8Array ? result.bytes : new Uint8Array(result.bytes)
        if (result.offset !== begin || bytes.byteLength > end - begin) {
          throw new Error('宿主返回的 PDF 分段数据无效。')
        }
        this.onDataRange(begin, bytes)
      })
      .catch(() => this.abort())
  }

  override abort(): void {
    this.aborted = true
    super.abort()
  }
}
