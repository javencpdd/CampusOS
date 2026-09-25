import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

const apiProxyTarget = process.env.CAMPUSOS_API_PROXY_TARGET || 'http://localhost:8080'

export default defineConfig({
	plugins: [vue(), Components({ resolvers: [ElementPlusResolver()], dts: false })],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 3001,
    proxy: {
      '/api': {
        target: apiProxyTarget,
        // Keep the browser's Host header for v4 isolated-plugin UI origins.
        // `api:8080` is a Docker-only name and must never reach the browser.
        changeOrigin: false,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          framework: ['vue', 'vue-router', 'pinia'],
        },
      },
    },
  },
})
