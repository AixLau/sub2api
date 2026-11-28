/**
 * Vue 落地页 SEO 元信息管理。
 * 仅在落地页 / 认证页挂载时调用；canonical 域名改为运行时 origin。
 * 页面文案与结构化数据统一来自 `seo/publicPages.ts`（构建期预渲染也用它）。
 */
import { getSeoPage } from '@/seo/publicPages'

type PageMetadata = {
  title: string
  description: string
  canonical: string
  robots: string
  schema?: Record<string, unknown>
}

function getSiteUrl(): string {
  return window.location.origin
}

export function getLandingPageMetadata(pathname: string): PageMetadata {
  const siteUrl = getSiteUrl()
  const { path, page } = getSeoPage(pathname)
  return {
    title: page.title,
    description: page.description,
    canonical: path === '/' ? `${siteUrl}/` : `${siteUrl}${path}`,
    robots: page.robots,
    schema: page.schema?.(siteUrl),
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

export function applyLandingSeo(pathname: string) {
  const metadata = getLandingPageMetadata(pathname)

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
