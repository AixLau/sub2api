import {
  serializeRewardAudience,
  zonedLocalDateTimeToISO,
  type RewardAudience
} from '@/api/admin/rewards'

describe('admin reward campaign serialization', () => {
  it('converts campaign wall-clock values with the selected IANA timezone', () => {
    expect(zonedLocalDateTimeToISO('2026-07-30T09:30', 'Asia/Shanghai'))
      .toBe('2026-07-30T01:30:00.000Z')
    expect(zonedLocalDateTimeToISO('2026-01-15T09:30', 'America/New_York'))
      .toBe('2026-01-15T14:30:00.000Z')
    expect(zonedLocalDateTimeToISO('2026-07-15T09:30', 'America/New_York'))
      .toBe('2026-07-15T13:30:00.000Z')
  })

  it('converts date audience rules and preserves relative-day rules', () => {
    const audience: RewardAudience = {
      any_of: [{
        all_of: [
          { field: 'registered_at', operator: 'after', value: '2026-07-01T08:00' },
          { field: 'last_active_at', operator: 'within_days', value: 30 }
        ]
      }]
    }

    expect(serializeRewardAudience(audience, 'Asia/Shanghai')).toEqual({
      any_of: [{
        all_of: [
          {
            field: 'registered_at',
            operator: 'after',
            value: '2026-07-01T00:00:00.000Z'
          },
          { field: 'last_active_at', operator: 'within_days', value: 30 }
        ]
      }]
    })
    expect(audience.any_of[0].all_of[0].value).toBe('2026-07-01T08:00')
  })

  it('normalizes list operators to array values', () => {
    const audience: RewardAudience = {
      any_of: [{
        all_of: [
          { field: 'user_id', operator: 'in', value: 42 },
          { field: 'user_id', operator: 'not_in', value: null }
        ]
      }]
    }

    expect(serializeRewardAudience(audience, 'UTC').any_of[0].all_of)
      .toEqual([
        { field: 'user_id', operator: 'in', value: [42] },
        { field: 'user_id', operator: 'not_in', value: [] }
      ])
  })
})
