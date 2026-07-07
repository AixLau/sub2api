<script setup lang="ts">
defineProps<{
  label: string
  tone?: 'pearl' | 'warm' | 'sage' | 'rose'
}>()
</script>

<template>
  <span
    class="floating-value-pill inline-flex items-center rounded-full border px-4 py-2 text-[0.82rem] font-medium tracking-[0.01em] shadow-soft backdrop-blur-2xl transition-colors duration-500"
    :class="{
      'border-pearl/44 bg-pearl/18 text-pearl': !tone || tone === 'pearl',
      'border-peach/42 bg-peach/18 text-pearl': tone === 'warm',
      'border-sage/38 bg-sage/17 text-pearl': tone === 'sage',
      'border-rose/42 bg-rose/18 text-pearl': tone === 'rose',
    }"
  >
    {{ label }}
  </span>
</template>
