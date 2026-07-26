/**
 * 星链AI 落地页静态数据与文案，移植自 react-frontend/src/data/alwayzz.ts。
 * 落地页整体为该部署的品牌营销内容，文案保持与线上 React 版一致（中文硬编码）。
 */

export const brandName = '星链AI'

export type NavLink = {
  label: string
  href: string
}

export type PublicPageLink = NavLink & {
  eyebrow: string
  description: string
}

export type Capability = {
  index: string
  title: string
  description: string
  detail: string
}

export type FaqItem = {
  question: string
  answer: string
}

export type AuthSideCopy = {
  eyebrow: string
  title: string
  subtitle: string
  features: string[]
}

export const publicPageLinks: PublicPageLink[] = [
  {
    label: '模型广场',
    href: '/model-market',
    eyebrow: 'Models',
    description: '查看系统当前可用模型、API 标识与实时价格。',
  },
  {
    label: '服务能力',
    href: '/services',
    eyebrow: 'Services',
    description: '了解统一接入、用量查看和 API Key 管理能力。',
  },
  {
    label: '服务状态',
    href: '/service-status',
    eyebrow: 'Status',
    description: '了解服务可用情况、调用记录和额度信息。',
  },
  {
    label: 'FAQ',
    href: '/faq',
    eyebrow: 'Help',
    description: '快速确认适用场景、费用口径和服务边界。',
  },
]

export const navLinks: NavLink[] = publicPageLinks.map(({ label, href }) => ({ label, href }))

export const serviceTickerItems = ['模型统一接入', '稳定调用', '用量清晰', '灵活服务']

// 首页合作品牌展示保持与线上一致。
export const trustedCompanies = [
  { name: 'Airbnb', className: 'trusted-logo trusted-logo--airbnb' },
  { name: 'Shopify', className: 'trusted-logo trusted-logo--shopify' },
  { name: 'Notion', className: 'trusted-logo trusted-logo--notion' },
  { name: 'Linear', className: 'trusted-logo trusted-logo--linear' },
  { name: 'Webflow', className: 'trusted-logo trusted-logo--webflow' },
  { name: 'Figma', className: 'trusted-logo trusted-logo--figma' },
  { name: 'Slack', className: 'trusted-logo trusted-logo--slack' },
  { name: 'Stripe', className: 'trusted-logo trusted-logo--stripe' },
  { name: 'Vercel', className: 'trusted-logo trusted-logo--vercel' },
  { name: 'Framer', className: 'trusted-logo trusted-logo--framer' },
]

export const capabilities: Capability[] = [
  {
    index: '01',
    title: '统一模型入口',
    description: '使用一套 API 服务接入主流模型能力，减少多平台配置和协议切换。',
    detail: 'OpenAI / Anthropic / Gemini 兼容',
  },
  {
    index: '02',
    title: '用量与费用清晰',
    description: '按请求查看 Token、模型、耗时和费用，随时掌握 API Key 的使用情况。',
    detail: '调用记录 / 额度 / 订阅进度',
  },
  {
    index: '03',
    title: '稳定服务',
    description: '面向持续开发场景提供稳定可用的模型服务体验，降低日常维护成本。',
    detail: '多模型 / 稳定可用 / 持续服务',
  },
  {
    index: '04',
    title: '自助管理',
    description: '在线创建 API Key、购买套餐、查看订单，并根据需要限制额度和有效期。',
    detail: 'API Key / 套餐 / 支付',
  },
]

export const faqItems: FaqItem[] = [
  {
    question: 'GPT API 中转站可以用于哪些场景？',
    answer:
      '星链AI 可作为 GPT API 中转站，用于 Codex、Claude Code 等 AI 开发工具，也可以通过 OpenAI、Anthropic 或 Gemini 兼容接口接入程序、脚本与内部应用。具体可用模型以登录后的控制台为准。',
  },
  {
    question: 'Codex 如何接入？',
    answer: '注册并登录后，可按照现有 Codex 接入教程完成配置，无需手工整理复杂参数。',
  },
  {
    question: '费用如何计算？',
    answer:
      '平台支持按量计费和订阅套餐。实际费用与模型、Token 用量和所选服务有关，请以控制台展示的信息为准。',
  },
  {
    question: '可以查看调用明细吗？',
    answer:
      '可以。控制台提供调用记录、Token 用量、费用、模型、耗时和错误信息，也可以按 API Key 与时间范围筛选。',
  },
  {
    question: '可用模型会一直保持不变吗？',
    answer: '可用模型可能随服务情况调整，请以模型广场和控制台中的最新信息为准。',
  },
]

export const authSideCopy: Record<string, AuthSideCopy> = {
  login: {
    eyebrow: '模型 API 工作台',
    title: '回到星链服务中枢',
    subtitle: '继续管理 API Key、模型接入与调用用量，让团队在一个入口完成配置与查看。',
    features: ['API Key 管理', '模型统一接入', '调用用量视图'],
  },
  register: {
    eyebrow: '统一身份入口',
    title: '接入从账号开始',
    subtitle: '创建账号后即可进入服务配置、调用管理与用量查看流程。',
    features: ['注册身份', '配置服务', '创建 API Key'],
  },
  'reset-password': {
    eyebrow: '账号恢复入口',
    title: '找回访问权限',
    subtitle: '通过邮箱确认身份，重新回到模型服务管理流程。',
    features: ['邮箱验证', '重置密码', '返回工作台'],
  },
}

export const HERO_BACKGROUND_IMAGE =
  'https://images.higgs.ai/?default=1&output=webp&url=https%3A%2F%2Fd8j0ntlcm91z4.cloudfront.net%2Fuser_38xzZboKViGWJOttwIXH07lWA1P%2Fhf_20260626_041422_4a459e05-abce-4150-9fb7-4ededc423cd1.png&w=1280&q=85'
