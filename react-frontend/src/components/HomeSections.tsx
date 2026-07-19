import { ArrowUpRight, Check, CircleHelp, Gauge, KeyRound, Route } from 'lucide-react'
import {
  capabilities,
  faqItems,
  integrationSteps,
  pricingOptions,
} from '../data/alwayzz'

function SectionHeading({
  id,
  eyebrow,
  title,
  description,
}: {
  id: string
  eyebrow: string
  title: string
  description: string
}) {
  return (
    <div className="section-heading">
      <p>{eyebrow}</p>
      <h2 id={id}>{title}</h2>
      <span>{description}</span>
    </div>
  )
}

export function ServicesSection() {
  return (
    <section className="content-section services-section" id="services" aria-labelledby="services-title">
      <div className="content-inner">
        <SectionHeading
          id="services-title"
          eyebrow="Service"
          title="复杂能力，保持简单。"
          description="覆盖模型使用、用量查看和账号管理等常用需求。"
        />

        <div className="capability-grid">
          {capabilities.map((capability) => (
            <article className="capability-card" key={capability.index}>
              <span className="capability-index">{capability.index}</span>
              <div>
                <h3>{capability.title}</h3>
                <p>{capability.description}</p>
              </div>
              <small>{capability.detail}</small>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

export function GettingStartedSection() {
  return (
    <section className="content-section process-section" id="getting-started" aria-labelledby="process-title">
      <div className="content-inner process-layout">
        <SectionHeading
          id="process-title"
          eyebrow="Getting started"
          title="三步开始调用。"
          description="Codex 用户可直接使用已有的一键配置教程，其他应用也可以通过兼容 API 接入。"
        />

        <div className="process-list">
          {integrationSteps.map((item) => (
            <article className="process-item" key={item.step}>
              <span>{item.step}</span>
              <div>
                <h3>{item.title}</h3>
                <p>{item.description}</p>
              </div>
            </article>
          ))}

          <div className="process-actions">
            <a className="inline-action inline-action--dark" href="/register">
              创建账号
              <ArrowUpRight aria-hidden="true" size={16} />
            </a>
            <a className="inline-action" href="/docs/install/">
              Codex 接入文档
              <ArrowUpRight aria-hidden="true" size={16} />
            </a>
          </div>
        </div>
      </div>
    </section>
  )
}

export function PricingSection() {
  return (
    <section className="content-section pricing-section" id="pricing" aria-labelledby="pricing-title">
      <div className="content-inner">
        <SectionHeading
          id="pricing-title"
          eyebrow="Pricing"
          title="按使用方式选择。"
          description="价格、额度与当前可售套餐以登录后的控制台为准，避免公开页面展示过期信息。"
        />

        <div className="pricing-grid">
          {pricingOptions.map((option) => (
            <article className="pricing-card" key={option.title}>
              <span className="pricing-label">{option.label}</span>
              <h3>{option.title}</h3>
              <p>{option.description}</p>
              <ul>
                {option.points.map((point) => (
                  <li key={point}>
                    <Check aria-hidden="true" size={15} />
                    {point}
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </div>

        <div className="pricing-action-row">
          <p>注册后可查看当前套餐、可用模型与实际计费信息。</p>
          <a className="inline-action inline-action--dark" href="/register">
            注册后查看套餐
            <ArrowUpRight aria-hidden="true" size={16} />
          </a>
        </div>
      </div>
    </section>
  )
}

export function StatusSection() {
  const statusItems = [
    { icon: Route, label: '服务状态', detail: '查看当前服务可用情况' },
    { icon: Gauge, label: '调用记录', detail: '查询延迟、Token、费用与错误' },
    { icon: KeyRound, label: '额度管理', detail: '查看余额、订阅与 API Key 用量' },
  ]

  return (
    <section className="content-section status-section" id="status" aria-labelledby="status-title">
      <div className="content-inner status-panel">
        <div className="status-copy">
          <p className="status-kicker">
            <span aria-hidden="true" />
            Service status
          </p>
          <h2 id="status-title">状态与用量，都有迹可循。</h2>
          <p>登录后可查看服务状态、请求明细和费用信息。</p>
          <a className="inline-action inline-action--light" href="/monitor">
            登录后查看服务状态
            <ArrowUpRight aria-hidden="true" size={16} />
          </a>
        </div>

        <div className="status-list">
          {statusItems.map(({ icon: Icon, label, detail }) => (
            <div className="status-item" key={label}>
              <Icon aria-hidden="true" size={20} strokeWidth={1.8} />
              <div>
                <strong>{label}</strong>
                <span>{detail}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

export function FaqSection() {
  return (
    <section className="content-section faq-section" id="faq" aria-labelledby="faq-title">
      <div className="content-inner faq-layout">
        <div>
          <SectionHeading
            id="faq-title"
            eyebrow="FAQ"
            title="接入前，先了解这些。"
            description="这里说明产品能力和服务边界；具体模型、套餐与价格以控制台为准。"
          />
          <CircleHelp className="faq-mark" aria-hidden="true" size={32} strokeWidth={1.4} />
        </div>

        <div className="faq-list">
          {faqItems.map((item) => (
            <details className="faq-item" key={item.question}>
              <summary>{item.question}</summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  )
}

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="footer-inner">
        <div className="footer-brand">
          <a className="logo" href="/" aria-label="星链AI home">
            星链AI
          </a>
          <p>统一模型 API 服务，让开发工具与应用更简单地接入主流模型能力。</p>
        </div>

        <nav className="footer-links" aria-label="页脚导航">
          <a href="/model-market">模型广场</a>
          <a href="/services">服务能力</a>
          <a href="/service-status">服务状态</a>
          <a href="/faq">FAQ</a>
          <a href="/docs/install/">Codex 接入文档</a>
          <a href="/login">登录</a>
          <a href="/register">注册</a>
          <a href="/legal/terms">服务条款</a>
          <a href="/legal/usage-policy">使用政策</a>
        </nav>

        <p className="footer-meta">© 2026 星链AI · 模型、套餐与可用性以控制台为准</p>
      </div>
    </footer>
  )
}
