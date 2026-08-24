import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PurchaseHero from '../purchase/PurchaseHero.vue'
import PurchasePageStage from '../purchase/PurchasePageStage.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

describe('PurchaseHero', () => {
  it('renders the generated headline artwork while retaining an exact accessible heading', () => {
    const wrapper = mount(PurchaseHero)

    expect(wrapper.get('#purchase-hero-title').classes()).toContain('sr-only')
    expect(wrapper.get('#purchase-hero-title').text()).toContain('payment.purchaseHero.lineOne')
    expect(wrapper.get('#purchase-hero-title').text()).toContain('payment.purchaseHero.lineTwoLead')
    expect(wrapper.get('#purchase-hero-title').text()).toContain('payment.purchaseHero.lineTwoAccent')
    expect(wrapper.get('[data-testid="purchase-headline-art"]').attributes('src'))
      .toBe('/assets/purchase/purchase-headline-transparent.webp')
  })

  it('keeps the balance outside the hero column and omits the retired right rail', async () => {
    const wrapper = mount(PurchasePageStage, {
      props: { formattedBalance: '$7,365.87' },
    })

    const hero = wrapper.get('[data-testid="purchase-hero"]')
    const balance = wrapper.get('[data-testid="purchase-balance-ticket"]')

    expect(hero.find('[data-testid="purchase-balance-ticket"]').exists()).toBe(false)
    expect(hero.find('[data-testid="purchase-right-rail"]').exists()).toBe(false)
    expect(balance.text()).toContain('$7,365.87')
    expect(wrapper.find('[data-testid="purchase-right-rail"]').exists()).toBe(false)

    await wrapper.setProps({ formattedBalance: '$12.34' })
    expect(wrapper.get('[data-testid="purchase-balance-ticket"]').text()).toContain('$12.34')
    expect(wrapper.text()).not.toContain('$7,365.87')
  })

  it('keeps only the robot and power-up assets inside the hero decoration layer', () => {
    const wrapper = mount(PurchaseHero, {
      attachTo: document.body,
    })
    const decorations = wrapper.get('[data-testid="purchase-decorations"]')
    const images = decorations.findAll('img')

    expect(images.map(image => image.attributes('src'))).toEqual([
      '/assets/purchase/energy-gacha-robot-direct.webp',
      '/assets/purchase/power-up-badge.webp',
    ])
    expect(images[0]?.attributes()).toMatchObject({
      width: '1536',
      height: '1024',
    })
    for (const image of images) {
      expect(image.attributes('alt')).toBe('')
      expect(image.attributes('aria-hidden')).toBe('true')
      expect((image.element as HTMLImageElement).draggable).toBe(false)
      expect(image.classes()).toContain('purchase-decoration')
      expect(image.attributes('width')).toBeTruthy()
      expect(image.attributes('height')).toBeTruthy()
    }

    wrapper.unmount()
  })
})
