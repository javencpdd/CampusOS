import { defineConfig } from 'vitest/config'
import { resolve } from 'node:path'

export default defineConfig({
  // This bundle is served below the immutable release path
  // `/plugins/<key>/<version-digest>/ui/<audience>/`.  A root-absolute Vite
  // base would request `/assets/...`, outside the restricted plugin gateway.
  // Relative URLs keep every entry, chunk and worker inside its release.
  base: './',
  build: {
    outDir: resolve(__dirname, '../dist'),
    emptyOutDir: true,
    rollupOptions: {
      input: {
        user: resolve(__dirname, 'user/index.html'),
        admin: resolve(__dirname, 'admin/index.html'),
      },
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
  test: { environment: 'jsdom' },
})
