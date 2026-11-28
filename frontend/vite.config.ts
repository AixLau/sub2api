import { createServer, defineConfig, loadEnv, Plugin } from 'vite'
import { mkdir, readFile, writeFile } from 'fs/promises'
import vue from '@vitejs/plugin-vue'
import checker from 'vite-plugin-checker'
import { resolve } from 'path'

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character] || character)
}

function isSafeImageUrl(value: string): boolean {
  const trimmed = value.trim()
  if ((trimmed.startsWith('/') && !trimmed.startsWith('//')) || /^data:image\//i.test(trimmed)) {
    return true
  }
  try {
    const parsed = new URL(trimmed)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function injectBranding(html: string, config: { site_name?: string; site_logo?: string }): string {
  let brandedHtml = html
  const siteName = config.site_name?.trim()
  if (siteName) {
    brandedHtml = brandedHtml.replace(
      /<title>[^<]*<\/title>/i,
      `<title>${escapeHtml(siteName)} - AI API Gateway</title>`,
    )
  }

  const siteLogo = config.site_logo?.trim()
  if (siteLogo && isSafeImageUrl(siteLogo)) {
    brandedHtml = brandedHtml.replace(
      /<link\s+rel=["']icon["'][^>]*>/i,
      `<link rel="icon" href="${escapeHtml(siteLogo)}" />`,
    )
  }
  return brandedHtml
}

/**
 * Vite 插件：开发模式下注入公开配置到 index.html
 * 与生产模式的后端注入行为保持一致，消除闪烁
 */
function injectPublicSettings(backendUrl: string): Plugin {
  return {
    name: 'inject-public-settings',
    apply: 'serve',
    transformIndexHtml: {
      order: 'pre',
      async handler(html) {
        try {
          const response = await fetch(`${backendUrl}/api/v1/settings/public`, {
            signal: AbortSignal.timeout(2000)
          })
          if (response.ok) {
            const data = await response.json()
            if (data.code === 0 && data.data) {
              const script = `<script>window.__APP_CONFIG__=${JSON.stringify(data.data)};</script>`
              return injectBranding(html, data.data).replace('</head>', `${script}\n</head>`)
            }
          }
        } catch (e) {
          console.warn('[vite] 无法获取公开配置，将回退到 API 调用:', (e as Error).message)
        }
        return html
      }
    }
  }
}

function replaceMetaContent(html: string, attribute: 'name' | 'property', key: string, content: string): string {
  const pattern = new RegExp(`<meta\\s+${attribute}="${key}"[\\s\\S]*?/>`, 'i')
  const tag = `<meta ${attribute}="${key}" content="${escapeHtml(content)}" />`
  return pattern.test(html) ? html.replace(pattern, tag) : html.replace('</head>', `    ${tag}\n  </head>`)
}

/**
 * Vite 插件：构建结束后为公开路由生成独立 HTML（outDir/_seo/*.html）。
 * 后端按请求路径选用对应文件作为模板，让不执行 JS 的搜索引擎和 AI 爬虫也能读到
 * 专属 title / description / canonical / JSON-LD 与正文。数据来自 src/seo/publicPages.ts。
 */
// 与 src/seo/publicPages.ts 的导出保持一致；不直接 import 类型，避免把 src 拉进 tsconfig.node 项目。
type SeoPagesModule = {
  SEO_SITE_URL: string
  SEO_FALLBACK_STYLE: string
  seoPages: Record<
    string,
    {
      title: string
      description: string
      robots: string
      body?: () => string
      schema?: (siteUrl: string) => Record<string, unknown>
    }
  >
}

function seoPrerender(outDir: string): Plugin {
  return {
    name: 'seo-prerender',
    apply: 'build',
    async closeBundle() {
      const loader = await createServer({
        configFile: false,
        logLevel: 'error',
        appType: 'custom',
        server: { middlewareMode: true, hmr: false },
        optimizeDeps: { noDiscovery: true, include: [] },
      })
      try {
        const seo = (await loader.ssrLoadModule(resolve(__dirname, 'src/seo/publicPages.ts'))) as SeoPagesModule
        const template = await readFile(resolve(outDir, 'index.html'), 'utf-8')
        const seoDir = resolve(outDir, '_seo')
        await mkdir(seoDir, { recursive: true })

        for (const [path, page] of Object.entries(seo.seoPages)) {
          const canonical = path === '/' ? `${seo.SEO_SITE_URL}/` : `${seo.SEO_SITE_URL}${path}`
          let html = template.replace(/<title>[^<]*<\/title>/i, `<title>${escapeHtml(page.title)}</title>`)
          html = replaceMetaContent(html, 'name', 'description', page.description)
          html = replaceMetaContent(html, 'name', 'robots', page.robots)
          html = replaceMetaContent(html, 'property', 'og:title', page.title)
          html = replaceMetaContent(html, 'property', 'og:description', page.description)
          html = replaceMetaContent(html, 'property', 'og:url', canonical)
          html = replaceMetaContent(html, 'property', 'og:image', `${seo.SEO_SITE_URL}/og-image.png`)
          html = replaceMetaContent(html, 'name', 'twitter:title', page.title)
          html = replaceMetaContent(html, 'name', 'twitter:description', page.description)
          html = replaceMetaContent(html, 'name', 'twitter:image', `${seo.SEO_SITE_URL}/og-image.png`)

          let headExtra = `<link rel="canonical" href="${escapeHtml(canonical)}" />`
          const schema = page.schema?.(seo.SEO_SITE_URL)
          if (schema) {
            // JSON-LD 不是可执行脚本，不受 CSP script-src 限制；转义 < 防止提前闭合标签。
            const json = JSON.stringify(schema).replace(/</g, '\\u003c')
            headExtra += `\n    <script type="application/ld+json" id="site-schema">${json}</script>`
          }
          if (page.body) {
            headExtra += `\n    <style>${seo.SEO_FALLBACK_STYLE}</style>`
            html = html.replace('<div id="app"></div>', `<div id="app">${page.body()}</div>`)
          }
          html = html.replace('</head>', `    ${headExtra}\n  </head>`)

          const fileName = path === '/' ? 'index.html' : `${path.slice(1)}.html`
          await writeFile(resolve(seoDir, fileName), html)
        }
      } finally {
        await loader.close()
      }
    },
  }
}

export default defineConfig(({ mode }) => {
  // 加载环境变量
  const env = loadEnv(mode, process.cwd(), '')
  const backendUrl = env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080'
  const devPort = Number(env.VITE_DEV_PORT || 3000)

  return {
    plugins: [
      vue(),
      checker({
        vueTsc: true
      }),
      injectPublicSettings(backendUrl),
      seoPrerender(resolve(__dirname, '../backend/internal/web/dist'))
    ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      // 使用 vue-i18n 运行时版本，避免 CSP unsafe-eval 问题；
      // v10 起 JIT 编译恒开启，消息编译为 AST 对象而非 JS 代码，无需 unsafe-eval
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js'
    }
  },
  build: {
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        dashboardComponentsPreview: resolve(__dirname, 'dashboard-components-preview.html'),
        platformGaugePreview: resolve(__dirname, 'platform-gauge-preview.html'),
      },
      output: {
        /**
         * 手动分包配置
         * 分离第三方库并按功能合并应用代码，避免循环依赖
         */
        manualChunks(id: string) {
          if (id.includes('node_modules')) {
            // Vue 核心库
            if (
              id.includes('/vue/') ||
              id.includes('/vue-router/') ||
              id.includes('/pinia/') ||
              id.includes('/@vue/')
            ) {
              return 'vendor-vue'
            }

            // UI 工具库（较大，单独分离）
            if (id.includes('/@vueuse/') || id.includes('/xlsx/')) {
              return 'vendor-ui'
            }

            // 图表库
            if (
              id.includes('/chart.js/') ||
              id.includes('/vue-chartjs/') ||
              id.includes('/@antv/') ||
              id.includes('/@ant-design/plots/') ||
              id.includes('/@ant-design/charts-util/') ||
              id.includes('/react/') ||
              id.includes('/react-dom/')
            ) {
              return 'vendor-chart'
            }

            // 国际化
            if (id.includes('/vue-i18n/') || id.includes('/@intlify/')) {
              return 'vendor-i18n'
            }

            // Stripe 仅在支付流程中按需加载，避免进入首页公共依赖。
            if (id.includes('/@stripe/stripe-js/')) {
              return 'vendor-stripe'
            }

            // 其他小型第三方库合并
            return 'vendor-misc'
          }

          // 应用代码：按入口点自动分包，不手动干预
          // 这样可以避免循环依赖，同时保持合理的 chunk 数量
        }
      }
    }
  },
    server: {
      host: '0.0.0.0',
      port: devPort,
      proxy: {
        '/api': {
          target: backendUrl,
          changeOrigin: true
        },
        '/v1': {
          target: backendUrl,
          changeOrigin: true
        },
        '/setup': {
          target: backendUrl,
          changeOrigin: true
        }
      }
    }
  }
})
