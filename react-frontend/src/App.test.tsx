import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App, { publicPageRoutes } from './App'
import { HERO_BACKGROUND_IMAGE } from './data/alwayzz'

const originalLocation = window.location
const disabledLoginAgreementResponse = {
  code: 0,
  data: {
    login_agreement_enabled: false,
    login_agreement_documents: [],
  },
}

function publicSettingsResponse(): Response {
  return new Response(JSON.stringify(disabledLoginAgreementResponse), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function mockFetchWithResponse(responseFactory: () => Response) {
  return vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
    if (input === '/api/v1/settings/public') {
      return publicSettingsResponse()
    }

    return responseFactory()
  })
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: originalLocation,
  })
  window.history.pushState({}, '', '/')
})

describe('星链 landing page', () => {
  it('keeps the Git HEAD homepage while exposing business pages through the menu', () => {
    render(<App />)

    expect(screen.getAllByLabelText('星链 home')).toHaveLength(1)
    expect(screen.getAllByLabelText('星链 home')[0]).toHaveTextContent('星链')
    expect(screen.getByRole('button', { name: '菜单' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      '让模型 API 接入，像光一样自然。',
    )
    expect(screen.getByText(/稳定的 API 服务/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '开始接入' })).toHaveAttribute('href', '/login')
    expect(screen.getByRole('link', { name: '查看接入文档' })).toHaveAttribute(
      'href',
      '/docs/install/',
    )
    expect(screen.getAllByRole('link', { name: '登录' })).toHaveLength(1)
    screen.getAllByRole('link', { name: '登录' }).forEach((link) => {
      expect(link).toHaveAttribute('href', '/login')
    })
    expect(screen.getAllByRole('link', { name: '注册' })).toHaveLength(1)
    screen.getAllByRole('link', { name: '注册' }).forEach((link) => {
      expect(link).toHaveAttribute('href', '/register')
    })
    expect(screen.getByText('Codex 配置 / 使用指南')).toBeInTheDocument()
    expect(screen.queryByText(/GPT-like/i)).not.toBeInTheDocument()
    expect(screen.queryByText('商业可用')).not.toBeInTheDocument()
    expect(screen.getByText('Partnered with top-tier companies globally')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '产品页面' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '复杂能力，保持简单。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '模型与价格，接入前就看清。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '三步开始调用。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '按使用方式选择。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '状态与用量，都有迹可循。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '接入前，先了解这些。' })).not.toBeInTheDocument()
    expect(screen.queryByText('当前为官网原型')).not.toBeInTheDocument()
    expect(screen.getAllByText('Airbnb').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Stripe').length).toBeGreaterThan(0)
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '菜单' }))
    expect(document.querySelector('.drawer-links a[href="/model-market"]')).toBeInTheDocument()
    expect(document.querySelector('.drawer-links a[href="/services"]')).toBeInTheDocument()
    expect(document.querySelector('.drawer-links a[href="/getting-started"]')).not.toBeInTheDocument()
    expect(document.querySelector('.drawer-links a[href="/pricing"]')).not.toBeInTheDocument()
    expect(document.querySelector('.drawer-links a[href="/service-status"]')).toBeInTheDocument()
    expect(document.querySelector('.drawer-links a[href="/faq"]')).toBeInTheDocument()
  })

  it.each([
    ['/services', '复杂能力，保持简单。', '/services'],
    ['/service-status', '状态与用量，都有迹可循。', '/service-status'],
    ['/faq', '接入前，先了解这些。', '/faq'],
  ])('renders %s as an isolated public page', (path, heading, activeHref) => {
    window.history.pushState({}, '', path)
    render(<App />)

    expect(screen.getByRole('main')).toHaveClass('public-page-main')
    expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '让模型 API 接入，像光一样自然。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '需要的信息，各有入口。' })).not.toBeInTheDocument()
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '菜单' }))
    expect(document.querySelector(`.drawer-links a[href="${activeHref}"]`)).toHaveAttribute(
      'aria-current',
      'page',
    )
  })

  it.each(['/getting-started', '/pricing'])('removes the retired public route %s', (path) => {
    window.history.pushState({}, '', path)
    render(<App />)

    expect(screen.getByRole('heading', { name: '这个页面不存在。' })).toBeInTheDocument()
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument()
  })

  it('renders an explicit 404 instead of falling back to the homepage', () => {
    window.history.pushState({}, '', '/not-a-public-page')
    render(<App />)

    expect(screen.getByRole('heading', { name: '这个页面不存在。' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '让模型 API 接入，像光一样自然。' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回首页' })).toHaveAttribute('href', '/')
  })

  it('keeps account forms on dedicated pages instead of the homepage', () => {
    render(<App />)

    expect(screen.queryByRole('heading', { name: '登录星链' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '创建账号' })).not.toBeInTheDocument()
  })

  it('keeps every public homepage link on a real anchor or an owned application route', () => {
    const { container } = render(<App />)
    const vueRoutes = readFileSync(join(process.cwd(), '../frontend/src/router/index.ts'), 'utf8')
    const reactRoutes = readFileSync(join(process.cwd(), 'src/App.tsx'), 'utf8')
    const links = Array.from(container.querySelectorAll<HTMLAnchorElement>('a[href]'))

    for (const link of links) {
      const href = link.getAttribute('href')
      expect(href, `链接“${link.textContent?.trim()}”缺少 href`).toBeTruthy()

      if (!href) continue

      if (href.startsWith('#')) {
        expect(document.querySelector(href), `锚点 ${href} 不存在`).not.toBeNull()
        continue
      }

      if (href === '/') {
        expect(window.location.pathname).toBe('/')
        continue
      }

      if (href.startsWith('https://')) {
        const url = new URL(href)
        expect(['developers.openai.com', 'platform.claude.com']).toContain(url.hostname)
        expect(link).toHaveAttribute('target', '_blank')
        expect(link).toHaveAttribute('rel', 'noreferrer')
        continue
      }

      if (href === '/docs/install/') {
        expect(existsSync(join(process.cwd(), '../docs/install/index.html'))).toBe(true)
        continue
      }

      if (href.startsWith('/legal/')) {
        expect(vueRoutes).toContain("path: '/legal/:documentId'")
        continue
      }

      if (href === '/monitor') {
        expect(vueRoutes).toContain("path: '/monitor'")
        continue
      }

      if (href === '/dashboard') {
        expect(vueRoutes).toContain("path: '/dashboard'")
        continue
      }

      expect(reactRoutes, `React 未声明公开路由 ${href}`).toContain(`'${href}'`)
    }
  })

  it('publishes every public business route through sitemap and the production Caddy matcher', () => {
    const sitemap = readFileSync(join(process.cwd(), 'public/sitemap.xml'), 'utf8')
    const runbook = readFileSync(
      join(process.cwd(), '../deploy/react-landing-production.md'),
      'utf8',
    )
    const caddyMatcher = runbook.match(/@react_landing \{(?<body>[\s\S]*?)\n\t\}/)?.groups?.body ?? ''

    for (const route of publicPageRoutes) {
      expect(sitemap).toContain(`<loc>https://aixlau.me${route}</loc>`)
      expect(caddyMatcher).toContain(route)
    }
    expect(sitemap).not.toContain('/getting-started')
    expect(sitemap).not.toContain('/pricing')
    expect(caddyMatcher).not.toContain('/getting-started')
    expect(caddyMatcher).not.toContain('/pricing')
  })

  it('renders the system model catalog and converts per-token prices to per-million prices', async () => {
    window.history.pushState({}, '', '/model-market')
    const fetchMock = vi.spyOn(window, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            models: [
              {
                name: 'gpt-5.5',
                platform: 'openai',
                pricing: {
                  billing_mode: 'token',
                  input_price: 0.000005,
                  output_price: 0.00003,
                  cache_write_price: 0,
                  cache_read_price: 0.0000005,
                  image_input_price: null,
                  image_output_price: null,
                  per_request_price: null,
                  intervals: [],
                },
              },
              {
                name: 'claude-sonnet-5',
                platform: 'anthropic',
                pricing: {
                  billing_mode: 'token',
                  input_price: 0.000003,
                  output_price: 0.000015,
                  cache_write_price: 0.00000375,
                  cache_read_price: 0.0000003,
                  image_input_price: null,
                  image_output_price: null,
                  per_request_price: null,
                  intervals: [],
                },
              },
              {
                name: 'gemini-3.1-pro',
                platform: 'gemini',
                pricing: {
                  billing_mode: 'token',
                  input_price: 0.000002,
                  output_price: 0.000012,
                  cache_write_price: null,
                  cache_read_price: 0.0000002,
                  image_input_price: null,
                  image_output_price: null,
                  per_request_price: null,
                  intervals: [],
                },
              },
            ],
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    expect(screen.getByRole('main')).toHaveClass('public-page-main')
    expect(screen.queryByRole('heading', { name: '让模型 API 接入，像光一样自然。' })).not.toBeInTheDocument()
    const gpt55Card = (await screen.findByText('gpt-5.5')).closest('article')
    expect(gpt55Card).toBeInTheDocument()
    expect(gpt55Card).toHaveTextContent(/缓存创建 \/ 1M tokens\s*免费/)
    expect(gpt55Card).toHaveTextContent(/输入 \/ 1M tokens\s*\$5/)
    expect(gpt55Card).toHaveTextContent(/输出 \/ 1M tokens\s*\$30/)
    expect(gpt55Card).toHaveTextContent(/缓存读取 \/ 1M tokens\s*\$0.5/)
    expect(screen.getByText('claude-sonnet-5')).toBeInTheDocument()
    expect(screen.getByText('gemini-3.1-pro')).toBeInTheDocument()
    expect(screen.getByText('3 个模型')).toBeInTheDocument()
    expect(screen.queryByText('支持调用')).not.toBeInTheDocument()
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/models/public', {
      signal: expect.any(AbortSignal),
      headers: { Accept: 'application/json' },
    })
    expect(screen.queryByText(/页面展示基础起价/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText('官方模型参考')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Claude' }))
    expect(screen.queryByText('gpt-5.5')).not.toBeInTheDocument()
    expect(screen.getByText('claude-sonnet-5')).toBeInTheDocument()
    expect(screen.getByText('1 个模型')).toBeInTheDocument()
  })

  it('keeps search empty state usable for the system model catalog', async () => {
    window.history.pushState({}, '', '/model-market')
    vi.spyOn(window, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            models: [{ name: 'gpt-5.5', platform: 'openai', pricing: null }],
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    render(<App />)

    expect(await screen.findByText('gpt-5.5')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('搜索模型名称'), {
      target: { value: 'not-a-model' },
    })
    expect(screen.getByText('没有找到匹配模型')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清除筛选' }))
    expect(screen.getByText('gpt-5.5')).toBeInTheDocument()
  })

  it('offers a retry when the system model catalog request fails', async () => {
    window.history.pushState({}, '', '/model-market')
    const fetchMock = vi
      .spyOn(window, 'fetch')
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ code: 500, message: 'unavailable' }), {
          status: 503,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            code: 0,
            data: { models: [{ name: 'gpt-5.5', platform: 'openai', pricing: null }] },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )

    render(<App />)

    expect(await screen.findByText('模型目录暂时无法加载')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }))

    expect(await screen.findByText('gpt-5.5')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('renders a dedicated registration page with the two-column account layout', async () => {
    window.history.pushState({}, '', '/register')
    const { container } = render(<App />)

    expect(await screen.findByRole('link', { name: '返回首页' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: '星链 home' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('heading', { name: '创建账号' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '接入从账号开始' })).toBeInTheDocument()
    expect(screen.getByText('统一身份入口')).toBeInTheDocument()
    expect(screen.queryByText('星链账号服务')).not.toBeInTheDocument()
    expect(container.querySelector('.auth-visual-card')).not.toBeInTheDocument()
    expect(container.querySelector('.auth-visual-content')).toBeInTheDocument()
    expect(container.querySelector('.auth-glass-depth')).not.toBeInTheDocument()
    expect(container.querySelectorAll('.auth-glass-block')).toHaveLength(0)
    expect(screen.getByText('注册身份')).toBeInTheDocument()
    expect(screen.getByText('配置服务')).toBeInTheDocument()
    expect(screen.getByText('创建 API Key')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Google' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'GitHub' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('姓')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('名')).not.toBeInTheDocument()
    expect(screen.getByLabelText('邮箱')).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toBeInTheDocument()
    expect(screen.getByLabelText('邮箱验证码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送验证码' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '显示密码' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建账号' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '已有账号？登录' })).toHaveAttribute('href', '/login')
  })

  it('sends a registration email verification code through the Sub2API API', async () => {
    window.history.pushState({}, '', '/register')
    const fetchMock = mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            message: 'ok',
            countdown: 45,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/send-verify-code', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        email: 'demo@example.com',
      }),
    }))
    expect(await screen.findByText('验证码已发送，请查收邮箱。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '45s' })).toBeDisabled()
  })

  it('does not call Sub2API when sending a verification code with an invalid email', async () => {
    window.history.pushState({}, '', '/register')
    const fetchMock = mockFetchWithResponse(() =>
      new Response(JSON.stringify({ code: 0, data: { message: 'ok' } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'admin@qq' } })
    fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('请输入有效的邮箱地址。')
    expect(document.querySelector('.auth-form-message')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/auth/send-verify-code', expect.anything())
  })

  it('dismisses auth toast feedback automatically after a few seconds', async () => {
    window.history.pushState({}, '', '/register')

    render(<App />)

    const emailInput = await screen.findByLabelText('邮箱')
    vi.useFakeTimers()
    fireEvent.change(emailInput, { target: { value: 'admin@qq' } })
    fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))

    expect(screen.getByRole('alert')).toHaveTextContent('请输入有效的邮箱地址。')

    act(() => {
      vi.advanceTimersByTime(5000)
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a Chinese prompt when Sub2API rejects the verification-code email format', async () => {
    window.history.pushState({}, '', '/register')
    mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 400,
          message:
            "Invalid request: Key: 'SendVerifyCodeRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag",
        }),
        { status: 400, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'admin@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))

    expect(await screen.findByText('请输入有效的邮箱地址。')).toBeInTheDocument()
  })

  it('registers through Sub2API with the entered email verification code', async () => {
    window.history.pushState({}, '', '/register')
    const fetchMock = mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            access_token: 'access-token',
            user: {
              id: 1,
              username: 'demo',
              email: 'demo@example.com',
              role: 'user',
            },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const assignMock = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign: assignMock },
    })

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.change(screen.getByLabelText('邮箱验证码'), { target: { value: '123456' } })
    fireEvent.click(screen.getByRole('button', { name: '创建账号' }))

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/dashboard'))
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/register', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        email: 'demo@example.com',
        password: 'secret123',
        verify_code: '123456',
      }),
    })
  })

  it.each([
    ['/register?aff=%20AFF123%20', 'AFF123'],
    ['/register?aff_code=AFF456', 'AFF456'],
  ])('binds the affiliate invitation from %s during registration', async (path, affCode) => {
    window.history.pushState({}, '', path)
    const fetchMock = mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            access_token: 'access-token',
            user: {
              id: 1,
              username: 'demo',
              email: 'demo@example.com',
              role: 'user',
            },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const assignMock = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign: assignMock },
    })

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.change(screen.getByLabelText('邮箱验证码'), { target: { value: '123456' } })
    fireEvent.click(screen.getByRole('button', { name: '创建账号' }))

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/dashboard'))
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/register', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        email: 'demo@example.com',
        password: 'secret123',
        verify_code: '123456',
        aff_code: affCode,
      }),
    })
  })

  it('shows a helpful Chinese prompt when Sub2API requires email verification', async () => {
    window.history.pushState({}, '', '/register')
    mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 400,
          message: 'email verification is required',
        }),
        { status: 400, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.click(screen.getByRole('button', { name: '创建账号' }))

    expect(await screen.findByText('请先获取邮箱验证码并填写后再注册。')).toBeInTheDocument()
  })

  it('reuses the homepage curve-line background motion on auth pages', async () => {
    window.history.pushState({}, '', '/register')
    const { container } = render(<App />)

    const shell = await screen.findByRole('main')

    expect(shell).toHaveClass('auth-shell')
    expect(shell).toHaveAttribute('style', expect.stringContaining(HERO_BACKGROUND_IMAGE))
    expect(container.querySelector('.auth-dynamic-field')).not.toBeInTheDocument()
    expect(container.querySelectorAll('.auth-orb')).toHaveLength(0)
    expect(container.querySelector('.auth-motion-field')).not.toBeInTheDocument()
    expect(container.querySelectorAll('.auth-motion-line')).toHaveLength(0)
    expect(container.querySelectorAll('.curve-lines')).toHaveLength(3)
    expect(container.querySelectorAll('.curve-line')).toHaveLength(60)
  })

  it('renders login, reset password, and change password as dedicated pages', async () => {
    window.history.pushState({}, '', '/login')
    const { container, rerender } = render(<App />)

    expect(await screen.findByText('模型 API 工作台')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '回到星链服务中枢' })).toBeInTheDocument()
    expect(screen.getByText('继续管理 API Key、模型接入与调用用量，让团队在一个入口完成配置与查看。')).toBeInTheDocument()
    expect(screen.getByText('API Key 管理')).toBeInTheDocument()
    expect(screen.getByText('模型统一接入')).toBeInTheDocument()
    expect(screen.getByText('调用用量视图')).toBeInTheDocument()
    expect(container.querySelector('.auth-feature-list')).toBeInTheDocument()
    expect(container.querySelector('.auth-brand-icon')).not.toBeInTheDocument()
    expect(container.querySelector('.auth-brand-submark')).toHaveTextContent('API')
    expect(screen.getByRole('heading', { name: '登录星链' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '创建账号' })).toHaveAttribute('href', '/register')
    expect(screen.getByRole('link', { name: '找回密码' })).toHaveAttribute('href', '/reset-password')
    expect(screen.queryByRole('button', { name: 'Google' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'GitHub' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('姓')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('名')).not.toBeInTheDocument()

    window.history.pushState({}, '', '/reset-password')
    rerender(<App />)

    expect(await screen.findByRole('heading', { name: '找回密码' })).toBeInTheDocument()
    expect(screen.getByText('输入账号邮箱，我们会发送密码重置说明。')).toBeInTheDocument()
    expect(screen.queryByText('输入邮箱，我们会展示重置流程入口；当前页面仅为官网原型。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送重置说明' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: '返回登录' })).toHaveLength(1)

    window.history.pushState({}, '', '/change-password')
    rerender(<App />)

    expect(await screen.findByRole('heading', { name: '修改密码' })).toBeInTheDocument()
    expect(screen.getByLabelText('当前密码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '更新密码' })).toBeInTheDocument()
  })

  it('keeps the forgot-password URL in React for the dual-service deployment', async () => {
    window.history.pushState({}, '', '/forgot-password')
    render(<App />)

    expect(await screen.findByRole('heading', { name: '找回密码' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送重置说明' })).toBeInTheDocument()
  })

  it('builds React assets under a Sub2API-safe URL prefix', () => {
    const config = readFileSync(join(process.cwd(), 'vite.config.ts'), 'utf8')

    expect(config).toContain("assetsDir: 'landing-assets'")
  })

  it('keeps auth-only dependencies out of the first homepage bundle', () => {
    const appSource = readFileSync(join(process.cwd(), 'src/App.tsx'), 'utf8')
    const config = readFileSync(join(process.cwd(), 'vite.config.ts'), 'utf8')

    expect(appSource).toContain('lazy(')
    expect(appSource).toContain("import('./components/AuthPage')")
    expect(config).toContain('manualChunks')
    expect(config).toContain('auth-vendor')
  })

  it('loads external web fonts without blocking the initial render', () => {
    const html = readFileSync(join(process.cwd(), 'index.html'), 'utf8')
    const htmlWithoutNoscript = html.replace(/<noscript>[\s\S]*?<\/noscript>/g, '')

    expect(html).toContain('rel="preload"')
    expect(html).toContain('as="style"')
    expect(html).toContain('this.rel=\'stylesheet\'')
    expect(htmlWithoutNoscript).not.toMatch(
      /<link[\s\S]*fonts\.googleapis\.com\/css2\?family=Inter[\s\S]*rel="stylesheet"/,
    )
  })

  it('resets a password through the Sub2API API when the email token link is opened in React', async () => {
    window.history.pushState({}, '', '/reset-password?email=demo%40example.com&token=reset-token')
    const fetchMock = vi.spyOn(window, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 0,
          data: { message: 'ok' },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    expect(await screen.findByDisplayValue('demo@example.com')).toHaveAttribute('readonly')
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new-secret-123' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'new-secret-123' } })
    fireEvent.click(screen.getByRole('button', { name: '重置密码' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/reset-password', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        email: 'demo@example.com',
        token: 'reset-token',
        new_password: 'new-secret-123',
      }),
    }))
    expect(await screen.findByText('密码已更新，请返回登录。')).toBeInTheDocument()
  })

  it('logs in through the Sub2API API and stores tokens for the Vue console', async () => {
    window.history.pushState({}, '', '/login')
    const fetchMock = mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            access_token: 'access-token',
            refresh_token: 'refresh-token',
            expires_in: 3600,
            user: {
              id: 1,
              username: 'demo',
              email: 'demo@example.com',
              role: 'user',
            },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const assignMock = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign: assignMock },
    })

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/dashboard'))
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/login', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        email: 'demo@example.com',
        password: 'secret123',
      }),
    })
    expect(localStorage.getItem('auth_token')).toBe('access-token')
    expect(localStorage.getItem('refresh_token')).toBe('refresh-token')
    expect(localStorage.getItem('auth_user')).toContain('demo@example.com')
    expect(Number(localStorage.getItem('token_expires_at'))).toBeGreaterThan(Date.now())
  })

  it('requires the current backend login agreement before logging in through React', async () => {
    window.history.pushState({}, '', '/login')
    const fetchMock = vi.spyOn(window, 'fetch').mockImplementation(async (input) => {
      if (input === '/api/v1/settings/public') {
        return new Response(
          JSON.stringify({
            code: 0,
            data: {
              login_agreement_enabled: true,
              login_agreement_mode: 'checkbox',
              login_agreement_updated_at: '2026-03-31',
              login_agreement_revision: 'revision-2026-03-31',
              login_agreement_documents: [
                { id: 'terms', title: '服务条款', content_md: '' },
                { id: 'usage-policy', title: '使用政策', content_md: '' },
                { id: 'supported-regions', title: '支持的国家和地区', content_md: '' },
                { id: 'service-specific-terms', title: '服务特定条款', content_md: '' },
              ],
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }

      return new Response(
        JSON.stringify({
          code: 0,
          data: {
            access_token: 'access-token',
            user: {
              id: 1,
              username: 'demo',
              email: 'demo@example.com',
              role: 'user',
            },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    })
    const assignMock = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign: assignMock },
    })

    render(<App />)

    expect(await screen.findByText('我已阅读并同意')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '服务条款' })).toHaveAttribute('href', '/legal/terms')
    expect(screen.getByRole('link', { name: '使用政策' })).toHaveAttribute('href', '/legal/usage-policy')
    expect(screen.getByRole('link', { name: '支持的国家和地区' })).toHaveAttribute(
      'href',
      '/legal/supported-regions',
    )
    expect(screen.getByRole('link', { name: '服务特定条款' })).toHaveAttribute(
      'href',
      '/legal/service-specific-terms',
    )

    fireEvent.change(screen.getByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('请先阅读并同意最新条款。')).toBeInTheDocument()
    expect(assignMock).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/v1/auth/login', expect.anything())

    fireEvent.click(screen.getByLabelText('同意登录条款'))
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(assignMock).toHaveBeenCalledWith('/dashboard'))
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/login', expect.anything())
    expect(JSON.parse(localStorage.getItem('sub2api_login_agreement_consent') || '{}')).toMatchObject({
      revision: 'revision-2026-03-31',
    })
  })

  it('keeps users on the React login page when Sub2API requires 2FA', async () => {
    window.history.pushState({}, '', '/login')
    mockFetchWithResponse(() =>
      new Response(
        JSON.stringify({
          code: 0,
          data: {
            requires_2fa: true,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    render(<App />)

    fireEvent.change(await screen.findByLabelText('邮箱'), { target: { value: 'demo@example.com' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret123' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByText('当前账号开启了二次验证，请前往控制台登录页完成验证。')).toBeInTheDocument()
    expect(localStorage.getItem('auth_token')).toBeNull()
  })

  it('keeps the hero height responsive so compatibility information appears near the first viewport', () => {
    const css = readFileSync(join(process.cwd(), 'src/styles/alwayzz.css'), 'utf8')
    const heroBlock = css.match(/\.hero-section\s*{(?<body>[\s\S]*?)\n}/)?.groups?.body ?? ''

    expect(heroBlock).toContain('100svh')
    expect(heroBlock).toContain('clamp(')
    expect(heroBlock).not.toContain('min-height: 850px')
  })

  it('restores the fixed viewport homepage layout from Git HEAD', () => {
    render(<App />)

    expect(screen.getByRole('main')).toHaveClass('home-main')

    const css = readFileSync(join(process.cwd(), 'src/styles/alwayzz.css'), 'utf8')
    const homeShellBlock = css.match(/\.app-shell--home\s*{(?<body>[\s\S]*?)\n}/)?.groups?.body ?? ''
    const homeMainBlock = css.match(/\.home-main\s*{(?<body>[\s\S]*?)\n}/)?.groups?.body ?? ''
    const homeHeroBlock =
      css.match(/\.home-main \.hero-section\s*{(?<body>[\s\S]*?)\n}/)?.groups?.body ?? ''

    expect(homeShellBlock).toContain('height: 100svh')
    expect(homeShellBlock).toContain('overflow: hidden')
    expect(homeMainBlock).toContain('display: grid')
    expect(homeMainBlock).toContain('height: 100%')
    expect(homeMainBlock).toContain('overflow: hidden')
    expect(homeHeroBlock).toContain('min-height: 0')
    expect(homeHeroBlock).toContain('height: 100%')
  })

  it('keeps the auth card light and uses a clean text brand mark', () => {
    const css = readFileSync(join(process.cwd(), 'src/styles/alwayzz.css'), 'utf8')
    const authCardBlock = css.match(/\.auth-card\s*{(?<body>[\s\S]*?)\n}/)?.groups?.body ?? ''

    expect(authCardBlock).toContain('box-shadow:')
    expect(authCardBlock).toContain('inset')
    expect(authCardBlock).not.toContain('0 28px 80px')
    expect(authCardBlock).not.toContain('0 36px 90px')
    expect(css).toContain('.auth-brand-submark')
    expect(css).not.toContain('.auth-brand-icon')
  })
})
