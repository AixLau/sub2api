import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

const originalInnerWidth = window.innerWidth

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

// The tooltip hides via a <Transition> leave animation, so display: none is
// applied asynchronously after the leave finishes rather than on next tick.
async function expectHidden(tooltip: HTMLDivElement) {
  await vi.waitFor(() => expect(tooltip.style.display).toBe('none'))
}

describe('HelpTooltip', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    setViewportWidth(originalInnerWidth)
    vi.restoreAllMocks()
  })

  it('keeps the existing hover interaction by default', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'hover details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave')
    await expectHidden(tooltip)

    wrapper.unmount()
  })

  it('supports click-to-toggle details and closes on outside click', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'click details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('click details')

    const closeButton = tooltip.querySelector('button[aria-label="Close"]')
    if (!(closeButton instanceof HTMLButtonElement)) {
      throw new Error('close button not found')
    }
    closeButton.click()
    await expectHidden(tooltip)

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await expectHidden(tooltip)

    wrapper.unmount()
  })

  it('clamps the tooltip center inside the viewport', async () => {
    setViewportWidth(390)
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'viewport details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const triggerElement = trigger.element as HTMLElement
    const tooltip = getTooltipElement()
    vi.spyOn(triggerElement, 'getBoundingClientRect').mockReturnValue({
      x: 360,
      y: 200,
      top: 200,
      right: 380,
      bottom: 220,
      left: 360,
      width: 20,
      height: 20,
      toJSON: () => ({}),
    })
    Object.defineProperty(tooltip, 'offsetWidth', {
      configurable: true,
      value: 288,
    })

    await trigger.trigger('click')
    await nextTick()

    expect(tooltip.style.left).toBe('238px')

    wrapper.unmount()
  })
})
