import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import RewardGrantDialog from '../RewardGrantDialog.vue'
import type { RewardGrant } from '@/types'

const mocks = vi.hoisted(() => ({
  claimReward: vi.fn(),
  fireCelebration: vi.fn(),
}))

vi.mock('@/api', () => ({
  userAPI: {
    claimReward: mocks.claimReward,
  },
}))

vi.mock('@/components/inspira/confetti', () => ({
  fireCelebration: mocks.fireCelebration,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'en' },
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const BaseDialogStub = defineComponent({
  props: {
    show: Boolean,
    title: String,
    closeOnEscape: Boolean,
    closeOnClickOutside: Boolean,
    showCloseButton: Boolean,
  },
  emits: ['close'],
  template: `
    <section>
      <p data-testid="dialog-title">{{ title }}</p>
      <button data-testid="dialog-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `,
})

const ScratchToRevealStub = defineComponent({
  props: {
    coverColor: String,
    coverImage: String,
    coverTextColor: String,
    coverText: String,
  },
  emits: ['complete'],
  template: `
    <div data-testid="scratch">
      <button data-testid="scratch-complete" @click="$emit('complete')">scratch</button>
      <slot />
    </div>
  `,
})

const sampleGrant: RewardGrant = {
  grant_id: 19,
  campaign_id: 7,
  title: 'Summer reward',
  hint: 'A thank-you for staying active',
  cover_text: 'Reveal the balance',
  claim_cta: 'Back to dashboard',
  success_message: 'Your campaign credit is ready',
  skin: {
    id: 3,
    name: 'Summer',
    image_url: '/api/v1/user/reward-skins/3/content',
    cover_color: '#123456',
    cover_text_color: '#ffffff',
    alt: 'Summer reward cover',
  },
  priority: 20,
  expires_at: '2026-08-01T08:00:00Z',
}

function mountDialog(grant = sampleGrant) {
  return mount(RewardGrantDialog, {
    props: { show: true, grant },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ScratchToReveal: ScratchToRevealStub,
        LoadingSpinner: true,
        Icon: true,
      },
    },
  })
}

describe('RewardGrantDialog', () => {
  beforeEach(() => {
    mocks.claimReward.mockReset()
    mocks.fireCelebration.mockReset()
    mocks.claimReward.mockResolvedValue({
      grant_id: 19,
      amount: 4,
      balance: 104,
      claimed_at: '2026-07-30T10:00:00Z',
    })
  })

  it('renders server copy and same-origin skin without exposing an amount', () => {
    const wrapper = mountDialog()
    const scratch = wrapper.getComponent(ScratchToRevealStub)

    expect(wrapper.get('[data-testid="dialog-title"]').text()).toBe('Summer reward')
    expect(wrapper.text()).toContain('A thank-you for staying active')
    expect(wrapper.text()).not.toContain('$4.00')
    expect(scratch.props('coverColor')).toBe('#123456')
    expect(scratch.props('coverText')).toBe('Reveal the balance')
    expect(scratch.props('coverImage')).toContain('/api/v1/user/reward-skins/3/content')
  })

  it('allows deferring before the scratch is claimed', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="dialog-close"]').trigger('click')

    expect(wrapper.emitted('defer')).toHaveLength(1)
    expect(mocks.claimReward).not.toHaveBeenCalled()
  })

  it('claims on reveal, displays USD, and finishes separately', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="scratch-complete"]').trigger('click')
    await flushPromises()

    expect(mocks.claimReward).toHaveBeenCalledWith(19)
    expect(wrapper.text()).toContain('$4.00')
    expect(wrapper.text()).toContain('Your campaign credit is ready')
    expect(wrapper.get('[data-testid="reward-finish"]').text()).toContain('Back to dashboard')
    expect(wrapper.emitted('claimed')?.[0]?.[0]).toMatchObject({ balance: 104 })
    expect(mocks.fireCelebration).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('finish')).toBeUndefined()

    await wrapper.get('[data-testid="reward-finish"]').trigger('click')
    expect(wrapper.emitted('finish')).toHaveLength(1)
  })

  it('supports claim retry after a transient failure', async () => {
    mocks.claimReward
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({
        grant_id: 19,
        amount: 3,
        balance: 103,
        claimed_at: '2026-07-30T10:00:00Z',
      })
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="scratch-complete"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('rewardQueue.claimFailed')

    await wrapper.get('[data-testid="reward-claim-retry"]').trigger('click')
    await flushPromises()

    expect(mocks.claimReward).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('$3.00')
    expect(mocks.fireCelebration).toHaveBeenCalledTimes(1)
  })

  it('rejects cross-origin skin images before they reach the canvas', () => {
    const wrapper = mountDialog({
      ...sampleGrant,
      skin: { ...sampleGrant.skin, image_url: 'https://cdn.example.com/reward.webp' },
    })

    expect(wrapper.getComponent(ScratchToRevealStub).props('coverImage')).toBeUndefined()
  })
})
