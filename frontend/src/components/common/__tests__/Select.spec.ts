import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    attachTo: document.body,
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await flushPromises()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

beforeEach(() => {
  setViewportWidth(1024)
})

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('keeps the teleported dropdown at the trigger width', async () => {
    setViewportWidth(1024)
    mockTriggerRect(68, 694)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('68px')
    expect(dropdown?.style.width).toBe('694px')
    expect(dropdown?.style.minWidth).toBe('')
  })

  it('preserves a narrow trigger width when space is available', async () => {
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.width).toBe('80px')
  })

  it('moves the dropdown left to fit near the right viewport edge', async () => {
    setViewportWidth(320)
    mockTriggerRect(260, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('232px')
    expect(dropdown?.style.width).toBe('80px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.width).toBe('80px')
  })

  it('clamps an offscreen-right trigger while retaining its width', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('232px')
    expect(dropdown?.style.width).toBe('80px')
  })

  it('shrinks a trigger wider than the viewport', async () => {
    setViewportWidth(320)
    mockTriggerRect(0, 500)

    const dropdown = await openSelect()

    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.width).toBe('304px')
  })
})
