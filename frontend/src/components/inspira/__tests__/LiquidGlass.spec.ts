import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import LiquidGlass from '../LiquidGlass.vue'

const chromiumUserAgent = 'Mozilla/5.0 Chrome/126.0.0.0 Safari/537.36'
const safariUserAgent = 'Mozilla/5.0 Version/17.5 Safari/605.1.15'
const firefoxUserAgent = 'Mozilla/5.0 Firefox/128.0'

describe('LiquidGlass', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('uses the documented SVG displacement filter in Chromium', async () => {
    const observe = vi.fn()
    const disconnect = vi.fn()
    vi.stubGlobal('navigator', { userAgent: chromiumUserAgent })
    vi.stubGlobal('ResizeObserver', class ResizeObserverMock {
      observe() {
        observe()
      }

      disconnect() {
        disconnect()
      }
    })

    const wrapper = mount(LiquidGlass, {
      slots: { default: '<button>Contact support</button>' }
    })
    await nextTick()

    expect(wrapper.attributes('data-liquid-glass')).toBe('supported')
    expect(wrapper.classes()).toContain('liquid-glass--supported')
    expect(wrapper.find('svg').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('filter').attributes('id')).toMatch(/^liquid-glass-/)
    expect(wrapper.findAll('feDisplacementMap').map(node => node.attributes('scale'))).toEqual([
      '-180',
      '-170',
      '-160'
    ])
    expect(wrapper.get('button').text()).toBe('Contact support')
    expect(observe).toHaveBeenCalledOnce()

    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it.each([
    ['Safari', safariUserAgent],
    ['Firefox', firefoxUserAgent]
  ])('uses the opaque raised surface token without the SVG filter in %s', async (_browser, userAgent) => {
    const resizeObserver = vi.fn()
    vi.stubGlobal('navigator', { userAgent })
    vi.stubGlobal('ResizeObserver', resizeObserver)

    const wrapper = mount(LiquidGlass, {
      slots: { default: '<span>Fallback content</span>' }
    })
    await nextTick()

    expect(wrapper.attributes('data-liquid-glass')).toBe('fallback')
    expect(wrapper.classes()).toContain('liquid-glass--fallback')
    expect(wrapper.attributes('style')).toContain('background-color: rgb(var(--color-surface-raised))')
    expect(wrapper.attributes('style')).not.toContain('url(')
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.text()).toContain('Fallback content')
    expect(resizeObserver).not.toHaveBeenCalled()
  })
})
