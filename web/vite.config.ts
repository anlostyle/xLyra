import path from 'node:path'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, loadEnv } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// 广告/隐私类内容拦截规则（EasyPrivacy 等）会按文件名拦截 analytics-*.js 之类的请求，
// 导致 iOS Safari 懒加载页面时报 "Importing a module script failed"，所以产物名里避开这些词
const BLOCKER_PRONE_CHUNK_WORDS: Array<[RegExp, string]> = [
  [/analytics/gi, 'usage-stats'],
  [/tracking/gi, 'flow-trace'],
  [/telemetry/gi, 'runtime-info'],
  [/beacon/gi, 'signal'],
]

function safeChunkName(name: string) {
  return BLOCKER_PRONE_CHUNK_WORDS.reduce((current, [pattern, replacement]) => current.replace(pattern, replacement), name)
}

// https://vite.dev/config/
export default defineConfig(({ command, mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxyTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:5801'
  const buildTimestamp =
    command === 'build' ? env.VITE_BUILD_TIMESTAMP || Date.now().toString() : 'development'

  return {
    plugins: [
      react(),
      tailwindcss(),
      VitePWA({
        includeAssets: [
          'apple-touch-icon.png',
          'favicon-16.png',
          'favicon-32.png',
          'logo.png',
        ],
        injectRegister: false,
        registerType: 'prompt',
        manifest: {
          name: 'xLyra',
          short_name: 'xLyra',
          description: 'xLyra 管理控制台',
          lang: 'zh-CN',
          theme_color: '#f4f7fb',
          background_color: '#f4f7fb',
          display: 'standalone',
          scope: '/',
          start_url: '/',
          icons: [
            {
              src: '/pwa-192.png',
              sizes: '192x192',
              type: 'image/png',
            },
            {
              src: '/pwa-512.png',
              sizes: '512x512',
              type: 'image/png',
            },
            {
              src: '/pwa-maskable-512.png',
              sizes: '512x512',
              type: 'image/png',
              purpose: 'maskable',
            },
          ],
        },
        workbox: {
          cleanupOutdatedCaches: true,
          clientsClaim: true,
          globIgnores: ['**/node_modules/**/*', '**/version.json', '**/index.html'],
          manifestTransforms: [
            async (entries) => ({
              manifest: entries.map((entry) =>
                entry.url === 'index.html' || entry.url.endsWith('/index.html')
                  ? {
                      ...entry,
                      revision: `${entry.revision ?? 'index'}-${buildTimestamp}`,
                    }
                  : entry,
              ),
            }),
          ],
        },
      }),
      {
        name: 'xlyra-build-id',
        generateBundle() {
          this.emitFile({
            type: 'asset',
            fileName: 'version.json',
            source: JSON.stringify({ build: buildTimestamp }),
          })
        },
      },
    ],
    define: {
      'import.meta.env.VITE_BUILD_ID': JSON.stringify(buildTimestamp),
    },
    build: {
      rolldownOptions: {
        output: {
          chunkFileNames: (chunk) => `assets/${safeChunkName(chunk.name)}-[hash].js`,
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    optimizeDeps: {
      include: ['react-is', 'recharts'],
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      strictPort: false,
      proxy: {
        '/api/playground': proxyTarget,
        '/api/v1': proxyTarget,
        '/v1': proxyTarget,
        '/healthz': proxyTarget,
        '/readyz': proxyTarget,
        '/debug': proxyTarget,
      },
    },
  }
})
