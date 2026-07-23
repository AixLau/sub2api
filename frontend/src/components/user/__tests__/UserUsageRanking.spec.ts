import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UserUsageRanking from '../UserUsageRanking.vue'

const getUserRanking = vi.fn()

vi.mock('@/api', () => ({
  usageAPI: {
    getUserRanking: (...args: unknown[]) => getUserRanking(...args),
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const mountRanking = () => mount(UserUsageRanking, {
  props: {
    startDate: '2026-07-01',
    endDate: '2026-07-08',
    startTime: '2026-07-01T00:00:00Z',
    endTime: '2026-07-08T00:00:00Z',
  },
  global: {
    stubs: {
      Icon: true,
      LoadingSpinner: true,
    },
  },
})

describe('UserUsageRanking', () => {
  beforeEach(() => {
    getUserRanking.mockReset()
    getUserRanking.mockResolvedValue({
      ranking: [
        { rank: 1, display_name: 'a***@e***.com', requests: 10, total_tokens: 1000, is_current: false },
        { rank: 2, display_name: 'me@example.com', requests: 8, total_tokens: 800, is_current: true },
      ],
    })
  })

  it('loads the fixed ranking range and highlights only a current-user row returned in Top 20', async () => {
    const wrapper = mountRanking()
    await flushPromises()

    expect(getUserRanking).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      start_time: '2026-07-01T00:00:00Z',
      end_time: '2026-07-08T00:00:00Z',
    })
    expect(wrapper.text()).toContain('a***@e***.com')
    expect(wrapper.text()).toContain('me@example.com')
    expect(wrapper.findAll('[aria-current="true"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('total_users')
  })

  it('does not render a separate current-user area when the current user is outside Top 20', async () => {
    getUserRanking.mockResolvedValue({
      ranking: [{ rank: 1, display_name: 'a***@e***.com', requests: 10, total_tokens: 1000, is_current: false }],
    })

    const wrapper = mountRanking()
    await flushPromises()

    expect(wrapper.findAll('[aria-current="true"]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('me@example.com')
    expect(wrapper.text()).not.toContain('Top 20 outside')
  })
})
