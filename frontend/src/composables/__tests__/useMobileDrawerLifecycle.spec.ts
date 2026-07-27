import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useMobileDrawerLifecycle } from '../useMobileDrawerLifecycle'

const DrawerHarness = defineComponent({
  setup(_, { expose }) {
    const open = ref(false)
    const close = vi.fn(() => {
      open.value = false
    })
    useMobileDrawerLifecycle(open, close)
    expose({ open, close })
    return () => null
  }
})

describe('useMobileDrawerLifecycle', () => {
  afterEach(() => {
    document.body.style.overflow = ''
    document.documentElement.style.overflow = ''
  })

  it('locks scrolling while open and restores the prior inline styles', async () => {
    document.body.style.overflow = 'auto'
    document.documentElement.style.overflow = 'scroll'
    const wrapper = mount(DrawerHarness)

    wrapper.vm.open = true
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.documentElement.style.overflow).toBe('hidden')

    wrapper.vm.open = false
    await nextTick()
    expect(document.body.style.overflow).toBe('auto')
    expect(document.documentElement.style.overflow).toBe('scroll')

    wrapper.unmount()
  })

  it('closes an open drawer on Escape and cleans up on unmount', async () => {
    const wrapper = mount(DrawerHarness)
    wrapper.vm.open = true
    await nextTick()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()

    expect(wrapper.vm.close).toHaveBeenCalledOnce()
    expect(wrapper.vm.open).toBe(false)
    expect(document.body.style.overflow).toBe('')
    expect(document.documentElement.style.overflow).toBe('')

    wrapper.vm.open = true
    await nextTick()
    wrapper.unmount()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.vm.close).toHaveBeenCalledOnce()
  })
})
