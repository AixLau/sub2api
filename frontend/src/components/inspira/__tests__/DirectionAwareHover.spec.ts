/**
 * DirectionAwareHover 冒烟测试
 *
 * 注意：全局 setup.ts 把 matchMedia mock 成对任何 query 都返回 matches:true，
 * 即同时命中 reduced-motion 与 (hover: none)（触屏）分支。测试方向滑入路径时
 * 需局部覆写 matchMedia 按 query 返回，并在 afterEach 还原。
 *
 * jsdom 中 getBoundingClientRect 全为 0，元素中心即 (0, 0)，
 * 因此用带符号的 clientX/clientY 即可模拟从四个象限进入。
 */
import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import DirectionAwareHover from '../DirectionAwareHover.vue'

const originalMatchMedia = window.matchMedia
const originalRaf = globalThis.requestAnimationFrame

/** 按 query 精确控制 matches，未列出的 query 默认 false */
function mockMatchMedia(matchesByQuery: Record<string, boolean>) {
  window.matchMedia = ((query: string) => ({
    matches: matchesByQuery[query] ?? false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  })) as unknown as typeof window.matchMedia
}

/** 让双 rAF 同步执行，便于断言 dah-visible */
function useSyncRaf() {
  globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    cb(0)
    return 0
  }) as typeof requestAnimationFrame
}

afterEach(() => {
  window.matchMedia = originalMatchMedia
  globalThis.requestAnimationFrame = originalRaf
})

function mountWithMouse() {
  mockMatchMedia({}) // reduced-motion 与 (hover: none) 均为 false
  useSyncRaf()
  return mount(DirectionAwareHover, {
    slots: { default: '<span class="content">hi</span>' }
  })
}

describe('DirectionAwareHover', () => {
  it('渲染插槽内容与 aria-hidden 高光层', () => {
    const wrapper = mount(DirectionAwareHover, {
      slots: { default: '<span class="content">hi</span>' }
    })
    expect(wrapper.find('.content').exists()).toBe(true)
    const layer = wrapper.find('.dah-layer')
    expect(layer.exists()).toBe(true)
    expect(layer.attributes('aria-hidden')).toBe('true')
  })

  it.each([
    ['top', { clientX: 0, clientY: -10 }],
    ['right', { clientX: 10, clientY: 0 }],
    ['bottom', { clientX: 0, clientY: 10 }],
    ['left', { clientX: -10, clientY: 0 }]
  ] as const)('从 %s 进入时高光层带 dah-from-%s 且滑入可见', async (dir, coords) => {
    const wrapper = mountWithMouse()
    await wrapper.trigger('mouseenter', coords)
    await nextTick()

    const layer = wrapper.find('.dah-layer')
    expect(layer.classes()).toContain(`dah-from-${dir}`)
    expect(layer.classes()).toContain('dah-visible')
    wrapper.unmount()
  })

  it('mouseleave 时方向更新为离开方向并隐藏（向该方向滑出）', async () => {
    const wrapper = mountWithMouse()
    await wrapper.trigger('mouseenter', { clientX: 0, clientY: -10 })
    await nextTick()
    expect(wrapper.find('.dah-layer').classes()).toContain('dah-from-top')

    await wrapper.trigger('mouseleave', { clientX: 10, clientY: 0 })
    const layer = wrapper.find('.dah-layer')
    expect(layer.classes()).toContain('dah-from-right')
    expect(layer.classes()).not.toContain('dah-visible')
    wrapper.unmount()
  })

  it('color prop 通过 CSS 变量透传', async () => {
    mockMatchMedia({})
    useSyncRaf()
    const wrapper = mount(DirectionAwareHover, {
      props: { color: 'rgba(1, 2, 3, 0.5)' },
      slots: { default: '<span>x</span>' }
    })
    expect(wrapper.find('.dah-layer').attributes('style')).toContain('--dah-color: rgba(1, 2, 3, 0.5)')
    wrapper.unmount()
  })

  it('reduced-motion 时只淡入淡出，不带方向滑动类', async () => {
    mockMatchMedia({ '(prefers-reduced-motion: reduce)': true })
    const wrapper = mount(DirectionAwareHover, {
      slots: { default: '<span>x</span>' }
    })
    await wrapper.trigger('mouseenter', { clientX: 0, clientY: -10 })

    const layer = wrapper.find('.dah-layer')
    expect(layer.classes()).toContain('dah-fade')
    expect(layer.classes()).toContain('dah-visible')
    expect(layer.classes().some((c) => c.startsWith('dah-from-'))).toBe(false)

    await wrapper.trigger('mouseleave', { clientX: 10, clientY: 0 })
    expect(wrapper.find('.dah-layer').classes()).not.toContain('dah-visible')
    wrapper.unmount()
  })

  it('触屏（hover: none）时 mouseenter 无副作用', async () => {
    mockMatchMedia({ '(hover: none)': true })
    const wrapper = mount(DirectionAwareHover, {
      slots: { default: '<span>x</span>' }
    })
    await wrapper.trigger('mouseenter', { clientX: 0, clientY: -10 })
    await nextTick()
    expect(wrapper.find('.dah-layer').classes()).not.toContain('dah-visible')
    wrapper.unmount()
  })
})
