const siteUrl = 'https://aixlau.me'

type PageMetadata = {
  title: string
  description: string
  canonical: string
  robots: string
  schema?: Record<string, unknown>
}

const homeSchema = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'Organization',
      '@id': `${siteUrl}/#organization`,
      name: '星链AI',
      alternateName: ['星链 AI', 'AIXLAU'],
      url: `${siteUrl}/`,
    },
    {
      '@type': 'WebSite',
      '@id': `${siteUrl}/#website`,
      url: `${siteUrl}/`,
      name: '星链AI',
      alternateName: '星链 AI',
      description: '面向开发者的 Codex 接入与 GPT API 中转站，提供 OpenAI、Claude、Gemini API 统一中转与兼容接入',
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
      description: '面向开发者的 GPT API 中转站，支持 OpenAI API、Claude API、Gemini API 和 Codex 的统一中转接入。',
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

const authTitles: Record<string, string> = {
  '/login': '登录 - 星链AI',
  '/register': '注册 - 星链AI',
  '/forgot-password': '找回密码 - 星链AI',
  '/reset-password': '重置密码 - 星链AI',
  '/change-password': '修改密码 - 星链AI',
}

const indexableRobots = 'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1'

const publicPageMetadata: Record<string, Omit<PageMetadata, 'canonical' | 'robots'>> = {
  '/model-market': {
    title: 'GPT、Claude、Gemini API 模型与价格 - 星链AI',
    description: '查看星链AI GPT API 中转站当前公开的 OpenAI、Claude、Gemini 等可售模型、API 标识与基础 Token 价格。',
  },
  '/services': {
    title: 'AI API 中转站服务能力 - 星链AI',
    description: '了解星链AI 的 GPT、OpenAI、Claude、Gemini API 中转接入、用量查看与 API Key 管理能力。',
  },
  '/service-status': {
    title: 'API 中转服务状态与用量 - 星链AI',
    description: '了解星链AI API 中转服务的可用状态、调用记录、Token 费用与 API Key 额度查看能力。',
  },
  '/faq': {
    title: 'GPT API 中转站常见问题 - 星链AI',
    description: '了解 GPT API 中转站的适用场景，以及星链AI 的 OpenAI、Claude、Gemini、Codex 接入、费用和调用明细。',
  },
}

export function getPageMetadata(pathname: string): PageMetadata {
  if (authTitles[pathname]) {
    return {
      title: authTitles[pathname],
      description: '星链AI 账号入口。',
      canonical: `${siteUrl}${pathname}`,
      robots: 'noindex,nofollow,noarchive',
    }
  }

  if (publicPageMetadata[pathname]) {
    return {
      ...publicPageMetadata[pathname],
      canonical: `${siteUrl}${pathname}`,
      robots: indexableRobots,
    }
  }

  if (pathname !== '/' && pathname !== '/home') {
    return {
      title: '页面不存在 - 星链AI',
      description: '未找到请求的星链AI 页面。',
      canonical: `${siteUrl}${pathname}`,
      robots: 'noindex,nofollow,noarchive',
    }
  }

  return {
    title: '星链AI｜Codex 接入与 GPT API 中转站',
    description:
      '星链AI 提供 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini API，支持 API Key 管理、Codex / Claude Code 配置和调用用量查看。',
    canonical: `${siteUrl}/`,
    robots: indexableRobots,
    schema: homeSchema,
  }
}

function upsertMeta(attribute: 'name' | 'property', key: string, content: string) {
  let element = document.head.querySelector<HTMLMetaElement>(`meta[${attribute}="${key}"]`)
  if (!element) {
    element = document.createElement('meta')
    element.setAttribute(attribute, key)
    document.head.appendChild(element)
  }
  element.content = content
}

function upsertLink(rel: string, href: string) {
  let element = document.head.querySelector<HTMLLinkElement>(`link[rel="${rel}"]`)
  if (!element) {
    element = document.createElement('link')
    element.rel = rel
    document.head.appendChild(element)
  }
  element.href = href
}

export function applySeoMetadata(metadata: PageMetadata) {
  document.title = metadata.title
  upsertMeta('name', 'description', metadata.description)
  upsertMeta('name', 'robots', metadata.robots)
  upsertMeta('property', 'og:title', metadata.title)
  upsertMeta('property', 'og:description', metadata.description)
  upsertMeta('property', 'og:url', metadata.canonical)
  upsertMeta('property', 'og:type', 'website')
  upsertMeta('name', 'twitter:title', metadata.title)
  upsertMeta('name', 'twitter:description', metadata.description)
  upsertMeta('name', 'twitter:card', 'summary_large_image')
  upsertLink('canonical', metadata.canonical)

  document.getElementById('site-schema')?.remove()
  if (metadata.schema) {
    const schema = document.createElement('script')
    schema.id = 'site-schema'
    schema.type = 'application/ld+json'
    schema.textContent = JSON.stringify(metadata.schema)
    document.head.appendChild(schema)
  }
}
