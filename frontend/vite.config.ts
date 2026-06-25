import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In `vite dev`, the frontend talks to a running core via the proxy below.
// CORE_PORT selects the core to proxy to; defaults to 7777 (the conventional
// dev core port) so `npm run dev` works without extra configuration. In the
// packaged Wails app the port comes from the PortBinder binding instead.
const corePort = process.env.CORE_PORT ?? '7777'
const target = `http://127.0.0.1:${corePort}`

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target, changeOrigin: true, ws: true },
      '/events': { target, changeOrigin: true },
      '/healthz': { target, changeOrigin: true },
    },
  },
  build: { outDir: 'dist' },
})
