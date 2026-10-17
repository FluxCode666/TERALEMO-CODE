import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { BasicPlatform, Chart as ChartJS, type ChartData, type ChartOptions, type TooltipItem } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseUsageTrend } from '@/types/enterprise'
import { formatDateTime } from '@/utils/format'
import EnterpriseUsagePanel from '../components/EnterpriseUsagePanel.vue'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('vue-chartjs', async importOriginal => ({
  ...await importOriginal<typeof import('vue-chartjs')>(),
  Line: { name: 'Line', props: ['data', 'options', 'height'], template: '<canvas />' }
}))

const wrappers: VueWrapper[] = []
const charts: ChartJS[] = []
const singleDay: EnterpriseUsageTrend = { date: '2026-10-18', requests: 4, total_tokens: 6000, actual_cost: 0.08 }
const createdAt = '2026-10-18T00:15:27.98669+08:00'

async function mountUsage(trend: EnterpriseUsageTrend[]) {
  const api = {
    departments: vi.fn().mockResolvedValue([]),
    usage: vi.fn().mockResolvedValue({
      items: [{ id: 1, model: 'gpt-5.4', created_at: createdAt, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 0, cache_read_tokens: 0, actual_cost: 0.02 }],
      total: 1, page: 1, page_size: 20, pages: 1
    }),
    usageStats: vi.fn().mockResolvedValue({ total_requests: 4, total_tokens: 6000, total_actual_cost: 0.08 }),
    usageTrend: vi.fn().mockResolvedValue(trend),
    usageModels: vi.fn().mockResolvedValue([])
  }
  const wrapper = mount(EnterpriseUsagePanel, {
    props: { api: api as unknown as EnterpriseResourceAPI },
    global: { stubs: { Pagination: true } }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, api }
}

// jsdom has no canvas renderer. Keep Chart.js's real scales, controllers and
// drawing lifecycle; only replace the browser's 2D drawing surface.
function renderChart(wrapper: VueWrapper) {
  const line = wrapper.getComponent(Line)
  const canvas = document.createElement('canvas')
  canvas.width = 640
  canvas.height = 256
  const context = new Proxy({
    canvas,
    measureText: (text: string) => ({ width: String(text).length * 7 }),
    getLineDash: () => []
  }, {
    get(target, key) { return Reflect.has(target, key) ? Reflect.get(target, key) : () => undefined }
  }) as unknown as CanvasRenderingContext2D
  vi.spyOn(canvas, 'getContext').mockReturnValue(context)
  const chart = new ChartJS(canvas, {
    type: 'line',
    data: line.props('data') as ChartData<'line'>,
    options: { ...line.props('options') as ChartOptions<'line'>, responsive: false, animation: false, devicePixelRatio: 1 },
    platform: BasicPlatform
  })
  charts.push(chart)
  return chart
}

afterEach(() => {
  for (const chart of charts.splice(0)) chart.destroy()
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  vi.restoreAllMocks()
})

describe('enterprise usage display', () => {
  it('renders the backend single-day cost as a visible point inside the plot', async () => {
    const { wrapper, api } = await mountUsage([singleDay])
    expect(api.usageTrend).toHaveBeenCalledWith({ granularity: 'day' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const line = wrapper.getComponent(Line)
    expect(line.props('data')).toMatchObject({ labels: ['2026-10-18'], datasets: [{ data: [0.08] }] })
    expect(line.props('height')).toBe(256)
    expect(line.element.parentElement?.classList.contains('h-64')).toBe(true)
    expect(line.element.parentElement?.classList.contains('relative')).toBe(true)

    const chart = renderChart(wrapper)
    const point = chart.getDatasetMeta(0).data[0]
    expect(point).toMatchObject({ skip: false, options: { radius: 5 } })
    expect(point.x).toBeGreaterThan(chart.chartArea.left + 5)
    expect(point.x).toBeLessThan(chart.chartArea.right - 5)
    expect(point.y).toBeGreaterThan(chart.chartArea.top + 5)
    expect(point.y).toBeLessThan(chart.chartArea.bottom - 5)

    const callbacks = (line.props('options') as ChartOptions<'line'>).plugins!.tooltip!.callbacks!
    const item = { dataIndex: 0, dataset: { label: 'enterprise.cost' }, parsed: { x: 0, y: 0.08 } } as TooltipItem<'line'>
    expect(callbacks.label!.call({} as never, item)).toBe('enterprise.cost: $0.0800')
    expect(callbacks.afterBody!.call({} as never, [item])).toEqual(['enterprise.requests: 4', `enterprise.tokens: ${(6000).toLocaleString()}`])
  })

  it('renders multiple daily cost points with the original date order and connected line', async () => {
    const trend = [singleDay, { ...singleDay, date: '2026-10-19', actual_cost: 0.03 }, { ...singleDay, date: '2026-10-20', actual_cost: 0.12 }]
    const { wrapper } = await mountUsage(trend)
    const chart = renderChart(wrapper)
    expect(chart.data.labels).toEqual(trend.map(point => point.date))
    expect(chart.data.datasets[0].data).toEqual([0.08, 0.03, 0.12])
    const meta = chart.getDatasetMeta(0)
    expect(meta.data).toHaveLength(3)
    meta.data.forEach((point, index) => {
      expect(point).toMatchObject({ skip: false, options: { radius: 3 } })
      expect(Number.isFinite(point.x) && Number.isFinite(point.y)).toBe(true)
      if (index) expect(point.x).toBeGreaterThan(meta.data[index - 1].x)
    })
    expect(meta.dataset).toMatchObject({ segments: [{ start: 0, end: 2, loop: false }] })
  })

  it('keeps the empty state when there are no trend buckets', async () => {
    const { wrapper } = await mountUsage([])
    expect(wrapper.findComponent(Line).exists()).toBe(false)
    expect(wrapper.text()).toContain('enterprise.empty')
  })

  it('formats usage creation times with the shared formatter and renders stat icons', async () => {
    const { wrapper } = await mountUsage([])
    expect(wrapper.text()).toContain(formatDateTime(createdAt))
    expect(wrapper.text()).not.toContain(createdAt)
    expect(wrapper.findAll('.stat-icon svg')).toHaveLength(3)
    expect(wrapper.find('.stat-icon-warning svg').exists()).toBe(true)
    expect(wrapper.find('.stat-icon-success svg').exists()).toBe(true)
  })
})
