import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

// Component tests supply explicit Element Plus stubs. Keeping the production
// auto-component resolver out of this config avoids loading component CSS in
// Node while still exercising the Vue view logic.
export default defineConfig({
	plugins: [vue()],
	resolve: { alias: { '@': resolve(__dirname, 'src') } },
})
