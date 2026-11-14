import type {
  EnterpriseScope,
  MassEmailCampaign,
  MassEmailStatus,
  RecipientFilter
} from '@/api/admin/massEmails'

export interface RecipientFormState {
  includeAllUsers: boolean
  useUserFilter: boolean
  userStatuses: string[]
  userRoles: string[]
  groupIds: number[]
  userSearch: string
  userCreatedFrom: string | null
  userCreatedTo: string | null
  useEnterprise: boolean
  enterpriseIds: number[]
  enterpriseScope: EnterpriseScope
  useManual: boolean
  manualText: string
}

/**
 * Mirrors backend domain.massEmailAddressPattern / IsValidMassEmailAddress:
 * ASCII local part, dotted ASCII domain with 1-63 char labels (no leading/trailing '-').
 */
export const EMAIL_RE =
  /^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$/

/** Backend MassEmailEmailMaxLen (bytes). */
export const EMAIL_MAX_BYTES = 255

export function isValidMassEmailAddress(email: string): boolean {
  const trimmed = email.trim()
  if (!trimmed) return false
  if (new TextEncoder().encode(trimmed).length > EMAIL_MAX_BYTES) return false
  return EMAIL_RE.test(trimmed)
}

/** Separators of backend ParseMassEmailManualInput. */
const MANUAL_SEPARATORS = /[\n\r,;\t ，；、]+/

export function createEmptyRecipientForm(): RecipientFormState {
  return {
    includeAllUsers: false,
    useUserFilter: false,
    userStatuses: [],
    userRoles: [],
    groupIds: [],
    userSearch: '',
    userCreatedFrom: null,
    userCreatedTo: null,
    useEnterprise: false,
    enterpriseIds: [],
    enterpriseScope: 'both',
    useManual: false,
    manualText: ''
  }
}

/** Split pasted text (same separators as backend) into normalized valid emails and invalid items. */
export function parseManualEmails(text: string): { valid: string[]; invalid: string[] } {
  const seen = new Set<string>()
  const valid: string[] = []
  const invalid: string[] = []
  for (const raw of text.split(MANUAL_SEPARATORS)) {
    const item = raw.trim()
    if (!item) continue
    const lower = item.toLowerCase()
    if (!isValidMassEmailAddress(lower)) {
      invalid.push(item)
      continue
    }
    if (seen.has(lower)) continue
    seen.add(lower)
    valid.push(lower)
  }
  return { valid, invalid }
}

/** True when the user filter has at least one real condition (backend MassEmailUserFilter.IsEmpty is false). */
export function userFilterHasContent(f: {
  statuses?: string[] | null
  roles?: string[] | null
  group_ids?: number[] | null
  search?: string | null
  created_from?: string | null
  created_to?: string | null
} | null | undefined): boolean {
  if (!f) return false
  return (
    (f.statuses?.length ?? 0) > 0 ||
    (f.roles?.length ?? 0) > 0 ||
    (f.group_ids?.length ?? 0) > 0 ||
    (f.search ?? '').trim() !== '' ||
    !!f.created_from ||
    !!f.created_to
  )
}

/**
 * "Filter users" is ticked but no condition filled in. The backend treats an empty
 * user_filter as "no user source" (all users must use include_all_users), so block it.
 */
export function hasEmptyUserFilter(form: RecipientFormState): boolean {
  if (form.includeAllUsers || !form.useUserFilter) return false
  return !(
    form.userStatuses.length > 0 ||
    form.userRoles.length > 0 ||
    form.groupIds.length > 0 ||
    form.userSearch.trim() !== '' ||
    !!form.userCreatedFrom ||
    !!form.userCreatedTo
  )
}

export function buildRecipientFilter(form: RecipientFormState): RecipientFilter {
  const filter: RecipientFilter = {
    include_all_users: form.includeAllUsers,
    user_filter: null,
    enterprise_filter: null,
    manual_emails: []
  }
  if (!form.includeAllUsers && form.useUserFilter && !hasEmptyUserFilter(form)) {
    filter.user_filter = {
      statuses: [...form.userStatuses],
      roles: [...form.userRoles],
      group_ids: [...form.groupIds],
      search: form.userSearch.trim(),
      ...(form.userCreatedFrom ? { created_from: form.userCreatedFrom } : {}),
      ...(form.userCreatedTo ? { created_to: form.userCreatedTo } : {})
    }
  }
  if (form.useEnterprise && form.enterpriseIds.length > 0) {
    filter.enterprise_filter = {
      enterprise_ids: [...form.enterpriseIds],
      scope: form.enterpriseScope
    }
  }
  if (form.useManual) {
    filter.manual_emails = parseManualEmails(form.manualText).valid
  }
  return filter
}

/** True when the filter selects at least one recipient source. */
export function hasAnyRecipientSource(filter: RecipientFilter): boolean {
  return (
    filter.include_all_users ||
    userFilterHasContent(filter.user_filter) ||
    (filter.enterprise_filter?.enterprise_ids.length ?? 0) > 0 ||
    (filter.manual_emails?.length ?? 0) > 0
  )
}

export function recipientFormFromFilter(filter: RecipientFilter | null | undefined): RecipientFormState {
  const form = createEmptyRecipientForm()
  if (!filter) return form
  form.includeAllUsers = !!filter.include_all_users
  if (userFilterHasContent(filter.user_filter) && filter.user_filter) {
    form.useUserFilter = true
    form.userStatuses = [...(filter.user_filter.statuses ?? [])]
    form.userRoles = [...(filter.user_filter.roles ?? [])]
    form.groupIds = [...(filter.user_filter.group_ids ?? [])]
    form.userSearch = filter.user_filter.search ?? ''
    form.userCreatedFrom = filter.user_filter.created_from ?? null
    form.userCreatedTo = filter.user_filter.created_to ?? null
  }
  if (filter.enterprise_filter && filter.enterprise_filter.enterprise_ids?.length) {
    form.useEnterprise = true
    form.enterpriseIds = [...filter.enterprise_filter.enterprise_ids]
    form.enterpriseScope = filter.enterprise_filter.scope || 'both'
  }
  if (filter.manual_emails?.length) {
    form.useManual = true
    form.manualText = filter.manual_emails.join('\n')
  }
  return form
}

export function progressPercent(c: Pick<MassEmailCampaign, 'sent_count' | 'failed_count' | 'total_count'>): number {
  if (!c.total_count) return 0
  return Math.min(100, Math.round(((c.sent_count + c.failed_count) / c.total_count) * 100))
}

export function openRatePercent(rate: number | null | undefined): string {
  return `${((rate ?? 0) * 100).toFixed(1)}%`
}

export function isEditableStatus(status: MassEmailStatus): boolean {
  return status === 'draft' || status === 'scheduled'
}

export function canCancelStatus(status: MassEmailStatus): boolean {
  return status === 'draft' || status === 'scheduled' || status === 'sending'
}

export function canSendStatus(status: MassEmailStatus): boolean {
  return status === 'draft' || status === 'scheduled' || status === 'failed'
}

export const MASS_EMAIL_VARIABLES = ['name', 'email', 'enterprise_name', 'site_name'] as const

/** Map a recipient tab to API filter params. */
export type RecipientTab = 'all' | 'opened' | 'unopened' | 'failed'
export function tabToFilters(tab: RecipientTab): { status?: string; opened?: 'true' | 'false' } {
  switch (tab) {
    case 'opened':
      return { opened: 'true' }
    case 'unopened':
      return { opened: 'false', status: 'sent' }
    case 'failed':
      return { status: 'failed' }
    default:
      return {}
  }
}
