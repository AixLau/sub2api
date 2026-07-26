<template>
  <section id="services" class="content-section services-section" aria-labelledby="services-title">
    <div class="content-inner">
      <div class="section-heading">
        <p>Service</p>
        <h2 id="services-title">复杂能力，保持简单。</h2>
        <span>覆盖模型使用、用量查看和账号管理等常用需求。</span>
      </div>

      <div class="capability-grid">
        <article v-for="capability in capabilities" :key="capability.index" class="capability-card">
          <span class="capability-index">{{ capability.index }}</span>
          <div>
            <h3>{{ capability.title }}</h3>
            <p>{{ capability.description }}</p>
          </div>
          <small>{{ capability.detail }}</small>
        </article>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { capabilities } from '@/data/landing'
</script>
