/**
 * ScratchToReveal 冒烟测试
 * jsdom 下 canvas getContext 返回 null：不渲染覆盖层，内容直接可见并 emit complete
 */
import { afterEach, describe, it, expect, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import ScratchToReveal from '../ScratchToReveal.vue'

vi.mock('@/composables/usePrefersReducedMotion', async () => {
  const { readonly, ref } = await import('vue')
  return {
    usePrefersReducedMotion: () => ({
      prefersReducedMotion: readonly(ref(false))
    })
  }
})

describe('ScratchToReveal', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('挂载与卸载不抛错，插槽内容渲染', () => {
    const wrapper = mount(ScratchToReveal, {
      slots: { default: '<p class="prize">$10</p>' }
    })
    expect(wrapper.find('.prize').exists()).toBe(true)
    expect(wrapper.find('.prize').text()).toBe('$10')
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('jsdom 无 canvas context 时移除覆盖层并 emit complete', async () => {
    const wrapper = mount(ScratchToReveal, {
      slots: { default: '<p class="prize">reward</p>' }
    })
    await nextTick()
    await nextTick()
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.find('.prize').exists()).toBe(true)
    expect(wrapper.emitted('complete')).toBeTruthy()
    wrapper.unmount()
  })

  it('自定义 props 挂载不抛错', () => {
    const wrapper = mount(ScratchToReveal, {
      props: {
        coverColor: '#0033FF',
        coverTextColor: '#ffffff',
        coverText: 'scratch me',
        threshold: 0.3,
        radius: 12
      },
      slots: { default: '<span>content</span>' }
    })
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('图片加载成功后按 cover 模式绘制覆盖层', async () => {
    const drawImage = vi.fn()
    const context = createCanvasContext(drawImage)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context)
    vi.stubGlobal('Image', createImageMock(true))

    const wrapper = mount(ScratchToReveal, {
      props: {
        coverColor: '#123456',
        coverImage: '/skin.jpg',
        coverText: 'scratch'
      },
      slots: { default: '<span>content</span>' }
    })
    await flushPromises()

    expect(context.fillRect).toHaveBeenCalled()
    expect(drawImage).toHaveBeenCalledTimes(1)
    expect(drawImage.mock.calls[0]).toHaveLength(9)
    wrapper.unmount()
  })

  it('图片加载失败时保留颜色覆盖层', async () => {
    const drawImage = vi.fn()
    const context = createCanvasContext(drawImage)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context)
    vi.stubGlobal('Image', createImageMock(false))

    const wrapper = mount(ScratchToReveal, {
      props: {
        coverColor: '#123456',
        coverImage: '/missing.jpg'
      },
      slots: { default: '<span>content</span>' }
    })
    await flushPromises()

    expect(context.fillRect).toHaveBeenCalled()
    expect(drawImage).not.toHaveBeenCalled()
    expect(wrapper.find('canvas').exists()).toBe(true)
    wrapper.unmount()
  })

  it('少量刮动后 ResizeObserver 回调不会提前完成', async () => {
    const context = createCanvasContext(vi.fn())
    context.getImageData = vi.fn(() => ({
      data: new Uint8ClampedArray([0, 0, 0, 255])
    })) as unknown as CanvasRenderingContext2D['getImageData']
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context)

    let resizeCallback: ResizeObserverCallback | null = null
    vi.stubGlobal('ResizeObserver', class ResizeObserverMock {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }

      observe() {}
      disconnect() {}
      unobserve() {}
    })

    const wrapper = mount(ScratchToReveal, {
      props: { threshold: 0.42 },
      slots: { default: '<span>content</span>' }
    })
    await flushPromises()

    await wrapper.get('canvas').trigger('pointerdown', {
      clientX: 1,
      clientY: 1,
      pointerId: 1
    })
    resizeCallback?.([], {} as ResizeObserver)
    await nextTick()

    expect(wrapper.emitted('complete')).toBeUndefined()
    wrapper.unmount()
  })
})

function createCanvasContext(drawImage: ReturnType<typeof vi.fn>) {
  return {
    fillStyle: '',
    font: '',
    textAlign: 'start',
    textBaseline: 'alphabetic',
    globalCompositeOperation: 'source-over',
    lineWidth: 1,
    lineCap: 'butt',
    shadowColor: '',
    shadowBlur: 0,
    fillRect: vi.fn(),
    fillText: vi.fn(),
    drawImage,
    beginPath: vi.fn(),
    arc: vi.fn(),
    fill: vi.fn(),
    moveTo: vi.fn(),
    lineTo: vi.fn(),
    stroke: vi.fn(),
    getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) }))
  } as unknown as CanvasRenderingContext2D
}

function createImageMock(succeeds: boolean) {
  return class ImageMock {
    decoding = ''
    naturalWidth = 1320
    naturalHeight = 500
    onload: (() => void) | null = null
    onerror: (() => void) | null = null

    set src(_value: string) {
      if (succeeds) {
        this.onload?.()
      } else {
        this.onerror?.()
      }
    }
  }
}
