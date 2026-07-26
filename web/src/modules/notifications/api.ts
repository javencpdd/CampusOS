import api from '../../shared/api/client'

export interface UserNotification {
  id: string
  user_id: string
  type: string
  title: string
  content: string
  action_url: string
  is_read: boolean
  read_at?: string
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface NotificationListData {
  items: UserNotification[]
  unread_count: number
  pagination: {
    page: number
    page_size: number
    total: number
    total_pages: number
  }
}

export const notificationApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/notifications', { params }),
  markRead: (id: string) => api.post(`/notifications/${encodeURIComponent(id)}/read`),
  markAllRead: () => api.post('/notifications/read-all'),
}
