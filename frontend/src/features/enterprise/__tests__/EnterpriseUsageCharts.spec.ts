import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { Doughnut, Bar } from 'vue-chartjs'
import type { ChartOptions, TooltipItem } from 'chart.js'
import type { EnterpriseUsageLog } from '@/types/enterprise'
import EnterpriseUsageDistribution from '../components/EnterpriseUsageDistribution.vue'
import EnterpriseUsageRanking from '../components/EnterpriseUsageRanking.vue'
import { selectOption } from './selectOption'
import { normalizeEnterpriseUsage } from '../useEnterpriseUsage'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({ Doughnut: { name: 'Doughnut', props: ['data', 'options'], template: '<canvas />' }, Bar: { name: 'Bar', props: ['data', 'options'], template: '<canvas />' } }))
const wrappers: VueWrapper[] = []
const stubs = { DataTable: { props: ['data'], template: '<div data-test="table">{{ JSON.stringify(data) }}</div>' } }
function distribution(items = [{ model: 'small', requests: 1, total_tokens: 200, actual_cost: 1 }, { model: 'large', requests: 3, total_tokens: 800, actual_cost: 3 }]) {
  const wrapper = mount(EnterpriseUsageDistribution, { props: { items, loading: false }, global: { stubs } })
  wrappers.push(wrapper); return wrapper
}
const rankingRows = Array.from({ length: 12 }, (_, index) => ({ id: index, label: `member-${index}`, requests: index + 1, total_tokens: index * 100, actual_cost: index / 10 }))
function ranking(items = rankingRows) {
  const wrapper = mount(EnterpriseUsageRanking, { props: { title: 'Members', items, loading: false }, global: { stubs } })
  wrappers.push(wrapper); return wrapper
}
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.documentElement.classList.remove('dark') })

describe('enterprise model doughnut', () => {
  it('maps costs, requests and tokens into slices and legends and shows every metric with its own percentage', async () => {
    const wrapper = distribution()
    const chart = wrapper.getComponent(Doughnut)
    expect(chart.props('data')).toMatchObject({ labels: ['large', 'small'], datasets: [{ data: [3, 1] }] })
    expect(wrapper.get('ul').text()).toContain('75.0%')
    const callback = (chart.props('options') as ChartOptions<'doughnut'>).plugins!.tooltip!.callbacks!.label!
    expect(callback.call({} as never, { dataIndex: 0 } as TooltipItem<'doughnut'>)).toEqual(['enterprise.requests: 3 (75.0%)', 'enterprise.tokens: 800 (80.0%)', 'enterprise.cost: $3.0000 (75.0%)'])
    expect(wrapper.find('select').exists()).toBe(false)
    await selectOption(wrapper, '[data-test="usage-distribution-metric"]', 'enterprise.tokens')
    expect(chart.props('data').datasets[0].data).toEqual([800, 200])
    await selectOption(wrapper, '[data-test="usage-distribution-metric"]', 'enterprise.requests')
    expect(chart.props('data').datasets[0].data).toEqual([3, 1])
  })
  it('handles empty, single and zero-total data without invalid percentages', () => {
    expect(distribution([]).findComponent(Doughnut).exists()).toBe(false)
    const single = distribution([{ model: 'one', requests: 1, total_tokens: 5, actual_cost: 0.1 }])
    expect(single.getComponent(Doughnut).props('data').datasets[0].data).toEqual([0.1])
    expect(single.get('ul').text()).toContain('100.0%')
    const zero = distribution([{ model: 'free', requests: 0, total_tokens: 0, actual_cost: 0 }])
    expect(zero.text()).not.toMatch(/NaN|Infinity/)
    expect(zero.get('ul').text()).toContain('0.0%')
  })
})

describe('enterprise horizontal rankings', () => {
  it('sorts by cost, limits chart to ten and switches to a complete table', async () => {
    const wrapper = ranking()
    const chart = wrapper.getComponent(Bar)
    expect(chart.props('data').labels).toEqual(Array.from({ length: 10 }, (_, index) => `member-${11 - index}`))
    expect(chart.props('data').datasets[0].data).toEqual(Array.from({ length: 10 }, (_, index) => (11 - index) / 10))
    const options = chart.props('options') as ChartOptions<'bar'>
    expect(options.indexAxis).toBe('y')
    expect(options.plugins!.tooltip!.callbacks!.label!.call({} as never, { dataIndex: 0 } as TooltipItem<'bar'>)).toEqual(['enterprise.requests: 12', `enterprise.tokens: ${(1100).toLocaleString()}`, 'enterprise.cost: $1.1000'])
    await wrapper.get('button').trigger('click')
    expect(wrapper.findComponent(Bar).exists()).toBe(false)
    expect(JSON.parse(wrapper.get('[data-test="table"]').text())).toHaveLength(12)
  })
  it('renders empty and single rankings and updates axes for dark mode', async () => {
    expect(ranking([]).findComponent(Bar).exists()).toBe(false)
    const single = ranking([rankingRows[0]])
    expect(single.getComponent(Bar).props('data').datasets[0].data).toEqual([0])
    const before = single.getComponent(Bar).props('options').scales.x.ticks.color
    document.documentElement.classList.add('dark'); await flushPromises()
    expect(single.getComponent(Bar).props('options').scales.x.ticks.color).not.toBe(before)
  })
})

it('preserves all new DTO fields while adapting nested summaries for the shared personal table', () => {
  const row: EnterpriseUsageLog = {
    id: 1, model: 'gpt-5', created_at: '2026-10-18T00:00:00Z', request_id: 'request-1',
    user: { id: 2, username: 'Alice', email: 'alice@example.com' }, department_id: 3, department_name: 'Research',
    api_key: { id: 4, name: 'Project' }, group: { id: 5, name: 'Premium', platform: 'openai' },
    service_tier: 'priority', reasoning_effort: 'high', inbound_endpoint: '/v1/responses',
    input_tokens: 10, output_tokens: 20, cache_creation_tokens: 30, cache_creation_5m_tokens: 10, cache_creation_1h_tokens: 20, cache_read_tokens: 40,
    input_cost: 0.1, output_cost: 0.2, cache_creation_cost: 0.3, cache_read_cost: 0.4, total_cost: 1, actual_cost: 2, rate_multiplier: 2,
    long_context_billing_applied: true, billing_type: 0, billing_mode: 'image', request_type: 'ws_v2', stream: true, openai_ws_mode: true, native_compaction_v2: true,
    duration_ms: 2000, first_token_ms: 250, image_count: 2, image_size: '1024x1024', image_input_size: '512x512', image_output_size: '1024x1024', image_size_source: 'output', image_size_breakdown: { '1024x1024': 2 },
    image_input_tokens: 50, image_input_cost: 0.5, image_output_tokens: 60, image_output_cost: 0.6,
    media_type: 'image', cache_ttl_overridden: true, user_agent: 'client/1', ip_address: '127.0.0.1'
  }
  const result = normalizeEnterpriseUsage(row, [], 'No department')
  expect(result).toMatchObject(row)
  expect(result).toMatchObject({ member: 'Alice · alice@example.com', department_name: 'Research', user_id: 2, api_key_id: 4, group_id: 5 })
})
