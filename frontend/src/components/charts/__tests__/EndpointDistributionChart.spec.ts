const dashboardMocks = vi.hoisted(() => ({
  getUserBreakdown: vi.fn(),
}))

vi.mock('@/api/admin/dashboard', () => ({
  getUserBreakdown: dashboardMocks.getUserBreakdown,
}))

import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

import EndpointDistributionChart from '../EndpointDistributionChart.vue'

const messages: Record<string, string> = {
  'usage.endpointDistribution': 'Endpoint Distribution',
  'usage.endpoint': 'Endpoint',
  'usage.inbound': 'Inbound',
  'usage.upstream': 'Upstream',
  'usage.path': 'Path',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

const VID_A = '/api/v1/admin/async-tasks/1/vidtask_' + 'a'.repeat(64) + '/refresh'
const VID_B = '/api/v1/admin/async-tasks/2/vidtask_' + 'b'.repeat(64) + '/refresh'
const VID_NORMALIZED = '/api/v1/admin/async-tasks/:id/:id/refresh'

function stat(endpoint: string, overrides: Partial<{ requests: number; total_tokens: number; cost: number; actual_cost: number }> = {}) {
  return {
    endpoint,
    requests: overrides.requests ?? 1,
    total_tokens: overrides.total_tokens ?? 100,
    cost: overrides.cost ?? 0.5,
    actual_cost: overrides.actual_cost ?? 0.25,
  }
}

function mountChart(props: Record<string, unknown>) {
  return mount(EndpointDistributionChart, {
    props: {
      endpointStats: [],
      ...props,
    },
    global: {
      stubs: {
        LoadingSpinner: true,
        UserBreakdownSubTable: true,
      },
    },
  })
}

describe('EndpointDistributionChart endpoint normalization', () => {
  beforeEach(() => {
    dashboardMocks.getUserBreakdown.mockReset()
    dashboardMocks.getUserBreakdown.mockResolvedValue({ users: [] })
  })

  it('merges vidtask endpoints that differ only by task id into one row', () => {
    const wrapper = mountChart({
      endpointStats: [
        stat(VID_A, { requests: 2, total_tokens: 300 }),
        stat(VID_B, { requests: 3, total_tokens: 700 }),
        stat('/v1/messages', { requests: 5, total_tokens: 500 }),
      ],
    })

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    // Higher-token bucket first; merged vidtask row sums both raw rows.
    expect(rows[0].text()).toContain(VID_NORMALIZED)
    expect(rows[0].text()).toContain('5')
    expect(rows[0].text()).toContain('1.00K')
    expect(rows[1].text()).toContain('/v1/messages')

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual([VID_NORMALIZED, '/v1/messages'])
    expect(chartData.datasets[0].data).toEqual([1000, 500])
  })

  it('truncates long endpoints in a single line with the full text in title', () => {
    const wrapper = mountChart({
      endpointStats: [stat(VID_A)],
    })

    const cell = wrapper.get('tbody td')
    expect(cell.attributes('title')).toBe(VID_NORMALIZED)
    const text = cell.find('span.truncate')
    expect(text.exists()).toBe(true)
    expect(text.text()).toBe(VID_NORMALIZED)
    expect(text.classes()).toContain('min-w-0')
  })

  it('sends the original raw endpoint (not the normalized template) for breakdown queries', async () => {
    const wrapper = mountChart({
      endpointStats: [
        stat(VID_A, { total_tokens: 300 }),
        stat(VID_B, { total_tokens: 700 }),
      ],
      source: 'inbound',
      startDate: '2026-10-01',
      endDate: '2026-10-22',
      filters: { model: 'gpt-5' },
    })

    await wrapper.get('tbody tr').trigger('click')

    expect(dashboardMocks.getUserBreakdown).toHaveBeenCalledTimes(1)
    const query = dashboardMocks.getUserBreakdown.mock.calls[0][0]
    // VID_B dominates by tokens, so its raw endpoint represents the merged bucket.
    expect(query.endpoint).toBe(VID_B)
    expect(query.endpoint_type).toBe('inbound')
    expect(query.model).toBe('gpt-5')
    expect(query.start_date).toBe('2026-10-01')
    expect(query.end_date).toBe('2026-10-22')
  })

  it('applies the same normalization to upstream and path sources', async () => {
    const wrapper = mountChart({
      endpointStats: [stat('/v1/messages')],
      upstreamEndpointStats: [stat(VID_A, { total_tokens: 10 }), stat(VID_B, { total_tokens: 20 })],
      endpointPathStats: [stat(VID_A), stat('/v1/chat/completions')],
      source: 'upstream',
      showSourceToggle: true,
    })

    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain(VID_NORMALIZED)

    const pathButton = wrapper.findAll('button').find((button) => button.text() === 'Path')!
    await pathButton.trigger('click')
    await wrapper.setProps({ source: 'path' })

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows.map((row) => row.text()).join()).toContain(VID_NORMALIZED)
    expect(rows.map((row) => row.text()).join()).toContain('/v1/chat/completions')
  })

  it('keeps normalized endpoints distinct from genuinely different endpoints', () => {
    const wrapper = mountChart({
      endpointStats: [
        stat('/v1/messages'),
        stat('/v1/chat/completions'),
        stat('/v1/models/gemini-2.5-pro:generateContent'),
      ],
    })

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(3)
    expect(wrapper.text()).not.toContain(':id')
  })
})
