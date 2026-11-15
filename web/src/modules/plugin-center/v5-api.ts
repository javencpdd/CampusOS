import { randomRequestID } from '../../../../sdk/typescript/src/random-request-id'
import api from '../../shared/api/client'
import type { ConfigEntry, ConfigValue, Coordinate } from '../../../../sdk/typescript/src/v5-configuration-form'
import type { ConfigOptions } from '../../../../sdk/typescript/src/v5-configuration-form'
import { parseV5Envelope, stringifyExactJSON } from '../../../../sdk/typescript/src/exact-json'
const wire = { transformResponse: [parseV5Envelope] }

const family = (c: Coordinate) => `/plugin-v5/${encodeURIComponent(c.publisher)}/${encodeURIComponent(c.plugin_key)}`
interface Envelope<T> {
  code: number
  data: T
}
// Only the current user token and user CSRF nonce are supplied by this client.
export const pluginV5Api = {
  list: (after = '') =>
    api.get<never, Envelope<{ items: ConfigEntry[]; next_cursor: string }>>('/plugin-v5', {
      ...wire,
      params: { after, limit: 25 },
    }),
  detail: (c: Coordinate) =>
    api.get<never, Envelope<ConfigEntry>>(`${family(c)}/${encodeURIComponent(c.version)}`, wire),
  companionTool: (platform: string) =>
    api.get<never, Blob>(`/web-companion/tools/${encodeURIComponent(platform)}`, {
      responseType: 'blob',
      timeout: 30000,
    }),
  uiAction: (c: Coordinate, data: Record<string, unknown>) =>
    api.post<never, Envelope<any>>(`${family(c)}/${encodeURIComponent(c.version)}/ui-actions`, data, {
      timeout: 22000,
    }),
  uiState: (c: Coordinate) =>
    api.get<never, Envelope<{ proof: string }>>(`${family(c)}/${encodeURIComponent(c.version)}/ui`, wire),
  uiManifest: () =>
    api.get<never, Envelope<{ plugins: any[]; frame_src: string }>>('/plugin-v5/ui', {
      ...wire,
      params: { parent_origin: window.location.origin },
    }),
  options: (c: Coordinate) =>
    api.get<never, Envelope<ConfigOptions>>(`${family(c)}/${encodeURIComponent(c.version)}/config-options`, wire),
  command: (c: Coordinate, intent: Record<string, unknown>) =>
    api.post<never, Envelope<{ id: string; status: string }>>(
      `${family(c)}/commands`,
      stringifyExactJSON({ ...intent, version: c.version, idempotency_key: randomRequestID() }),
      wire,
    ),
  configureSecrets: (c: Coordinate, revision: string, secret_refs: Record<string, string>) =>
    api.post<never, Envelope<{ id: string; status: string }>>(
      `${family(c)}/commands`,
      stringifyExactJSON({
        kind: 'configure',
        version: c.version,
        expected_revision: revision,
        idempotency_key: randomRequestID(),
        configuration_part: 'secret_refs',
        secret_refs,
      }),
      wire,
    ),
  configure: (c: Coordinate, revision: string, values: Record<string, ConfigValue>, uiProof?: string) =>
    api.post<never, Envelope<{ id: string; status: string }>>(
      `${family(c)}/commands`,
      stringifyExactJSON({
        kind: 'configure',
        version: c.version,
        expected_revision: revision,
        idempotency_key: randomRequestID(),
        values,
        configuration_part: 'values',
      }),
      { ...wire, ...(uiProof ? { headers: { 'X-Plugin-UI-Proof': uiProof } } : {}) },
    ),
}
