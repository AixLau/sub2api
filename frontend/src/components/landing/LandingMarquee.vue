<template>
  <div class="marquee" :class="wrapClass">
    <div class="marquee-track">
      <div v-for="(item, index) in repeatedItems" :key="index" class="marquee-item">
        <slot :item="item" :index="index" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts" generic="T">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    items: T[]
    repetitions?: number
    wrapClass?: string
  }>(),
  {
    repetitions: 4,
    wrapClass: ''
  }
)

defineSlots<{
  default(props: { item: T; index: number }): unknown
}>()

const repeatedItems = computed(() =>
  Array.from({ length: props.repetitions }).flatMap(() => props.items)
)
</script>
