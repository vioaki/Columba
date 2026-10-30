import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import path from 'node:path'

// Production assets are embedded under /ui/. Development proxies to the local backend.
function devProxyTarget(): string {
  const raw = process.env.COLUMBA_DEV_PROXY?.trim()
  if (raw) return raw
  return 'http://127.0.0.1:8765'
}

export default defineConfig(({ command }) => {
  const proxyTarget = devProxyTarget()
  const proxy = { target: proxyTarget, changeOrigin: true }
  return {
    base: command === 'build' ? '/ui/' : '/',
    publicDir: 'brand',
    plugins: [react()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
      },
    },
    server: {
      host: '127.0.0.1',
      port: 5273,
      proxy: {
        '/api': proxy,
        '/v1': proxy,
        '/health': proxy,
        // 诊断页的 pprof 链接是绝对路径，开发期也要能打开。
        '/debug': proxy,
      },
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      chunkSizeWarningLimit: 1200,
      rollupOptions: {
        output: {
          // 图表库与 React 运行时独立成 chunk：前者只随图表页按需加载，
          // 后者长期缓存不随业务代码发版失效。
          manualChunks: {
            charts: ['recharts'],
            vendor: ['react', 'react-dom', 'react-router-dom', 'swr'],
          },
        },
      },
    },
  }
})
