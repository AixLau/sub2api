<script setup lang="ts">
import { footerDisclaimer, navLinks, productName } from '../../data/homepage'
</script>

<template>
  <footer class="border-t border-pearl/10 bg-ink px-5 py-12 text-pearl sm:px-6">
    <div class="mx-auto grid max-w-7xl gap-10 md:grid-cols-[1.2fr_0.8fr]">
      <div>
        <a href="#" class="inline-flex items-center gap-3" aria-label="LumaAPI 首页">
          <span class="flex h-9 w-9 items-center justify-center rounded-full bg-pearl text-sm font-bold text-ink">
            L
          </span>
          <span class="text-lg font-semibold">{{ productName }}</span>
        </a>
        <p class="mt-5 max-w-lg text-sm leading-7 text-pearl/68">
          一层稳定、优雅的模型 API 服务，帮助产品团队、开发者与业务团队更轻地接入 GPT-like 模型能力。
        </p>
        <p class="mt-6 max-w-3xl text-xs leading-6 text-pearl/46">
          {{ footerDisclaimer }}
        </p>
      </div>

      <div class="flex flex-wrap gap-3 md:justify-end">
        <a
          v-for="link in navLinks"
          :key="link.href"
          :href="link.href"
          class="rounded-full border border-pearl/14 px-4 py-2 text-sm text-pearl/68 transition hover:border-pearl/32 hover:text-pearl focus:outline-none focus:ring-2 focus:ring-pearl/24"
        >
          {{ link.label }}
        </a>
      </div>
    </div>
  </footer>
</template>
