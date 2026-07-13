import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

// Production uses automatic Element Plus component imports. Tests provide
// local stubs instead, so this separate config keeps Node from loading CSS.
export default defineConfig({
	plugins: [vue()],
	resolve: { alias: { '@': resolve(__dirname, 'src') } },
})
