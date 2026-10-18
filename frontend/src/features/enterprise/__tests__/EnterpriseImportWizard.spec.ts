import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import * as XLSX from 'xlsx'
import type { EnterpriseImportPreview, EnterpriseImportResult } from '@/types/enterprise'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import { createMemberTemplate, IMPORT_HEADERS, parseMemberWorkbook, validateImportFile } from '../memberImport'
import * as memberImport from '../memberImport'
import EnterpriseImportWizard from '../components/EnterpriseImportWizard.vue'
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })
function data(rows: unknown[][], format: 'xlsx' | 'csv' = 'xlsx'): ArrayBuffer {
  const wb = XLSX.utils.book_new(); XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(rows), 'Members')
  return format === 'xlsx' ? XLSX.write(wb, { type: 'array', bookType: 'xlsx' }) : new TextEncoder().encode('\uFEFF' + XLSX.utils.sheet_to_csv(wb.Sheets.Members!)).buffer as ArrayBuffer
}
const source = [[...IMPORT_HEADERS], [], ['alice@example.com', 'Alice', '研发中心/后端组', '员工', '工程师', '']]
const payload = { rows: [{ row: 3, email: 'alice@example.com', username: 'Alice', department_path: '研发中心/后端组', role: 'member', title: '工程师' }] }
const preview: EnterpriseImportPreview = { summary: { total: 1, create: 1, update: 0, skip: 0, error: 0 }, departments_to_create: ['研发中心', '研发中心/后端组'], rows: [{ row: 3, status: 'create', email: 'alice@example.com', department_path: '研发中心/后端组', role: 'member', messages: [] }] }
function render(result?: EnterpriseImportResult, response = preview) {
  const api = { previewImport: vi.fn().mockResolvedValue(response), commitImport: vi.fn().mockResolvedValue(result) }
  const wrapper = mount(EnterpriseImportWizard, { props: { api: api as unknown as EnterpriseResourceAPI }, global: { stubs: { EnterpriseDialog: { template: '<div><slot /></div>' } } } }); wrappers.push(wrapper)
  return { wrapper, api }
}
async function upload(wrapper: VueWrapper) {
  await wrapper.get('[data-test="import-next"]').trigger('click')
  const file = new File(['test'], 'members.xlsx'); Object.defineProperty(file, 'arrayBuffer', { value: () => Promise.resolve(data(source)) })
  const input = wrapper.get('input[type="file"]'); Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
  await input.trigger('change'); await flushPromises()
  await wrapper.get('[data-test="import-preview"]').trigger('click'); await flushPromises()
}
describe('member spreadsheet parsing and template', () => {
  it('exports the exact six template headers with a separate instructions sheet', () => {
    const workbook = createMemberTemplate(['说明'], '成员', '说明')
    expect(XLSX.utils.sheet_to_json(workbook.Sheets['成员']!, { header: 1 })[0]).toEqual([...IMPORT_HEADERS])
    expect(workbook.SheetNames).toEqual(['成员', '说明'])
  })
  it.each(['xlsx', 'csv'] as const)('parses %s, omits blank rows and passwords, and normalizes Chinese roles', format => {
    expect(parseMemberWorkbook(data(source, format))).toEqual(payload.rows)
    expect(parseMemberWorkbook(data([['email', 'username', 'department_path', 'role', 'title', 'password'], ['a@example.com', '', '', 'manager', '', 'secret']]))[0]).toMatchObject({ role: 'manager', password: 'secret', row: 2 })
  })
  it('rejects missing headers, invalid roles, oversize files and more than 1000 rows', () => {
    expect(() => parseMemberWorkbook(data([['邮箱']]))).toThrow('headersError')
    expect(() => parseMemberWorkbook(data([[...IMPORT_HEADERS], ['a@example.com', '', '', 'admin']]))).toThrow('roleError')
    expect(() => validateImportFile({ name: 'members.txt', size: 2 })).toThrow('fileTypeError')
    expect(() => validateImportFile({ name: 'members.xlsx', size: 5 * 1024 * 1024 + 1 })).toThrow('fileSizeError')
    const rows = Array.from({ length: 1000 }, () => ['a@example.com', '', '', 'member'])
    expect(parseMemberWorkbook(data([[...IMPORT_HEADERS], ...rows]))).toHaveLength(1000)
    expect(() => parseMemberWorkbook(data([[...IMPORT_HEADERS], ...rows, rows[0]!]))).toThrow('tooManyRows')
  })
})
describe('import wizard contract and one-time passwords', () => {
  it.each([true, false])('previews and commits the same rows, and offers passwords only when returned (%s)', async withPassword => {
    const result: EnterpriseImportResult = { summary: { total: 1, created: 1, updated: 0, skipped: 0, failed: 0 }, rows: [{ row: 3, status: 'created', ...(withPassword ? { generated_password: 'Generated123' } : {}) }] }
    const { wrapper, api } = render(result); const download = vi.spyOn(memberImport, 'downloadCsv').mockImplementation(() => {})
    await upload(wrapper)
    expect(api.previewImport).toHaveBeenCalledWith(payload)
    expect(wrapper.text()).toContain('研发中心/后端组')
    expect(api.commitImport).not.toHaveBeenCalled()
    await wrapper.get('[data-test="import-commit"]').trigger('click'); await flushPromises()
    expect(api.commitImport).toHaveBeenCalledWith(payload)
    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.find('[data-test="import-passwords"]').exists()).toBe(withPassword)
    expect(wrapper.text()).not.toContain('Generated123')
    if (withPassword) {
      await wrapper.get('[data-test="import-passwords"]').trigger('click')
      expect(download).toHaveBeenCalledWith('enterprise-initial-passwords.csv', expect.arrayContaining([['alice@example.com', 'Generated123']]))
      expect(wrapper.text()).toContain('enterprise.importUi.passwordWarning')
    }
  })
  it('blocks commits with preview errors and exports an error report without credentials', async () => {
    const response: EnterpriseImportPreview = { ...preview, summary: { ...preview.summary, error: 1, create: 0 }, rows: [{ ...preview.rows[0]!, status: 'error', messages: ['邮箱已被占用'] }] }
    const { wrapper, api } = render(undefined, response); const download = vi.spyOn(memberImport, 'downloadCsv').mockImplementation(() => {})
    await upload(wrapper)
    expect(wrapper.get('[data-test="import-commit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="import-errors"]').trigger('click')
    expect(download).toHaveBeenCalledWith('enterprise-import-errors.csv', expect.arrayContaining([[3, 'alice@example.com', '研发中心/后端组', '邮箱已被占用']]))
    expect(api.commitImport).not.toHaveBeenCalled()
  })
  it('shows API failures and supports retrying the preview', async () => {
    const { wrapper, api } = render(); api.previewImport.mockRejectedValueOnce({ code: 'ENTERPRISE_PERMISSION_DENIED' })
    await upload(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('enterprise.errors.ENTERPRISE_PERMISSION_DENIED')
    await wrapper.get('[data-test="import-preview"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="import-commit"]').exists()).toBe(true)
  })
})
