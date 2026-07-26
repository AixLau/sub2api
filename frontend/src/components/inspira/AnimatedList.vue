<!--
  AnimatedList — 基于 Vue 原生 TransitionGroup 的入场动画列表包装组件。

  子项进入时淡入 + 上移，离开时淡出，位置变化平滑过渡(move class)；
  支持 stagger(按索引递增 transition-delay，上限 8 项)。
  prefers-reduced-motion 时无动画。

  用法(直接包裹 v-for 内容，子项必须带 key)：

    <AnimatedList tag="div" :stagger="60" class="space-y-3">
      <div v-for="item in items" :key="item.id">{{ item.name }}</div>
    </AnimatedList>
-->
<template>
  <TransitionGroup
    :tag="tag"
    name="animated-list"
    :css="!prefersReducedMotion"
    appear
    @enter="onEnter"
    @after-enter="clearDelay"
    @enter-cancelled="clearDelay"
  >
    <slot />
  </TransitionGroup>
</template>

<script setup lang="ts">
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'

interface Props {
  /** 渲染的根元素标签 */
  tag?: string
  /** 相邻子项进场延迟(ms)，最多叠加约 8 项 */
  stagger?: number
}

const props = withDefaults(defineProps<Props>(), {
  tag: 'div',
  stagger: 60
})

/** stagger 叠加的最大项数，之后统一用最大延迟，避免长列表尾部等待过久 */
const MAX_STAGGER_ITEMS = 8

const { prefersReducedMotion } = usePrefersReducedMotion()

function onEnter(el: Element) {
  if (prefersReducedMotion.value) return
  const parent = el.parentElement
  const index = parent ? Array.prototype.indexOf.call(parent.children, el) : 0
  const capped = Math.min(Math.max(index, 0), MAX_STAGGER_ITEMS - 1)
  ;(el as HTMLElement).style.transitionDelay = `${capped * props.stagger}ms`
}

function clearDelay(el: Element) {
  (el as HTMLElement).style.transitionDelay = ''
}
</script>

<style scoped>
/* 子项来自父组件插槽，需用 :slotted 才能命中 */
:slotted(.animated-list-enter-active) {
  transition:
    opacity 0.3s ease,
    transform 0.3s ease;
}

:slotted(.animated-list-enter-from) {
  opacity: 0;
  transform: translateY(12px);
}

:slotted(.animated-list-leave-active) {
  position: absolute;
  transition: opacity 0.2s ease;
}

:slotted(.animated-list-leave-to) {
  opacity: 0;
}

:slotted(.animated-list-move) {
  transition: transform 0.35s ease;
}

@media (prefers-reduced-motion: reduce) {
  :slotted(.animated-list-enter-active),
  :slotted(.animated-list-leave-active),
  :slotted(.animated-list-move) {
    transition: none;
  }
}
</style>
