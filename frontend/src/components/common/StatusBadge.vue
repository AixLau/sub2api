<template>
  <div class="flex items-center gap-1.5">
    <span
      :class="[
        'inline-block h-2 w-2 rounded-full',
        variantClass
      ]"
    ></span>
    <span class="text-sm text-content-secondary">
      {{ label }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  label: string
}>()

const variantClass = computed(() => {
  switch (props.status) {
    case 'active':
    case 'success':
      return 'bg-status-success'
    case 'disabled':
    case 'inactive':
    case 'warning':
      return 'bg-status-warning'
    case 'error':
    case 'danger':
      return 'bg-status-danger'
    default:
      return 'bg-content-disabled'
  }
})
</script>
