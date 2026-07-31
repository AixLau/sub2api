<template>
  <div :class="rootClass">
    <div :class="resolvedIconClass">
      <slot name="icon">
        <component v-if="icon" :is="icon" :class="compact ? 'h-5 w-5' : 'h-6 w-6'" aria-hidden="true" />
      </slot>
    </div>
    <div class="min-w-0 flex-1">
      <p :class="compact ? 'text-xs font-medium text-content-tertiary' : 'stat-label truncate'">
        {{ title }}
      </p>
      <div class="mt-1 flex items-baseline gap-2">
        <p :class="resolvedValueClass" :title="String(formattedValue)">
          <NumberTicker
            v-if="typeof value === 'number'"
            :value="value"
            :prefix="prefix"
            :suffix="suffix"
            :format-fn="formatValue"
          />
          <template v-else>{{ formattedValue }}</template>
        </p>
        <span v-if="change !== undefined" :class="['stat-trend', trendClass]">
          <Icon
            v-if="changeType !== 'neutral'"
            name="arrowUp"
            size="xs"
            :class="changeType === 'down' && 'rotate-180'"
          />
          {{ formattedChange }}
        </span>
      </div>
      <slot name="footer" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Component } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import NumberTicker from '@/components/inspira/NumberTicker.vue'

type ChangeType = 'up' | 'down' | 'neutral'
type IconVariant = 'primary' | 'success' | 'warning' | 'danger'

interface Props {
  title: string
  value: number | string
  icon?: Component
  iconVariant?: IconVariant
  change?: number
  changeType?: ChangeType
  formatValue?: (value: number) => string
  prefix?: string
  suffix?: string
  compact?: boolean
  cardClass?: string
  iconClass?: string
  valueClass?: string
}

const props = withDefaults(defineProps<Props>(), {
  changeType: 'neutral',
  iconVariant: 'primary',
  prefix: '',
  suffix: '',
  compact: false,
  cardClass: '',
  iconClass: '',
  valueClass: ''
})

const formattedValue = computed(() => {
  if (props.formatValue && typeof props.value === 'number') {
    return props.formatValue(props.value)
  }
  if (typeof props.value === 'number') {
    return props.value.toLocaleString()
  }
  return props.value
})

const formattedChange = computed(() => {
  if (props.change === undefined) return ''
  const absChange = Math.abs(props.change)
  return `${absChange}%`
})

const variantClass = computed(() => {
  const classes: Record<IconVariant, string> = {
    primary: 'stat-icon-primary',
    success: 'stat-icon-success',
    warning: 'stat-icon-warning',
    danger: 'stat-icon-danger'
  }
  return classes[props.iconVariant]
})

const rootClass = computed(() => {
  if (props.cardClass) return props.cardClass
  return props.compact ? 'card flex items-center gap-3 p-4' : 'stat-card'
})

const resolvedIconClass = computed(() => {
  if (props.iconClass) return props.iconClass
  return props.compact
    ? `rounded-lg p-2 ${variantClass.value}`
    : `stat-icon ${variantClass.value}`
})

const resolvedValueClass = computed(() => {
  if (props.valueClass) return props.valueClass
  return props.compact
    ? 'text-xl font-bold text-content-primary'
    : 'stat-value'
})

const trendClass = computed(() => {
  const classes: Record<ChangeType, string> = {
    up: 'stat-trend-up',
    down: 'stat-trend-down',
    neutral: 'text-content-tertiary'
  }
  return classes[props.changeType]
})
</script>
