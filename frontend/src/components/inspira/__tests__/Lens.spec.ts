/**
 * Lens 冒烟测试
 * 全局 setup 的 matchMedia mock 对任意 query 返回 matches: true,
 * 因此默认命中 pointer: coarse / reduced-motion 分支 → 不渲染放大层。
 * 测试放大层路径时局部覆写 matchMedia 并在 afterEach 还原。
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import Lens from '../Lens.vue'

const originalMatchMedia = window.matchMedia

function mockMatchMedia(matches: boolean) {
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false
  })) as unknown as typeof window.matchMedia
}

afterEach(() => {
  window.matchMedia = originalMatchMedia
  vi.restoreAllMocks()
})

describe('Lens', () => {
  it('挂载与卸载不抛错,插槽内容渲染;默认(coarse/reduced-motion)不渲染放大层', () => {
    const wrapper = mount(Lens, {
      slots: { default: '<img class="qr" alt="" />' }
    })
    expect(wrapper.findAll('.qr').length).toBe(1)
    expect(wrapper.find('.lens-layer').exists()).toBe(false)
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('细指针且无 reduced-motion 时渲染放大层,插槽渲染两份且 aria-hidden', () => {
    mockMatchMedia(false)
    const wrapper = mount(Lens, {
      slots: { default: '<img class="qr" alt="" />' }
    })
    expect(wrapper.findAll('.qr').length).toBe(2)
    const layer = wrapper.find('.lens-layer')
    expect(layer.exists()).toBe(true)
    expect(layer.attributes('aria-hidden')).toBe('true')
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('hover 切换放大层可见性,mousemove 不重复读布局(rect 在 mouseenter 缓存)', async () => {
    mockMatchMedia(false)
    const wrapper = mount(Lens, {
      props: { zoom: 2, size: 100 },
      slots: { default: '<span>content</span>' }
    })
    const rectSpy = vi.spyOn(wrapper.element, 'getBoundingClientRect')

    expect(wrapper.find('.lens-layer').classes()).toContain('opacity-0')
    await wrapper.trigger('mouseenter', { clientX: 30, clientY: 40 })
    expect(wrapper.find('.lens-layer').classes()).toContain('opacity-100')

    await wrapper.trigger('mousemove', { clientX: 50, clientY: 60 })
    await wrapper.trigger('mousemove', { clientX: 70, clientY: 80 })
    expect(rectSpy).toHaveBeenCalledTimes(1)

    await wrapper.trigger('mouseleave')
    expect(wrapper.find('.lens-layer').classes()).toContain('opacity-0')
    wrapper.unmount()
  })

  it('自定义 props 挂载不抛错并写入 CSS 变量', () => {
    mockMatchMedia(false)
    const wrapper = mount(Lens, {
      props: { zoom: 3, size: 200 },
      slots: { default: '<span>content</span>' }
    })
    const style = (wrapper.element as HTMLElement).style
    expect(style.getPropertyValue('--lens-r')).toBe('100px')
    expect(style.getPropertyValue('--lens-zoom')).toBe('3')
    expect(() => wrapper.unmount()).not.toThrow()
  })
})
