<template>
  <section id="faq" class="content-section faq-section" aria-labelledby="faq-title">
    <div class="content-inner faq-layout">
      <div>
        <div class="section-heading">
          <p>FAQ</p>
          <h2 id="faq-title">接入前，先了解这些。</h2>
          <span>这里说明产品能力和服务边界；具体模型、套餐与价格以控制台为准。</span>
        </div>
        <svg
          class="faq-mark"
          aria-hidden="true"
          width="32"
          height="32"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.4"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <circle cx="12" cy="12" r="10" />
          <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" />
          <path d="M12 17h.01" />
        </svg>
      </div>

      <div class="faq-list">
        <details v-for="item in faqItems" :key="item.question" class="faq-item">
          <summary>{{ item.question }}</summary>
          <p>{{ item.answer }}</p>
        </details>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { faqItems } from '@/data/landing'
</script>
