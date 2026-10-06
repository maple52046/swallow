import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  server: {
    // Bind all interfaces and accept any Host header so the dev server works behind the
    // Cursor preview proxy / container hostname (Vite 7 blocks non-localhost hosts by default).
    host: true,
    allowedHosts: true,
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
      // Server Enrollment downloads (the script and the CLI). The original Host is kept so the
      // script points its CLI download back at this dev server.
      '/downloads': {
        target: process.env.VITE_DEV_API_PROXY_TARGET ?? 'http://localhost:30051',
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})
