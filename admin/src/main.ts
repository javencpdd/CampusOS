import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import './styles/responsive.css'
import App from './App.vue'
import router from './router'
import { useAdminStore } from './modules/identity/store'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
const adminStore = useAdminStore(pinia)
app.use(router)

void adminStore.restore().finally(() => app.mount('#app'))
