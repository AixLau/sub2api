<template>
  <Teleport to="body">
    <!-- appear:支持父组件用 v-if 挂载并同时 show=true 的用法,首帧也播放进场动画 -->
    <Transition name="modal" appear>
      <div
        v-if="show"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" :class="['modal-content', widthClasses]" tabindex="-1" @click.stop>
          <!-- Header -->
          <div class="modal-header">
            <h3 :id="dialogId" class="modal-title">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              @click="emit('close')"
              class="-mr-2 rounded-xl p-2 text-content-disabled transition-colors hover:bg-surface-subtle hover:text-content-secondary focus:outline-none focus-visible:ring-2 focus-visible:ring-line-focus/30 focus-visible:ring-offset-2 focus-visible:ring-offset-surface-raised"
              aria-label="Close modal"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" class="modal-body">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import Icon from '@/components/icons/Icon.vue'

// 生成唯一ID以避免多个对话框时ID冲突
let dialogIdCounter = 0
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
  /** 打开时自动聚焦第一个可聚焦元素;含自定义聚焦逻辑的内容(如 OTP 输入格)可关闭 */
  autoFocus?: boolean
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: true,
  showCloseButton: true,
  zIndex: 50,
  autoFocus: true
})

const emit = defineEmits<Emits>()

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const focusableSelector =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

const getFocusableElements = () => {
  if (!dialogRef.value) return []
  return Array.from(dialogRef.value.querySelectorAll<HTMLElement>(focusableSelector)).filter(element => {
    const style = window.getComputedStyle(element)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
}

const handleKeydown = (event: KeyboardEvent) => {
  if (!props.show) return

  if (props.closeOnEscape && event.key === 'Escape') {
    emit('close')
    return
  }

  if (event.key !== 'Tab' || !dialogRef.value) return

  const focusable = getFocusableElements()
  if (focusable.length === 0) {
    event.preventDefault()
    dialogRef.value.focus()
    return
  }

  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement

  if (event.shiftKey && (active === first || !dialogRef.value.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  async (isOpen) => {
    if (isOpen) {
      // 保存当前焦点元素
      previousActiveElement = document.activeElement as HTMLElement
      // 使用CSS类而不是直接操作style,更易于管理多个对话框
      document.body.classList.add('modal-open')

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
		if (modalBodyRef.value) {
			modalBodyRef.value.scrollTop = 0
		}
		if (dialogRef.value && props.autoFocus) {
			const firstFocusable = getFocusableElements()[0]
			;(firstFocusable ?? dialogRef.value).focus()
      }
    } else {
      document.body.classList.remove('modal-open')
      // 恢复之前的焦点
      if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
        previousActiveElement.focus()
      }
      previousActiveElement = null
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
  // 确保组件卸载时移除滚动锁定
  document.body.classList.remove('modal-open')
})
</script>

<style scoped>
/*
 * Inspira「Animated Modal」风格进出场动画
 * 增强全局 .modal-* transition class(style.css),scoped 属性选择器优先级更高。
 * 进场:遮罩淡入 + backdrop-blur 0 → sm(~200ms);面板 scale 0.95→1 + translateY 8px→0 + 淡入(~220ms,轻微过冲)
 * 出场:遮罩淡出;面板 scale→0.97 + 快速淡出(~150ms)
 */
.modal-enter-active {
  transition:
    opacity 200ms ease-out,
    -webkit-backdrop-filter 200ms ease-out,
    backdrop-filter 200ms ease-out;
}

.modal-leave-active {
  transition:
    opacity 150ms ease-in,
    -webkit-backdrop-filter 150ms ease-in,
    backdrop-filter 150ms ease-in;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
  -webkit-backdrop-filter: blur(0);
  backdrop-filter: blur(0);
}

.modal-enter-active .modal-content {
  /* back-out 曲线带来轻微过冲 */
  transition:
    transform 220ms cubic-bezier(0.22, 1.25, 0.42, 1),
    opacity 220ms ease-out;
}

.modal-leave-active .modal-content {
  transition:
    transform 150ms ease-in,
    opacity 150ms ease-in;
}

.modal-enter-from .modal-content {
  transform: scale(0.95) translateY(8px);
  opacity: 0;
}

.modal-leave-to .modal-content {
  transform: scale(0.97) translateY(0);
  opacity: 0;
}

.modal-enter-to .modal-content,
.modal-leave-from .modal-content {
  transform: scale(1) translateY(0);
  opacity: 1;
}

@media (prefers-reduced-motion: reduce) {
  /* 减弱动效:去掉过渡,直接显隐 */
  .modal-enter-active,
  .modal-leave-active,
  .modal-enter-active .modal-content,
  .modal-leave-active .modal-content {
    transition: none;
  }

  .modal-enter-from .modal-content,
  .modal-leave-to .modal-content {
    transform: none;
  }
}
</style>
