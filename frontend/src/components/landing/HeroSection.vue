<template>
  <section class="hero-section" :style="heroStyle" aria-labelledby="hero-title">
    <HeroCurveLines />

    <div class="hero-content">
      <LandingMarquee wrap-class="hero-ticker" :items="serviceTickerItems">
        <template #default="{ item }">
          <span class="ticker-pill">{{ item }}</span>
        </template>
      </LandingMarquee>

      <h1 id="hero-title" class="hero-title hero-title--cn">
        Codex 与模型 API 接入，像光一样自然。
      </h1>

      <p class="hero-subtitle">
        星链AI 提供稳定的 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini
        等主流模型，减少复杂配置，让团队把注意力留给产品体验。
      </p>

      <div class="hero-actions" aria-label="首页操作">
        <router-link class="primary-button" to="/login">开始接入</router-link>
        <a class="booking-button service-button" href="/docs/install/" aria-label="Codex API 中转站配置教程">
          <span class="booking-copy">
            <span class="booking-title">Codex 接入教程</span>
            <span class="booking-meta">
              <span class="availability-dot" aria-hidden="true" />
              Codex 配置 / 使用指南
            </span>
          </span>
        </a>
      </div>
    </div>

    <div class="progressive-blur" aria-hidden="true" />
  </section>
</template>

<script setup lang="ts">
import { HERO_BACKGROUND_IMAGE, serviceTickerItems } from '@/data/landing'
import HeroCurveLines from './HeroCurveLines.vue'
import LandingMarquee from './LandingMarquee.vue'

const heroStyle = {
  '--hero-bg-image': `url("${HERO_BACKGROUND_IMAGE}")`
}
</script>
