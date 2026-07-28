import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import WelcomeRewardDialog from '../WelcomeRewardDialog.vue'

const fireCelebrationMock = vi.hoisted(() => vi.fn())
const claimWelcomeRewardMock = vi.hoisted(() => vi.fn())
const claimSurpriseRewardMock = vi.hoisted(() => vi.fn())

vi.mock('@/components/inspira/confetti', () => ({
  fireCelebration: fireCelebrationMock
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    claimWelcomeReward: claimWelcomeRewardMock,
    claimSurpriseReward: claimSurpriseRewardMock
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const BaseDialogStub = defineComponent({
  props: {
    show: Boolean,
    title: String,
    closeOnEscape: Boolean,
    closeOnClickOutside: Boolean,
    showCloseButton: Boolean
  },
  emits: ['close'],
  template: `
    <section>
      <button data-testid="dialog-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `
})

const ScratchToRevealStub = defineComponent({
  props: {
    coverColor: String,
    coverImage: String,
    coverTextColor: String
  },
  emits: ['complete'],
  template: `
    <button data-testid="scratch" @click="$emit('complete')">
      <slot />
    </button>
  `
})

function mountDialog() {
  return mount(WelcomeRewardDialog, {
    props: { show: true, skinId: 'lucky-passage' },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ScratchToReveal: ScratchToRevealStub,
        LoadingSpinner: true,
        Icon: true
      }
    }
  })
}

describe('WelcomeRewardDialog', () => {
  beforeEach(() => {
    fireCelebrationMock.mockReset()
    claimWelcomeRewardMock.mockReset()
    claimSurpriseRewardMock.mockReset()
    claimWelcomeRewardMock.mockResolvedValue({ amount: 4, balance: 104 })
    claimSurpriseRewardMock.mockResolvedValue({ amount: 3, balance: 103 })
    vi.stubGlobal('Image', createLoadedImageMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not show a claim failure before the scratch completes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.text()).not.toContain('welcomeReward.claimFailed')
    expect(claimWelcomeRewardMock).not.toHaveBeenCalled()
  })

  it('requires the user to reveal the reward before finishing', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="dialog-close"]').trigger('click')
    expect(wrapper.emitted('finish')).toBeUndefined()

    await wrapper.get('[data-testid="scratch"]').trigger('click')
    await flushPromises()
    expect(claimWelcomeRewardMock).toHaveBeenCalledTimes(1)
    expect(fireCelebrationMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('¥4.00')
    expect(wrapper.text()).toContain('welcomeReward.revealedHint')

    await wrapper.get('button.btn-primary').trigger('click')
    expect(wrapper.emitted('finish')).toHaveLength(1)
  })

  it('fires the celebration only once', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const scratch = wrapper.get('[data-testid="scratch"]')

    await scratch.trigger('click')
    await scratch.trigger('click')
    await flushPromises()

    expect(fireCelebrationMock).toHaveBeenCalledTimes(1)
  })

  it('uses the surprise reward copy and claim endpoint for active-user rewards', async () => {
    const wrapper = mountDialog()
    await wrapper.setProps({ variant: 'surprise' })
    await flushPromises()

    expect(wrapper.text()).toContain('surpriseReward.scratchHint')
    await wrapper.get('[data-testid="scratch"]').trigger('click')
    await flushPromises()

    expect(claimSurpriseRewardMock).toHaveBeenCalledTimes(1)
    expect(claimWelcomeRewardMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('¥3.00')
    expect(fireCelebrationMock).toHaveBeenCalledTimes(1)
  })

  it('does not celebrate or allow closing when the claim fails', async () => {
    claimWelcomeRewardMock.mockRejectedValueOnce(new Error('claim failed'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="scratch"]').trigger('click')
    await flushPromises()

    expect(fireCelebrationMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('welcomeReward.claimFailed')
    await wrapper.get('[data-testid="dialog-close"]').trigger('click')
    expect(wrapper.emitted('finish')).toBeUndefined()
  })

  it('waits for the skin to decode before mounting the scratch cover', async () => {
    let resolveImage: (() => void) | null = null
    vi.stubGlobal('Image', class ImageMock {
      decoding = ''
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      decode = vi.fn().mockResolvedValue(undefined)

      set src(_value: string) {
        resolveImage = () => this.onload?.()
      }
    })
    const wrapper = mountDialog()

    expect(wrapper.find('[data-testid="scratch"]').exists()).toBe(false)
    resolveImage?.()
    await flushPromises()

    expect(wrapper.find('[data-testid="scratch"]').exists()).toBe(true)
  })

  it('passes the selected IP skin to the scratch cover', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const scratch = wrapper.getComponent(ScratchToRevealStub)

    expect(scratch.props('coverImage')).toContain('lucky-passage.webp')
    expect(scratch.props('coverColor')).toBe('#13263a')
    expect(scratch.props('coverTextColor')).toBe('#fff5dd')
  })
})

function createLoadedImageMock() {
  return class ImageMock {
    decoding = ''
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    decode = vi.fn().mockResolvedValue(undefined)

    set src(_value: string) {
      this.onload?.()
    }
  }
}
