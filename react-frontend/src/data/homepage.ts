export interface NavLink {
  label: string
  href: string
}

export interface PillItem {
  label: string
}

export interface ValuePillItem {
  title: string
  description: string
}

export interface FeatureItem {
  eyebrow: string
  title: string
  description: string
}

export interface StepItem {
  title: string
  description: string
}

export interface PricingPlan {
  name: string
  label: string
  price: string
  description: string
  features: string[]
  featured?: boolean
}

export interface FAQItem {
  question: string
  answer: string
}

export const productName = 'LumaAPI'

export const navLinks: NavLink[] = [
  { label: '服务', href: '#services' },
  { label: '优势', href: '#advantages' },
  { label: '接入流程', href: '#process' },
  { label: '价格', href: '#pricing' },
  { label: 'FAQ', href: '#faq' },
]

export const heroValuePills: PillItem[] = [
  { label: 'GPT-like 接入' },
  { label: '稳定调用' },
  { label: '用量清晰' },
  { label: '灵活服务' },
]

export const valuePills: ValuePillItem[] = [
  {
    title: 'GPT-like 接入',
    description: '面向对话、文本与产品体验场景，保留清晰的模型能力入口。',
  },
  {
    title: '稳定调用',
    description: '把分散配置收束到一层服务，降低日常接入和维护的打断感。',
  },
  {
    title: '清晰用量',
    description: '让调用、额度与消耗更容易被产品和业务团队理解。',
  },
  {
    title: '灵活服务',
    description: '从原型验证到商业上线，按阶段调整服务方式与协作节奏。',
  },
]

export const features: FeatureItem[] = [
  {
    eyebrow: '统一入口',
    title: '统一模型接入',
    description: '用一套 API 服务接入 GPT 等模型能力，让产品团队少处理重复配置，把模型能力当作稳定服务来使用。',
  },
  {
    eyebrow: '调用体验',
    title: '调用更稳定',
    description: '面向真实产品调用场景整理请求路径、错误体验和服务协作，让上线后的使用更从容。',
  },
  {
    eyebrow: '团队视图',
    title: '用量更清晰',
    description: '把额度、消耗和增长趋势表达得更清楚，便于团队判断成本、节奏与下一步扩展。',
  },
  {
    eyebrow: '上线阶段',
    title: '适合产品上线',
    description: '保持接入方式轻盈，减少跨模型、跨环境和跨团队协作时容易出现的维护成本。',
  },
]

export const steps: StepItem[] = [
  {
    title: '选择模型能力',
    description: '根据产品体验选择对话、文本或其他 GPT-like 能力，不在第一步堆叠复杂技术细节。',
  },
  {
    title: '接入统一服务',
    description: '通过 LumaAPI 的服务层连接模型能力，把接入流程保持清爽、可解释、可协作。',
  },
  {
    title: '查看用量并扩展',
    description: '持续查看调用和额度变化，按产品增长阶段调整方案、支持方式与服务范围。',
  },
]

export const pricingPlans: PricingPlan[] = [
  {
    name: 'Starter',
    label: '适合原型验证',
    price: '价格占位',
    description: '轻量接入模型能力，适合验证产品方向与早期体验。',
    features: ['基础调用额度占位', '基础用量视图占位', '轻量支持占位'],
  },
  {
    name: 'Growth',
    label: '适合上线产品',
    price: '价格占位',
    description: '面向持续增长的产品团队，保留更灵活的服务空间。',
    features: ['更高调用额度占位', '团队用量管理占位', '服务协作占位'],
    featured: true,
  },
  {
    name: 'Business',
    label: '适合业务协作',
    price: '联系确认',
    description: '为业务团队预留可替换的服务范围、计费方式与协作流程。',
    features: ['方案配置占位', '服务流程占位', '协作支持占位'],
  },
]

export const faqs: FAQItem[] = [
  {
    question: 'LumaAPI 是什么？',
    answer:
      'LumaAPI 是一层模型 API 服务，帮助开发者、产品团队和业务团队用更简单的方式接入 GPT-like 模型能力。',
  },
  {
    question: '这是官方模型服务吗？',
    answer:
      '当前页面不声明任何官方合作关系。正式上线前，请以真实的供应商关系、服务条款和合规信息为准。',
  },
  {
    question: '是否支持 GPT 等模型能力？',
    answer:
      '页面展示的是 GPT-like 模型能力的服务定位，具体支持范围需要在正式产品信息中替换为真实内容。',
  },
  {
    question: '可以用于商业产品吗？',
    answer:
      '定位上适合商业产品接入，但正式使用前应确认模型来源、计费规则、服务承诺与合规要求。',
  },
  {
    question: '如何计算费用？',
    answer:
      '这里仅使用占位价格。真实计费方式应根据调用量、模型能力、服务支持和合同条款正式配置。',
  },
  {
    question: '是否支持企业服务？',
    answer:
      '页面保留了 Business 方案占位，可在正式上线前替换为真实的企业服务范围、响应方式和支持承诺。',
  },
]

export const footerDisclaimer =
  '当前页面为产品官网原型，模型、价格、服务承诺与合规信息请在正式上线前替换为真实内容。'
