<template>
  <section class="trusted-section" aria-label="服务能力">
    <div class="trusted-inner">
      <LandingMarquee wrap-class="trusted-marquee" :items="trustedCompanies">
        <template #default="{ item }">
          <span :class="item.className">{{ item.name }}</span>
        </template>
      </LandingMarquee>
    </div>
  </section>
</template>

<script setup lang="ts">
import { trustedCompanies } from '@/data/landing'
import LandingMarquee from './LandingMarquee.vue'
</script>
