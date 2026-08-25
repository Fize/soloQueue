import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/',
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    host: '127.0.0.1',
    allowedHosts: true,
    proxy: {
      '/api': 'http://localhost:8765',
      '/healthz': 'http://localhost:8765',
      '/api/runtime-config': 'http://localhost:8765',
      '/ws': {
        target: 'ws://localhost:8765',
        ws: true,
        configure: (proxy) => {
          proxy.on('error', () => {})
        },
      },
    },
  },
})
