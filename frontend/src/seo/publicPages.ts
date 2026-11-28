/**
 * 公开页面 SEO 数据的单一来源。
 *
 * 同时被两处使用：
 * - 浏览器端 `utils/landingSeo.ts`：页面挂载后同步 head；
 * - 构建期 `vite.config.ts` 的 seoPrerender 插件：为每个公开路由生成独立 HTML
 *   （带专属 title / description / canonical / JSON-LD 和正文），供不执行 JS 的
 *   搜索引擎与 AI 爬虫读取。正文在 Vue 挂载时被替换，用户可见页面不变。
 *
 * 本文件不得访问 window / document，且只用相对路径导入（构建期由 Vite SSR 加载）。
 */
import { brandName, capabilities, faqItems, publicPageLinks } from '../data/landing'

export const SEO_SITE_URL = 'https://aixlau.me'

export const INDEXABLE_ROBOTS = 'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1'
export const NOINDEX_ROBOTS = 'noindex,nofollow,noarchive'

export type SeoPage = {
  title: string
  description: string
  robots: string
  /** 构建期预渲染的正文；为空表示只输出 head（如登录页）。 */
  body?: () => string
  schema?: (siteUrl: string) => Record<string, unknown>
}

const HOME_TITLE = '星链AI｜Codex 接入与 GPT API 中转站'
const HOME_DESCRIPTION =
  '星链AI 提供 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini API，支持 API Key 管理、Codex / Claude Code 配置和调用用量查看。'

const statusItems = [
  { label: '服务状态', detail: '查看当前服务可用情况' },
  { label: '调用记录', detail: '查询延迟、Token、费用与错误' },
  { label: '额度管理', detail: '查看余额、订阅与 API Key 用量' },
]

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character] || character)
}

function renderNav(): string {
  const links = [{ label: '首页', href: '/' }, ...publicPageLinks, { label: '接入教程', href: '/docs/install/' }]
  return `<nav aria-label="主导航"><ul>${links
    .map((link) => `<li><a href="${escapeHtml(link.href)}">${escapeHtml(link.label)}</a></li>`)
    .join('')}</ul></nav>`
}

function renderLayout(main: string): string {
  return (
    `<div class="seo-fallback"><header><a href="/"><strong>${escapeHtml(brandName)}</strong></a>${renderNav()}</header>` +
    `<main>${main}</main>` +
    `<footer><p><a href="/register">注册</a> · <a href="/login">登录</a> · <a href="/docs/install/">Codex / Claude Code 接入教程</a></p></footer></div>`
  )
}

function renderHomeBody(): string {
  return renderLayout(
    `<h1>Codex 与模型 API 接入，像光一样自然。</h1>` +
      `<p>星链AI 提供稳定的 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini 等主流模型，减少复杂配置，让团队把注意力留给产品体验。</p>` +
      `<p><a href="/login">开始接入</a> · <a href="/docs/install/">查看接入教程</a></p>` +
      `<section><h2>了解星链AI</h2><ul>${publicPageLinks
        .map(
          (link) =>
            `<li><a href="${escapeHtml(link.href)}">${escapeHtml(link.label)}</a>：${escapeHtml(link.description)}</li>`,
        )
        .join('')}</ul></section>`,
  )
}

function renderServicesBody(): string {
  return renderLayout(
    `<h1>复杂能力，保持简单。</h1><p>覆盖模型使用、用量查看和账号管理等常用需求。</p>` +
      capabilities
        .map(
          (item) =>
            `<section><h2>${escapeHtml(item.title)}</h2><p>${escapeHtml(item.description)}</p><p>${escapeHtml(item.detail)}</p></section>`,
        )
        .join(''),
  )
}

function renderStatusBody(): string {
  return renderLayout(
    `<h1>状态与用量，都有迹可循。</h1><p>登录后可查看服务状态、请求明细和费用信息。</p>` +
      `<ul>${statusItems
        .map((item) => `<li><strong>${escapeHtml(item.label)}</strong>：${escapeHtml(item.detail)}</li>`)
        .join('')}</ul>`,
  )
}

function renderModelMarketBody(): string {
  return renderLayout(
    `<h1>模型与价格，接入前就看清。</h1><p>查看当前可用模型、计费方式与公开价格。</p>` +
      `<p>星链AI 统一接入 OpenAI GPT、Anthropic Claude、Google Gemini 等模型 API，价格按输入、输出与缓存 Token 计费；最新可用模型与价格以本页实时数据和控制台为准。</p>`,
  )
}

function renderFaqBody(): string {
  return renderLayout(
    `<h1>接入前，先了解这些。</h1><p>这里说明产品能力和服务边界；具体模型、套餐与价格以控制台为准。</p>` +
      faqItems
        .map((item) => `<section><h2>${escapeHtml(item.question)}</h2><p>${escapeHtml(item.answer)}</p></section>`)
        .join(''),
  )
}

export function buildHomeSchema(siteUrl: string): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'Organization',
        '@id': `${siteUrl}/#organization`,
        name: '星链AI',
        alternateName: ['星链 AI', 'AIXLAU'],
        url: `${siteUrl}/`,
        logo: `${siteUrl}/logo.png`,
      },
      {
        '@type': 'WebSite',
        '@id': `${siteUrl}/#website`,
        url: `${siteUrl}/`,
        name: '星链AI',
        alternateName: '星链 AI',
        description:
          '面向开发者的 Codex 接入与 GPT API 中转站，提供 OpenAI、Claude、Gemini API 统一中转与兼容接入',
        inLanguage: 'zh-CN',
        publisher: { '@id': `${siteUrl}/#organization` },
      },
      {
        '@type': 'SoftwareApplication',
        '@id': `${siteUrl}/#application`,
        name: '星链AI',
        alternateName: '星链 AI',
        applicationCategory: 'DeveloperApplication',
        operatingSystem: 'Web',
        url: `${siteUrl}/`,
        description:
          '面向开发者的 GPT API 中转站，支持 OpenAI API、Claude API、Gemini API 和 Codex 的统一中转接入。',
        keywords: 'GPT API 中转站,OpenAI API 中转,Claude API 中转,Gemini API 中转,Codex 中转站,AI API 中转站',
        featureList: [
          'GPT API 中转站',
          'OpenAI API 中转',
          'Claude API 中转',
          'Gemini API 中转',
          'Codex / Claude Code 配置',
          'API Key 管理',
        ],
      },
    ],
  }
}

function buildFaqSchema(): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((item) => ({
      '@type': 'Question',
      name: item.question,
      acceptedAnswer: { '@type': 'Answer', text: item.answer },
    })),
  }
}

function authPage(title: string): SeoPage {
  return { title, description: '星链AI 账号入口。', robots: NOINDEX_ROBOTS }
}

export const seoPages: Record<string, SeoPage> = {
  '/': {
    title: HOME_TITLE,
    description: HOME_DESCRIPTION,
    robots: INDEXABLE_ROBOTS,
    body: renderHomeBody,
    schema: buildHomeSchema,
  },
  '/model-market': {
    title: 'GPT、Claude、Gemini API 模型与价格 - 星链AI',
    description:
      '查看星链AI GPT API 中转站当前公开的 OpenAI、Claude、Gemini 等可售模型、API 标识与基础 Token 价格。',
    robots: INDEXABLE_ROBOTS,
    body: renderModelMarketBody,
  },
  '/services': {
    title: 'AI API 中转站服务能力 - 星链AI',
    description: '了解星链AI 的 GPT、OpenAI、Claude、Gemini API 中转接入、用量查看与 API Key 管理能力。',
    robots: INDEXABLE_ROBOTS,
    body: renderServicesBody,
  },
  '/service-status': {
    title: 'API 中转服务状态与用量 - 星链AI',
    description: '了解星链AI API 中转服务的可用状态、调用记录、Token 费用与 API Key 额度查看能力。',
    robots: INDEXABLE_ROBOTS,
    body: renderStatusBody,
  },
  '/faq': {
    title: 'GPT API 中转站常见问题 - 星链AI',
    description:
      '了解 GPT API 中转站的适用场景，以及星链AI 的 OpenAI、Claude、Gemini、Codex 接入、费用和调用明细。',
    robots: INDEXABLE_ROBOTS,
    body: renderFaqBody,
    schema: buildFaqSchema,
  },
  '/login': authPage('登录 - 星链AI'),
  '/register': authPage('注册 - 星链AI'),
  '/forgot-password': authPage('找回密码 - 星链AI'),
  '/reset-password': authPage('重置密码 - 星链AI'),
}

/** 未登记路径（含首页别名）一律按首页处理，与原 landingSeo 行为一致。 */
export function getSeoPage(pathname: string): { path: string; page: SeoPage } {
  const page = seoPages[pathname]
  return page ? { path: pathname, page } : { path: '/', page: seoPages['/'] }
}

export const SEO_FALLBACK_STYLE =
  '.seo-fallback{max-width:960px;margin:0 auto;padding:24px 16px;font-family:system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;line-height:1.7;color:#1f2937}' +
  '.seo-fallback a{color:#2563eb}.seo-fallback nav ul{display:flex;flex-wrap:wrap;gap:12px;list-style:none;padding:0}'
