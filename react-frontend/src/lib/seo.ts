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
      name: '星链 AI',
      url: `${siteUrl}/`,
    },
    {
      '@type': 'WebSite',
      '@id': `${siteUrl}/#website`,
      url: `${siteUrl}/`,
      name: '星链 AI',
      description: 'GPT、Claude、Gemini 等模型 API 统一接入与中转服务',
      inLanguage: 'zh-CN',
      publisher: { '@id': `${siteUrl}/#organization` },
    },
    {
      '@type': 'SoftwareApplication',
      '@id': `${siteUrl}/#application`,
      name: '星链 AI',
      applicationCategory: 'DeveloperApplication',
      operatingSystem: 'Web',
      url: `${siteUrl}/`,
      description: '面向开发者的 GPT、Claude、Gemini 模型 API 中转与统一接入平台，支持 API Key 管理和调用用量查看。',
      featureList: ['GPT API 中转', 'OpenAI 兼容 API', 'API Key 管理', 'Codex / Claude Code 配置'],
    },
  ],
}

const authTitles: Record<string, string> = {
  '/login': '登录 - 星链 AI',
  '/register': '注册 - 星链 AI',
  '/forgot-password': '找回密码 - 星链 AI',
  '/reset-password': '重置密码 - 星链 AI',
  '/change-password': '修改密码 - 星链 AI',
}

const indexableRobots = 'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1'

const publicPageMetadata: Record<string, Omit<PageMetadata, 'canonical' | 'robots'>> = {
  '/model-market': {
    title: '模型广场与 API 价格 - 星链 AI',
    description: '查看星链 AI 当前公开的 GPT、Claude、Gemini 等可售模型、基础 Token 价格与官方模型系列参考。',
  },
  '/services': {
    title: '服务能力 - 星链 AI',
    description: '了解星链 AI 的统一模型接入、用量查看与 API Key 管理能力。',
  },
  '/service-status': {
    title: '服务状态与用量 - 星链 AI',
    description: '了解星链 AI 提供的服务状态、调用记录、Token 费用与 API Key 额度查看能力。',
  },
  '/faq': {
    title: '常见问题 - 星链 AI',
    description: '查看星链 AI 的适用场景、Codex 接入、费用计算、调用明细和模型可用性说明。',
  },
}

export function getPageMetadata(pathname: string): PageMetadata {
  if (authTitles[pathname]) {
    return {
      title: authTitles[pathname],
      description: '星链 AI 账号入口。',
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
      title: '页面不存在 - 星链 AI',
      description: '未找到请求的星链 AI 页面。',
      canonical: `${siteUrl}${pathname}`,
      robots: 'noindex,nofollow,noarchive',
    }
  }

  return {
    title: '星链 AI｜GPT API 中转站与统一模型 API 平台',
    description:
      '星链 AI 面向开发者提供 GPT、Claude、Gemini 等模型 API 的统一接入与中转服务，支持 OpenAI 兼容 API、API Key 管理、Codex / Claude Code 配置和调用用量查看。',
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
