import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { useDocumentVisibility, useIntervalFn } from '@vueuse/core'
import { getRPM } from '@/api/admin/accounts'

/** Poll only the current page. List responses never overwrite a newer RPM sample. */
export function useAccountRPM(accountIDs: Ref<number[]>, visible: Ref<boolean>) {
  const visibility = useDocumentVisibility()
  const values = ref<Record<string, number>>({})
  const loading = ref(false)
  const active = computed(() => visible.value && visibility.value === 'visible' && accountIDs.value.length > 0)
  let controller: AbortController | null = null

  async function refresh() {
    if (!active.value || controller) return
    const request = new AbortController()
    controller = request
    try {
      const result = await getRPM(accountIDs.value, { signal: request.signal })
      if (!request.signal.aborted) values.value = result.rpm
    } catch (error) {
      if (!request.signal.aborted) {
        values.value = {}
        console.error('Failed to load account RPM:', error)
      }
    } finally {
      if (controller === request) {
        controller = null
        loading.value = false
      }
    }
  }

  const { pause, resume } = useIntervalFn(refresh, 5000, { immediate: false })
  watch([() => accountIDs.value.join(','), active], () => {
    controller?.abort()
    controller = null
    pause()
    values.value = {}
    loading.value = active.value
    if (active.value) {
      void refresh()
      resume()
    }
  }, { immediate: true })
  onScopeDispose(() => {
    pause()
    controller?.abort()
  })
  return { values, loading, refresh }
}
