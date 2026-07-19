import '@testing-library/jest-dom/vitest'
import { afterEach, describe, expect, it } from 'vitest'
import { applySeoMetadata, getPageMetadata } from './seo'

afterEach(() => {
  document.head.innerHTML = ''
})

describe('SEO metadata', () => {
  it('returns indexable metadata and schema for the homepage', () => {
    const metadata = getPageMetadata('/')

    expect(metadata.title).toBe('星链AI｜GPT API 中转站与统一模型 API 平台')
    expect(metadata.robots).toContain('index,follow')
    expect(metadata.canonical).toBe('https://aixlau.me/')
    expect(metadata.schema).toMatchObject({ '@context': 'https://schema.org' })
    expect(JSON.stringify(metadata.schema)).toContain('OpenAI API 中转')
  })

  it('returns noindex metadata for account routes', () => {
    const metadata = getPageMetadata('/login')

    expect(metadata.title).toBe('登录 - 星链AI')
    expect(metadata.robots).toBe('noindex,nofollow,noarchive')
    expect(metadata.canonical).toBe('https://aixlau.me/login')
    expect(metadata.schema).toBeUndefined()
  })

  it('returns a distinct canonical and title for each public business page', () => {
    const metadata = getPageMetadata('/model-market')

    expect(metadata.title).toBe('GPT、Claude、Gemini API 模型与价格 - 星链AI')
    expect(metadata.robots).toContain('index,follow')
    expect(metadata.canonical).toBe('https://aixlau.me/model-market')
    expect(metadata.schema).toBeUndefined()
  })

  it('keeps the home alias canonical and marks unknown routes as noindex', () => {
    expect(getPageMetadata('/home').canonical).toBe('https://aixlau.me/')
    expect(getPageMetadata('/missing').robots).toBe('noindex,nofollow,noarchive')
    expect(getPageMetadata('/missing').title).toBe('页面不存在 - 星链AI')
    expect(getPageMetadata('/getting-started').robots).toBe('noindex,nofollow,noarchive')
    expect(getPageMetadata('/pricing').robots).toBe('noindex,nofollow,noarchive')
  })

  it('updates document metadata without touching page content', () => {
    const content = document.createElement('main')
    content.textContent = 'existing page content'
    document.body.appendChild(content)

    applySeoMetadata(getPageMetadata('/'))

    expect(document.title).toBe('星链AI｜GPT API 中转站与统一模型 API 平台')
    expect(document.querySelector('meta[name="robots"]')).toHaveAttribute(
      'content',
      'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1',
    )
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute(
      'href',
      'https://aixlau.me/',
    )
    expect(document.querySelector('#site-schema')).toHaveAttribute('type', 'application/ld+json')
    expect(document.body).toHaveTextContent('existing page content')
  })
})
