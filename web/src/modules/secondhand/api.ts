import api from '../../shared/api/client'

export type ItemCondition = 'new' | 'like_new' | 'good' | 'fair'
export type TradeMethod = 'in_person' | 'campus_dropoff' | 'other'
export type TradeStatus = 'available' | 'reserved' | 'sold' | 'closed'

export type SecondhandDetail = {
  thread_id: string
  price_minor: number
  currency: 'CNY'
  item_condition: ItemCondition
  trade_method: TradeMethod
  trade_status: TradeStatus
  location_scope?: string
  version: number
  created_by: string
  created_at: string
  updated_at: string
}

export type SecondhandResult = {
  thread: Record<string, any>
  detail: SecondhandDetail
}

export type SecondhandRequest = {
  title: string
  content: string
  content_format: 'markdown' | 'safe_html'
  category_id?: string
  tags?: string[]
  price_minor: number
  currency?: 'CNY'
  item_condition: ItemCondition
  trade_method: TradeMethod
  location_scope?: string
  version?: number
}

export const secondhandApi = {
  status: () => api.get('/secondhand/status'),
  list: (params?: { page?: number; page_size?: number; category_id?: string; keyword?: string; tag?: string }) =>
    api.get('/secondhand/threads', { params }),
  get: (id: string) => api.get('/secondhand/threads/' + id),
  getMine: (id: string) => api.get('/secondhand/threads/' + id + '/me'),
  create: (data: SecondhandRequest) => api.post('/secondhand/threads', data),
  update: (id: string, data: SecondhandRequest) => api.put('/secondhand/threads/' + id, data),
  updateStatus: (id: string, data: { trade_status: TradeStatus; version: number }) =>
    api.post('/secondhand/threads/' + id + '/status', data),
}
