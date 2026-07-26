<!--
  Timeline — 通用竖向时间线组件。

  左侧竖线 + 节点圆点，圆点颜色由 tone 决定
  (default / success / warning / danger)，节点带轻微淡入上移的进场动画
  (按索引 stagger，上限 8 项)，亮暗模式适配。
  prefers-reduced-motion 时无动画。

  用法：

    <Timeline
      :items="[
        { time: '2026-07-26 10:00', title: 'admin@x.com · user.create', description: 'POST /api/users', badge: '200', tone: 'success' }
      ]"
    />
-->
<template>
  <ol class="timeline-root" role="list">
    <li
      v-for="(item, index) in items"
      :key="index"
      class="timeline-item"
      :class="{ 'timeline-item-animated': !prefersReducedMotion }"
      :style="
        prefersReducedMotion
          ? undefined
          : { animationDelay: `${Math.min(index, MAX_STAGGER_ITEMS - 1) * stagger}ms` }
      "
    >
      <!-- 竖线（最后一项不再向下延伸） -->
      <span
        v-if="index < items.length - 1"
        class="timeline-line"
        aria-hidden="true"
      ></span>

      <!-- 节点圆点 -->
      <span class="timeline-dot" :class="toneDotClass(item.tone)" aria-hidden="true">
        <span class="timeline-dot-core" :class="toneCoreClass(item.tone)"></span>
      </span>

      <div class="timeline-body">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
          <time class="text-xs tabular-nums text-gray-400 dark:text-gray-500">
            {{ item.time }}
          </time>
          <span
            v-if="item.badge"
            class="timeline-badge"
            :class="toneBadgeClass(item.tone)"
          >
            {{ item.badge }}
          </span>
        </div>
        <div class="mt-1 break-all text-sm font-medium text-gray-900 dark:text-white">
          {{ item.title }}
        </div>
        <div
          v-if="item.description"
          class="mt-0.5 break-all font-mono text-xs text-gray-500 dark:text-gray-400"
        >
          {{ item.description }}
        </div>
      </div>
    </li>
  </ol>
</template>

<script setup lang="ts">
export interface TimelineItem {
  time: string
  title: string
  description?: string
  badge?: string
  tone?: 'default' | 'success' | 'warning' | 'danger'
}

interface Props {
  items: TimelineItem[]
  /** 相邻节点进场延迟(ms)，最多叠加约 8 项 */
  stagger?: number
}

withDefaults(defineProps<Props>(), {
  stagger: 50
})

/** stagger 叠加的最大项数，之后统一用最大延迟，避免长列表尾部等待过久 */
const MAX_STAGGER_ITEMS = 8

const prefersReducedMotion =
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? (window.matchMedia('(prefers-reduced-motion: reduce)')?.matches ?? false)
    : false

type Tone = TimelineItem['tone']

function toneDotClass(tone: Tone): string {
  switch (tone) {
    case 'success':
      return 'border-green-200 bg-green-50 dark:border-green-500/30 dark:bg-green-500/10'
    case 'warning':
      return 'border-amber-200 bg-amber-50 dark:border-amber-500/30 dark:bg-amber-500/10'
    case 'danger':
      return 'border-red-200 bg-red-50 dark:border-red-500/30 dark:bg-red-500/10'
    default:
      return 'border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'
  }
}

function toneCoreClass(tone: Tone): string {
  switch (tone) {
    case 'success':
      return 'bg-green-500'
    case 'warning':
      return 'bg-amber-500'
    case 'danger':
      return 'bg-red-500'
    default:
      return 'bg-gray-400 dark:bg-gray-500'
  }
}

function toneBadgeClass(tone: Tone): string {
  switch (tone) {
    case 'success':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'warning':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'danger':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
}
</script>

<style scoped>
.timeline-root {
  @apply relative;
}

.timeline-item {
  @apply relative flex gap-4 pb-6 pl-1 last:pb-0;
}

.timeline-line {
  position: absolute;
  top: 1.375rem;
  bottom: -0.125rem;
  left: 0.9375rem;
  width: 2px;
  border-radius: 9999px;
  @apply bg-gray-200 dark:bg-dark-700;
}

.timeline-dot {
  @apply relative z-10 mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full border;
}

.timeline-dot-core {
  @apply h-2 w-2 rounded-full;
}

.timeline-body {
  @apply min-w-0 flex-1;
}

.timeline-badge {
  @apply inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-semibold;
}

.timeline-item-animated {
  animation: timeline-item-in 0.35s ease-out both;
}

@keyframes timeline-item-in {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .timeline-item-animated {
    animation: none;
  }
}
</style>
