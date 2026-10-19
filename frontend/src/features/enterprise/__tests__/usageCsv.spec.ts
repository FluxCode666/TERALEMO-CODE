import { describe, expect, it } from 'vitest'
import { buildEnterpriseUsageCsv, enterpriseUsageCsvFilename, escapeCsvCell } from '../usageCsv'
import { formatDateTime } from '@/utils/format'
import type { EnterpriseDepartment, EnterpriseUsageLog } from '@/types/enterprise'
const t = (key: string) => key
const source: EnterpriseUsageLog = { id: 1, model: '=1+1', created_at: '2026-10-18T04:30:00Z', user: { id: 1, username: 'Alice,"A"\nTeam', email: 'alice@example.com' }, department_id: 3, api_key: { id: 1, name: '@key' }, input_tokens: 123456789, output_tokens: 2, actual_cost: 0.123456789012345, total_cost: 0.234567890123456, first_token_ms: null, duration_ms: 1234 }
describe('enterprise CSV', () => {
  it.each(['=SUM(A1)', '+cmd', '-cmd', '@cmd', '\tcmd', '\rcmd'])('防公式注入 %j', text => {
    expect(escapeCsvCell(text)).toBe(text.includes('\r') ? `"'${text}"` : `'${text}`)
  })
  it('转义引号逗号换行，保留数字精度及负数', () => {
    expect(escapeCsvCell('a,"b"\nc')).toBe('"a,""b""\nc"')
    expect(escapeCsvCell(-0.123456789012345)).toBe('-0.123456789012345')
    expect(escapeCsvCell(null)).toBe('')
  })
  it('BOM、本地时间、表格复合列展开，隐藏列不导出', () => {
    const csv = buildEnterpriseUsageCsv([source], ['created_at', 'member', 'department_name', 'api_key', 'model', 'tokens', 'cost', 'latency'].map(key => ({ key, label: key })), [{ id: 3, name: 'R&D' }] as EnterpriseDepartment[], t)
    expect(csv.charCodeAt(0)).toBe(0xfeff)
    expect(csv).toContain(formatDateTime(source.created_at))
    expect(csv).toContain('"Alice,""A""\nTeam · alice@example.com"')
    expect(csv).toContain("R&D,'@key,'=1+1,123456789,2")
    expect(csv).toContain('0.234567890123456,1,0.123456789012345,,1234')
    expect(csv).not.toContain('request_id')
    expect(csv).toContain('enterprise.usageAnalysis.csv.cache1h')
    expect(csv).toContain(String(source.output_tokens! * 1000 / source.duration_ms!))
    expect(buildEnterpriseUsageCsv([source], [{ key: 'created_at', label: 'time' }], [], t)).not.toContain('0.123456789012345')
  })
  it('安全文件名包含企业和查询时间，日期与滚动窗都支持', () => {
    const name = enterpriseUsageCsvFilename('Acme/研发:*?\n', { start_date: '2026-10-01', end_date: '2026-10-18' })
    expect(name).toBe('Acme_研发_____2026-10-01_2026-10-18.csv')
    const rolling = enterpriseUsageCsvFilename('Acme', { start_time: source.created_at, end_time: '2026-10-19T04:30:00Z' })
    expect(rolling).toContain('2026-10-18')
    expect(rolling).toContain('2026-10-19')
    expect(rolling).not.toMatch(/[<>:"/\\|?*\r\n]/)
  })
})
