<script setup lang="ts">
import { navLinks, productName } from '../../data/homepage'
</script>

<template>
  <header class="fixed inset-x-0 top-0 z-50 px-4 pt-4 sm:px-6">
    <nav
      class="mx-auto flex max-w-7xl items-center justify-between gap-4 rounded-full border border-pearl/14 bg-ink/42 px-4 py-3 text-pearl shadow-[0_20px_70px_rgba(15,14,12,0.22)] backdrop-blur-2xl sm:px-5"
      aria-label="主导航"
    >
      <a href="#" class="flex items-center gap-3" aria-label="LumaAPI 首页">
        <span class="flex h-9 w-9 items-center justify-center rounded-full bg-pearl text-sm font-bold text-ink shadow-[0_0_32px_rgba(255,249,239,0.24)]">
          L
        </span>
        <span class="text-base font-semibold tracking-[0.01em]">{{ productName }}</span>
      </a>

      <div class="hidden items-center gap-1 md:flex">
        <a
          v-for="link in navLinks"
          :key="link.href"
          :href="link.href"
          class="rounded-full px-4 py-2 text-sm font-medium text-pearl/66 transition hover:bg-pearl/10 hover:text-pearl focus:outline-none focus:ring-2 focus:ring-pearl/22"
        >
          {{ link.label }}
        </a>
      </div>

      <a
        href="#pricing"
        class="rounded-full bg-pearl px-5 py-2.5 text-sm font-semibold text-ink shadow-[0_14px_40px_rgba(255,249,239,0.18)] transition hover:-translate-y-0.5 hover:bg-champagne focus:outline-none focus:ring-2 focus:ring-pearl/35"
      >
        开始接入
      </a>
    </nav>
  </header>
</template>
