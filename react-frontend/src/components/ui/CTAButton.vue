<script setup lang="ts">
withDefaults(
  defineProps<{
    href: string
    variant?: 'primary' | 'secondary' | 'light'
  }>(),
  {
    variant: 'primary',
  },
)
</script>

<template>
  <a
    :href="href"
    class="group cta-button inline-flex min-h-12 items-center justify-center rounded-full px-6 py-3 text-sm font-semibold tracking-[0.01em] transition-colors duration-500 focus:outline-none focus:ring-2 sm:px-7"
    :class="{
      'bg-pearl text-ink shadow-[0_18px_46px_rgba(255,249,239,0.22)] hover:bg-champagne focus:ring-pearl/40':
        variant === 'primary',
      'border border-pearl/28 bg-pearl/8 text-pearl backdrop-blur-xl hover:border-pearl/48 hover:bg-pearl/15 focus:ring-pearl/30':
        variant === 'secondary',
      'bg-ink text-pearl shadow-soft hover:bg-charcoal focus:ring-ink/24':
        variant === 'light',
    }"
  >
    <slot />
    <span
      class="ml-2 h-1.5 w-1.5 rounded-full transition duration-500 group-hover:translate-x-1"
      :class="variant === 'secondary' ? 'bg-pearl/70' : 'bg-peach'"
      aria-hidden="true"
    />
  </a>
</template>
