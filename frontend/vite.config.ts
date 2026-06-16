import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In `vite dev`, the frontend talks to a running core via the proxy below.
// Set CORE_PORT to the port of a separately started `core` process.
const corePort = process.env.CORE_PORT ?? 0
const target = `http://127.0.0.1:${corePort}`

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target, changeOrigin: true },
      '/events': { target, changeOrigin: true },
      '/healthz': { target, changeOrigin: true },
    },
  },
  build: { outDir: 'dist' },
})
