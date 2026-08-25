import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? 'http://localhost:30051',
        changeOrigin: true,
      },
      '/healthz': {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? 'http://localhost:30051',
        changeOrigin: true,
      },
      '/livez': {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? 'http://localhost:30051',
        changeOrigin: true,
      },
      '/readyz': {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? 'http://localhost:30051',
        changeOrigin: true,
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})
