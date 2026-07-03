import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import type { OpsDashboardOverview } from '@/api/admin/ops'

vi.mock('@/api', () => ({
  adminAPI: {
    groups: {
      getAll: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ summary: null }),
  },
}))

vi.mock('@/stores', () => ({
  useAdminSettingsStore: () => ({
    opsRealtimeMonitoringEnabled: false,
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, any>) => {
        if (key === 'admin.ops.diagnosis.ttftHigh' && params) {
          return `avg ttft high ${params.ttft}ms`
        }
        return key
      },
    }),
  }
})

const baseOverview = (ttft: Partial<OpsDashboardOverview['ttft']>): OpsDashboardOverview => ({
  start_time: '2026-07-02T00:00:00Z',
  end_time: '2026-07-02T01:00:00Z',
  platform: '',
  group_id: null,
  health_score: 100,
  system_metrics: null,
  job_heartbeats: [],
  success_count: 10,
  error_count_total: 0,
  business_limited_count: 0,
  error_count_sla: 0,
  request_count_total: 10,
  request_count_sla: 10,
  token_consumed: 100,
  sla: 1,
  error_rate: 0,
  upstream_error_rate: 0,
  upstream_error_count_excl_429_529: 0,
  upstream_429_count: 0,
  upstream_529_count: 0,
  qps: { current: 1, peak: 1, avg: 1 },
  tps: { current: 0, peak: 0, avg: 0 },
  duration: {},
  ttft: {
    p99_ms: null,
    avg_ms: null,
    ...ttft,
  },
})

const mountHeader = (overview: OpsDashboardOverview) => mount(OpsDashboardHeader, {
  props: {
    overview,
    platform: '',
    groupId: null,
    timeRange: '1h',
    queryMode: 'auto',
    loading: false,
    lastUpdated: new Date('2026-07-02T01:00:00Z'),
  },
  global: {
    stubs: {
      Select: true,
      HelpTooltip: true,
      BaseDialog: true,
      Icon: true,
    },
  },
})

describe('OpsDashboardHeader diagnostics', () => {
  it('uses average TTFT instead of P99 for the TTFT warning', () => {
    const highP99LowAverage = mountHeader(baseOverview({ p99_ms: 24_157, avg_ms: 400 }))
    expect(highP99LowAverage.text()).not.toContain('avg ttft high')

    const highAverage = mountHeader(baseOverview({ p99_ms: 24_157, avg_ms: 5_671 }))
    expect(highAverage.text()).toContain('avg ttft high 5671ms')
  })
})
