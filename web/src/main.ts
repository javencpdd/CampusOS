import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import './styles/responsive.css'
import App from './App.vue'
import router from './router'
import { useUserStore } from './modules/identity/store'
import { receivePreviewSessionHandoff } from './modules/identity/previewSessionHandoff'
import { useUIRuntimeStore } from './modules/plugin-runtime/store'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
const userStore = useUserStore(pinia)
void (async () => {
  const token = await receivePreviewSessionHandoff()
  if (token) userStore.updateAccessToken(token)
  await userStore.restore()
  await useUIRuntimeStore(pinia).sync()
})().finally(() => {
  app.use(router)
  app.mount('#app')
})
