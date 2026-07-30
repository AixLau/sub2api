import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SupportContactCardContent from '../SupportContactCardContent.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: { platform?: string }) =>
      params?.platform ? `${key}:${params.platform}` : key
  })
}))

describe('SupportContactCardContent', () => {
  it('uses the preview card treatment for both QR platforms', async () => {
    const wrapper = mount(SupportContactCardContent, {
      props: {
        modelValue: 'qq',
        qqQrCode: 'data:image/png;base64,qq',
        wechatQrCode: 'data:image/png;base64,wechat',
        testIdPrefix: 'support',
        'onUpdate:modelValue': (value) => wrapper.setProps({ modelValue: value })
      },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.get('[data-testid="support-tab-qq"]').classes()).toContain('bg-gray-950')
    expect(wrapper.get('[data-testid="support-tab-qq"]').classes()).toContain('text-white')
    expect(wrapper.get('[data-testid="support-tab-wechat"]').classes()).not.toContain('bg-gray-950')
    expect(wrapper.get('[data-testid="support-contact-content"]').classes()).toContain('h-[256px]')
    expect(wrapper.get('[data-testid="support-qr-image"]').classes()).toContain('h-[216px]')
    expect(wrapper.get('[data-testid="support-qr-image"]').attributes('src')).toBe(
      'data:image/png;base64,qq'
    )

    await wrapper.get('[data-testid="support-tab-wechat"]').trigger('click')

    expect(wrapper.get('[data-testid="support-tab-wechat"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="support-qr-image"]').attributes('src')).toBe(
      'data:image/png;base64,wechat'
    )
  })

  it('keeps the modal semantics and close behavior for the formal dialog', async () => {
    const wrapper = mount(SupportContactCardContent, {
      props: {
        modelValue: 'qq',
        dialog: true,
        dismissible: true,
        testIdPrefix: 'support'
      },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.get('[data-testid="support-dialog"]').attributes('role')).toBe('dialog')
    expect(wrapper.get('[data-testid="support-dialog"]').attributes('aria-modal')).toBe('true')
    expect(wrapper.get('[data-testid="support-qr-empty"]').text()).toContain(
      'common.supportQRCodeEmptyTitle:common.supportQQTab'
    )

    await wrapper.get('[data-testid="support-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
