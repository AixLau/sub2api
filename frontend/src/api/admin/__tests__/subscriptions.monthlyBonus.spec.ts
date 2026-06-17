import { describe, expect, it, vi } from 'vitest'

import { addMonthlyBonus } from '../subscriptions'

const post = vi.hoisted(() => vi.fn())

vi.mock('../../client', () => ({
  apiClient: {
    post
  }
}))

describe('admin subscriptions API monthly bonus', () => {
  it('posts the current-month bonus amount to the subscription endpoint', async () => {
    const subscription = { id: 42, monthly_bonus_usd: 25 }
    post.mockResolvedValueOnce({ data: subscription })

    await expect(addMonthlyBonus(42, 25)).resolves.toBe(subscription)

    expect(post).toHaveBeenCalledWith('/admin/subscriptions/42/add-monthly-bonus', {
      amount_usd: 25
    })
  })
})
