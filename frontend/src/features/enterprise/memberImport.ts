import * as XLSX from 'xlsx'
import type { EnterpriseImportRow, EnterpriseRole } from '@/types/enterprise'
export const IMPORT_HEADERS = ['邮箱', '用户名', '部门路径', '角色', '职位', '初始密码'] as const
export const IMPORT_MAX_ROWS = 1000
export const IMPORT_MAX_BYTES = 5 * 1024 * 1024
const englishHeaders = ['email', 'username', 'department_path', 'role', 'title', 'password']
const roles: Record<string, EnterpriseRole> = { owner: 'owner', manager: 'manager', member: 'member', 企业负责人: 'owner', 部门负责人: 'manager', 员工: 'member' }
export class MemberImportError extends Error {
  constructor(public key: string, public row?: number) { super(key) }
}
export function parseMemberWorkbook(data: ArrayBuffer): EnterpriseImportRow[] {
  const workbook = XLSX.read(data, { type: 'array', codepage: 65001, raw: true })
  const sheet = workbook.Sheets[workbook.SheetNames[0]!]
  if (!sheet) throw new MemberImportError('headersError')
  const grid = XLSX.utils.sheet_to_json<unknown[]>(sheet, { header: 1, defval: '', blankrows: true, raw: false })
  const first = grid.findIndex(row => row.some(value => String(value).trim()))
  if (first < 0) throw new MemberImportError('emptyFile')
  const headers = grid[first]!.map(value => String(value).trim().replace(/^\uFEFF/, ''))
  const indices = IMPORT_HEADERS.map((header, index) => headers.findIndex(value => value === header || value.toLowerCase() === englishHeaders[index]))
  if (indices.some(index => index < 0) || new Set(indices).size !== IMPORT_HEADERS.length) throw new MemberImportError('headersError')
  const rows: EnterpriseImportRow[] = []
  for (let index = first + 1; index < grid.length; index++) {
    const values = indices.map(column => String(grid[index]?.[column] ?? '').trim())
    if (values.every(value => !value)) continue
    if (rows.length >= IMPORT_MAX_ROWS) throw new MemberImportError('tooManyRows')
    const [email = '', username = '', department_path = '', roleText = '', title = '', password = ''] = values
    const role = roles[roleText.toLowerCase()]
    if (!role) throw new MemberImportError('roleError', index + 1)
    rows.push({ row: index + 1, email, username, department_path, role, title, ...(password ? { password } : {}) })
  }
  if (!rows.length) throw new MemberImportError('emptyFile')
  return rows
}
export function validateImportFile(file: Pick<File, 'size' | 'name'>) {
  if (!/\.(xlsx|csv)$/i.test(file.name)) throw new MemberImportError('fileTypeError')
  if (file.size > IMPORT_MAX_BYTES) throw new MemberImportError('fileSizeError')
}
export function createMemberTemplate(notes: string[], sheetName: string, notesName: string): XLSX.WorkBook {
  const workbook = XLSX.utils.book_new()
  const sheet = XLSX.utils.aoa_to_sheet([[...IMPORT_HEADERS]])
  sheet['!cols'] = [{ wch: 30 }, { wch: 20 }, { wch: 40 }, { wch: 20 }, { wch: 20 }, { wch: 24 }]
  XLSX.utils.book_append_sheet(workbook, sheet, sheetName)
  XLSX.utils.book_append_sheet(workbook, XLSX.utils.aoa_to_sheet(notes.map(note => [note])), notesName)
  return workbook
}
export function downloadMemberTemplate(format: 'xlsx' | 'csv', notes: string[], sheetName: string, notesName: string) {
  const workbook = createMemberTemplate(notes, sheetName, notesName)
  if (format === 'xlsx') XLSX.writeFile(workbook, 'enterprise-members-template.xlsx')
  else downloadCsv('enterprise-members-template.csv', [[...IMPORT_HEADERS]])
}
export function downloadCsv(name: string, rows: (string | number)[][]) {
  const escape = (value: string | number) => {
    const text = String(value)
    return `"${(/^[=+\-@\t\r]/.test(text) ? `'${text}` : text).replace(/"/g, '""')}"`
  }
  const blob = new Blob(['\uFEFF' + rows.map(row => row.map(escape).join(',')).join('\r\n')], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob), anchor = document.createElement('a')
  anchor.href = url; anchor.download = name; anchor.click(); URL.revokeObjectURL(url)
}
