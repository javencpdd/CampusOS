// Compatibility facade. New Admin code imports the owning domain module.
export { default } from '../shared/api/client'
export { authApi, userApi, roleApi, moderationApi } from '../modules/identity/api'
export { threadApi, richTextAdminApi, categoryApi } from '../modules/community/api'
export { pluginApi } from '../modules/plugins/api'
export { homeStylePackApi } from '../modules/appearance/api'
export { integrationApi, spaceAdminApi, webhookApi, mcpApi, messageApi } from '../modules/integrations/api'
export { eventApi, platformLogApi, healthApi } from '../modules/operations/api'
