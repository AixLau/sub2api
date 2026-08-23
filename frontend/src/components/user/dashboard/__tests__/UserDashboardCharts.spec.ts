import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import UserDashboardCharts from '../UserDashboardCharts.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const DateRangePickerStub = defineComponent({
  name: 'DateRangePicker',
  props: ['startDate', 'endDate'],
  emits: ['update:startDate', 'update:endDate', 'change'],
  template: '<div data-testid="date-range-picker-stub" />',
})

const SelectStub = defineComponent({
  name: 'DashboardGranularitySelectStub',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: '<div data-testid="granularity-select-stub" />',
})

const TokenUsageTrendStub = defineComponent({
  name: 'TokenUsageTrend',
  props: ['trendData', 'loading', 'showCost', 'surface', 'chartHeightClass'],
  template: '<div data-testid="token-trend-stub" />',
})

function mountCharts() {
  return mount(UserDashboardCharts, {
    props: {
      loading: false,
      startDate: '2026-06-13',
      endDate: '2026-06-19',
      granularity: 'day',
      trend: [],
      models: [],
    },
    global: {
      stubs: {
        DateRangePicker: DateRangePickerStub,
        Select: SelectStub,
        TokenUsageTrend: TokenUsageTrendStub,
        LoadingSpinner: true,
        Icon: true,
      },
    },
  })
}

describe('UserDashboardCharts controls', () => {
  it('forwards date, granularity, and refresh interactions with typed values', async () => {
    const wrapper = mountCharts()
    const datePicker = wrapper.getComponent(DateRangePickerStub)
    const granularitySelect = wrapper.getComponent(SelectStub)
    const range = {
      startDate: '2026-06-01',
      endDate: '2026-06-07',
      preset: 'last7days',
    }

    datePicker.vm.$emit('update:startDate', range.startDate)
    datePicker.vm.$emit('update:endDate', range.endDate)
    datePicker.vm.$emit('change', range)
    granularitySelect.vm.$emit('update:modelValue', 'hour')
    granularitySelect.vm.$emit('change')
    await wrapper.get('[data-testid="user-dashboard-refresh"]').trigger('click')

    expect(wrapper.emitted('update:startDate')).toEqual([[range.startDate]])
    expect(wrapper.emitted('update:endDate')).toEqual([[range.endDate]])
    expect(wrapper.emitted('dateRangeChange')).toEqual([[range]])
    expect(wrapper.emitted('update:granularity')).toEqual([['hour']])
    expect(wrapper.emitted('granularityChange')).toHaveLength(1)
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    granularitySelect.vm.$emit('update:modelValue', 'week')
    expect(wrapper.emitted('update:granularity')).toEqual([['hour']])
  })

  it('keeps the model panel and exposes its localized empty state', () => {
    const wrapper = mountCharts()

    expect(wrapper.get('[data-testid="user-dashboard-model-distribution"]').attributes('aria-busy')).toBe('false')
    expect(wrapper.get('[data-testid="user-dashboard-model-empty"]').text()).toBe('dashboard.noDataAvailable')
    expect(wrapper.find('[data-testid="user-dashboard-model-table"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="token-trend-stub"]').exists()).toBe(true)
    expect(wrapper.getComponent(TokenUsageTrendStub).props('chartHeightClass')).toBe('h-48')
  })
})
