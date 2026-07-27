import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'

export function useMobileDrawerLifecycle(
  isOpen: Readonly<Ref<boolean>>,
  close: () => void
) {
  let scrollLocked = false
  let previousBodyOverflow = ''
  let previousHtmlOverflow = ''

  function lockScroll() {
    if (scrollLocked) return

    previousBodyOverflow = document.body.style.overflow
    previousHtmlOverflow = document.documentElement.style.overflow
    document.body.style.overflow = 'hidden'
    document.documentElement.style.overflow = 'hidden'
    scrollLocked = true
  }

  function unlockScroll() {
    if (!scrollLocked) return

    document.body.style.overflow = previousBodyOverflow
    document.documentElement.style.overflow = previousHtmlOverflow
    scrollLocked = false
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && isOpen.value) {
      close()
    }
  }

  watch(
    isOpen,
    (open) => {
      if (open) {
        lockScroll()
      } else {
        unlockScroll()
      }
    },
    { immediate: true }
  )

  onMounted(() => {
    document.addEventListener('keydown', handleKeydown)
  })

  onBeforeUnmount(() => {
    document.removeEventListener('keydown', handleKeydown)
    unlockScroll()
  })
}
