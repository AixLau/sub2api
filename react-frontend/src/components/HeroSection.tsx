import type { CSSProperties } from 'react'
import { HERO_BACKGROUND_IMAGE, serviceTickerItems } from '../data/alwayzz'
import { HeroCurveLines } from './HeroCurveLines'
import { Marquee } from './Marquee'

export function HeroSection() {
  const heroStyle = {
    '--hero-bg-image': `url("${HERO_BACKGROUND_IMAGE}")`,
  } as CSSProperties

  return (
    <section className="hero-section" style={heroStyle} aria-labelledby="hero-title">
      <HeroCurveLines />

      <div className="hero-content">
        <Marquee
          className="hero-ticker"
          items={serviceTickerItems}
          renderItem={(item) => <span className="ticker-pill">{item}</span>}
        />

        <h1 className="hero-title hero-title--cn" id="hero-title">
          Codex 与模型 API 接入，像光一样自然。
        </h1>

        <p className="hero-subtitle">
          星链AI 提供稳定的 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini 等主流模型，减少复杂配置，让团队把注意力留给产品体验。
        </p>

        <div className="hero-actions" aria-label="首页操作">
          <a className="primary-button" href="/login">
            开始接入
          </a>
          <a className="booking-button service-button" href="/docs/install/" aria-label="Codex API 中转站配置教程">
            <span className="booking-copy">
              <span className="booking-title">Codex 接入教程</span>
              <span className="booking-meta">
                <span className="availability-dot" aria-hidden="true" />
                Codex 配置 / 使用指南
              </span>
            </span>
          </a>
        </div>
      </div>

      <div className="progressive-blur" aria-hidden="true" />
    </section>
  )
}
