import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import LiquidGlassReadabilityPreviewView from '../LiquidGlassReadabilityPreviewView.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: { platform?: string }) =>
      params?.platform ? `${key}:${params.platform}` : key
  })
}))

describe('LiquidGlassReadabilityPreviewView', () => {
  it('keeps one glass surface without an extra gray backdrop and switches to WeChat', async () => {
    const wrapper = shallowMount(LiquidGlassReadabilityPreviewView, {
      global: {
        stubs: {
          LiquidGlass: {
            template: '<div class="preview-liquid-glass-stub"><slot /></div>'
          },
          LiquidGlassBackdrop: {
            template: '<div data-testid="liquid-glass-backdrop" />'
          },
          Icon: true
        }
      }
    })

    expect(wrapper.get('[data-testid="liquid-glass-readability-preview"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="liquid-glass-backdrop"]').exists()).toBe(true)
    expect(wrapper.findAll('.preview-liquid-glass-stub')).toHaveLength(1)
    expect(wrapper.get('[data-testid="readability-preview-outer-liquid-glass"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="preview-inner-liquid-glass"]').exists()).toBe(false)
    expect(wrapper.find('.preview-calm-zone').exists()).toBe(false)
    expect(wrapper.get('.preview-reading-frost').exists()).toBe(true)
    expect(wrapper.get('[data-testid="readability-preview-subtitle"]').classes()).toContain(
      'text-gray-800'
    )
    expect(wrapper.get('[data-testid="preview-qq-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="preview-wechat-qr"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="preview-tab-qq"]').attributes('aria-selected')).toBe('true')

    await wrapper.get('[data-testid="preview-tab-wechat"]').trigger('click')

    expect(wrapper.get('[data-testid="preview-tab-wechat"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="preview-qq-empty"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="preview-wechat-qr"]').attributes('src')).toContain(
      'support-wechat-preview.webp'
    )
    expect(wrapper.get('[data-testid="preview-wechat-qr"]').attributes('alt')).toBe(
      'common.supportQRCodeAlt:common.supportWeChatTab'
    )
  })
})
