<template>
  <div class="search-glow relative w-full">
    <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
      <Icon name="search" size="md" class="text-gray-400" />
    </div>
    <input
      :value="modelValue"
      type="text"
      class="input pl-10"
      :placeholder="placeholder"
      @input="handleInput"
    />
  </div>
</template>

<script setup lang="ts">
import { useDebounceFn } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  debounceMs?: number
}>(), {
  placeholder: 'Search...',
  debounceMs: 300
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search', value: string): void
}>()

const debouncedEmitSearch = useDebounceFn((value: string) => {
  emit('search', value)
}, props.debounceMs)

const handleInput = (event: Event) => {
  const value = (event.target as HTMLInputElement).value
  emit('update:modelValue', value)
  debouncedEmitSearch(value)
}
</script>

<style scoped>
/*
 * Inspira-style focus glow: painted by a wrapper pseudo-element so it layers
 * on top of (never replaces) the input's own accessible focus ring.
 */
.search-glow::before {
  content: '';
  position: absolute;
  inset: -1px;
  /* input uses rounded-xl (0.75rem); +1px to hug the -1px inset */
  border-radius: 0.8125rem;
  pointer-events: none;
  opacity: 0;
  transition: opacity 250ms ease;
  /* primary-500 #14b8a6 — soft multi-layer diffusion, light mode kept faint */
  box-shadow:
    0 0 0 1px rgb(20 184 166 / 0.08),
    0 0 12px rgb(20 184 166 / 0.16),
    0 0 26px 2px rgb(20 184 166 / 0.1);
}

.dark .search-glow::before {
  /* slightly brighter in dark mode, still gentle */
  box-shadow:
    0 0 0 1px rgb(20 184 166 / 0.16),
    0 0 14px rgb(20 184 166 / 0.3),
    0 0 30px 2px rgb(20 184 166 / 0.16);
}

.search-glow:focus-within::before {
  opacity: 1;
  animation: search-glow-breathe 2.8s ease-in-out infinite;
}

/* very light breathing pulse while focused */
@keyframes search-glow-breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.72;
  }
}

@media (prefers-reduced-motion: reduce) {
  .search-glow::before {
    transition: none;
  }

  .search-glow:focus-within::before {
    /* keep the static glow, drop all motion */
    animation: none;
    opacity: 1;
  }
}
</style>
