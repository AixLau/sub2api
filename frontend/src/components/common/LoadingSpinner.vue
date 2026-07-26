<template>
  <div
    v-if="variant === 'orbit'"
    :class="['orbit-spinner', orbitSizeClass, colorClass]"
    role="status"
    :aria-label="t('common.loading')"
  >
    <span
      v-for="ring in 3"
      :key="ring"
      :class="['orbit-ring', `orbit-ring-${ring}`]"
      aria-hidden="true"
    >
      <span class="orbit-dot"></span>
    </span>
    <span class="orbit-center" aria-hidden="true"></span>
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
  <div
    v-else
    :class="['spinner', sizeClasses, colorClass]"
    role="status"
    :aria-label="t('common.loading')"
  >
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

type SpinnerSize = 'sm' | 'md' | 'lg' | 'xl'
type SpinnerColor = 'primary' | 'secondary' | 'white' | 'gray'
type SpinnerVariant = 'default' | 'orbit'

interface Props {
  size?: SpinnerSize
  color?: SpinnerColor
  variant?: SpinnerVariant
}

const props = withDefaults(defineProps<Props>(), {
  size: 'md',
  color: 'primary',
  variant: 'default'
})

const sizeClasses = computed(() => {
  const sizes: Record<SpinnerSize, string> = {
    sm: 'w-4 h-4 border-2',
    md: 'w-8 h-8 border-2',
    lg: 'w-12 h-12 border-[3px]',
    xl: 'w-16 h-16 border-4'
  }
  return sizes[props.size]
})

// orbit 变体只需要宽高(不需要 border-*,preflight 会让 border-width 直接显形)
const orbitSizeClass = computed(() => {
  const sizes: Record<SpinnerSize, string> = {
    sm: 'w-4 h-4',
    md: 'w-8 h-8',
    lg: 'w-12 h-12',
    xl: 'w-16 h-16'
  }
  return sizes[props.size]
})

const colorClass = computed(() => {
  const colors: Record<SpinnerColor, string> = {
    primary: 'text-primary-500',
    secondary: 'text-gray-500 dark:text-dark-400',
    white: 'text-white',
    gray: 'text-gray-400 dark:text-dark-500'
  }
  return colors[props.color]
})
</script>

<style scoped>
.spinner {
  @apply inline-block rounded-full border-solid border-current border-r-transparent;
  animation: spin 0.75s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

/* ===== orbit 变体(Inspira 轨道动画风格) ===== */
.orbit-spinner {
  @apply relative inline-block;
}

/* 每条轨道是一个铺满对应半径的正方形旋转层,小点固定在层的正上方 */
.orbit-ring {
  @apply absolute block rounded-full;
  animation: orbit-rotate linear infinite;
}

.orbit-ring-1 {
  inset: 0;
  animation-duration: 1.2s;
}

.orbit-ring-2 {
  inset: 16%;
  animation-duration: 1.8s;
  animation-direction: reverse;
}

.orbit-ring-3 {
  inset: 32%;
  animation-duration: 2.6s;
}

.orbit-dot {
  @apply absolute block rounded-full bg-current;
  top: 0;
  left: 50%;
  transform: translate(-50%, -50%);
}

/* 外圈点最大,越靠内越小、越淡,颜色随 colorClass 的 currentColor(含 dark: 适配) */
.orbit-ring-1 .orbit-dot {
  width: 17%;
  height: 17%;
}

.orbit-ring-2 .orbit-dot {
  width: 22%;
  height: 22%;
  opacity: 0.75;
}

.orbit-ring-3 .orbit-dot {
  width: 34%;
  height: 34%;
  opacity: 0.5;
}

.orbit-center {
  @apply absolute block rounded-full bg-current;
  top: 50%;
  left: 50%;
  width: 13%;
  height: 13%;
  transform: translate(-50%, -50%);
  opacity: 0.6;
}

@keyframes orbit-rotate {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

/* reduced-motion:停止公转,三点静态分布在不同角度 */
@media (prefers-reduced-motion: reduce) {
  .orbit-ring {
    animation: none;
  }

  .orbit-ring-1 {
    transform: rotate(0deg);
  }

  .orbit-ring-2 {
    transform: rotate(135deg);
  }

  .orbit-ring-3 {
    transform: rotate(250deg);
  }
}
</style>
