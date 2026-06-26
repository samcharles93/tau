import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      name: 'css-first',
      enforce: 'post',
      transformIndexHtml(html) {
        // Move stylesheet links before the first script tag so CSS loads first.
        // Prevents "Layout was forced before the page was fully loaded" warnings.
        const links: string[] = []
        let out = html.replace(/<link[^>]*rel="stylesheet"[^>]*>/g, (m) => {
          links.push(m)
          return ''
        })
        out = out.replace(/<script/, links.join('\n    ') + '\n    <script')
        return out
      },
    },
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: '../spa/dist',
    emptyOutDir: true,
  },
  base: '/',
  server: {
    // During `pnpm dev`, proxy the WebSocket and health endpoints to the Go
    // backend started with `go run ./cmd/tau --web --port 9343`.
    proxy: {
      '/ws': { target: 'ws://127.0.0.1:9343', ws: true },
      '/health': 'http://127.0.0.1:9343',
    },
  },
})
