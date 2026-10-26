import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import './styles/responsive.css'
import App from './App.vue'
import router from './router'
import { useUserStore } from './modules/identity/store'
import { receivePreviewSessionHandoff } from './modules/identity/previewSessionHandoff'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
const userStore = useUserStore(pinia)
void (async () => {
  const token = await receivePreviewSessionHandoff()
  if (token) userStore.updateAccessToken(token)
  app.use(router)
  await userStore.restore()
})().finally(() => app.mount('#app'))
