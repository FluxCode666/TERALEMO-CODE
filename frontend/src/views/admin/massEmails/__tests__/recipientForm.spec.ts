import { describe, expect, it } from 'vitest'
import {
  buildRecipientFilter,
  canCancelStatus,
  canSendStatus,
  createEmptyRecipientForm,
  hasAnyRecipientSource,
  hasEmptyUserFilter,
  isValidMassEmailAddress,
  isEditableStatus,
  parseManualEmails,
  progressPercent,
  recipientFormFromFilter,
  tabToFilters
} from '../recipientForm'

describe('parseManualEmails', () => {
  it('splits by newline/comma, lowercases, dedupes and separates invalid', () => {
    const { valid, invalid } = parseManualEmails('A@x.com,\nb@y.org; a@X.com\nbad-email, c@z.io')
    expect(valid).toEqual(['a@x.com', 'b@y.org', 'c@z.io'])
    expect(invalid).toEqual(['bad-email'])
  })
})

describe('buildRecipientFilter', () => {
  it('is empty by default', () => {
    expect(hasAnyRecipientSource(buildRecipientFilter(createEmptyRecipientForm()))).toBe(false)
  })

  it('combines all four sources', () => {
    const form = createEmptyRecipientForm()
    form.includeAllUsers = true
    form.useEnterprise = true
    form.enterpriseIds = [4, 5]
    form.enterpriseScope = 'contacts'
    form.useManual = true
    form.manualText = 'Boss@Example.com'
    const f = buildRecipientFilter(form)
    expect(f.include_all_users).toBe(true)
    expect(f.user_filter).toBeNull()
    expect(f.enterprise_filter).toEqual({ enterprise_ids: [4, 5], scope: 'contacts' })
    expect(f.manual_emails).toEqual(['boss@example.com'])
  })

  it('ignores user filter when all users is selected, keeps it otherwise', () => {
    const form = createEmptyRecipientForm()
    form.useUserFilter = true
    form.userStatuses = ['active']
    form.groupIds = [3]
    expect(buildRecipientFilter(form).user_filter).toMatchObject({ statuses: ['active'], group_ids: [3] })
    form.includeAllUsers = true
    expect(buildRecipientFilter(form).user_filter).toBeNull()
  })

  it('round-trips through recipientFormFromFilter', () => {
    const form = createEmptyRecipientForm()
    form.useUserFilter = true
    form.userRoles = ['user']
    form.useEnterprise = true
    form.enterpriseIds = [1]
    form.useManual = true
    form.manualText = 'a@b.co'
    const back = recipientFormFromFilter(buildRecipientFilter(form))
    expect(buildRecipientFilter(back)).toEqual(buildRecipientFilter(form))
  })

  it('preserves the date conditions of an existing campaign when editing', () => {
    const filter = {
      include_all_users: false,
      user_filter: {
        statuses: ['active'], roles: ['user'], group_ids: [3], search: 'alice',
        created_from: '2025-01-01T00:00:00Z', created_to: '2026-01-01T00:00:00Z'
      },
      enterprise_filter: null,
      manual_emails: []
    }
    expect(buildRecipientFilter(recipientFormFromFilter(filter))).toEqual(filter)
  })

  it('combines filtered users, enterprise members and manual addresses', () => {
    const form = createEmptyRecipientForm()
    Object.assign(form, {
      useUserFilter: true, userStatuses: ['active'], userRoles: ['user'], groupIds: [3],
      useEnterprise: true, enterpriseIds: [5], enterpriseScope: 'members',
      useManual: true, manualText: 'Boss@Example.com\nboss@example.com'
    })
    expect(buildRecipientFilter(form)).toEqual({
      include_all_users: false,
      user_filter: { statuses: ['active'], roles: ['user'], group_ids: [3], search: '' },
      enterprise_filter: { enterprise_ids: [5], scope: 'members' },
      manual_emails: ['boss@example.com']
    })
  })
})

describe('status helpers', () => {
  it('computes progress', () => {
    expect(progressPercent({ sent_count: 5, failed_count: 5, total_count: 20 })).toBe(50)
    expect(progressPercent({ sent_count: 0, failed_count: 0, total_count: 0 })).toBe(0)
  })
  it('gates actions by status', () => {
    expect(isEditableStatus('draft')).toBe(true)
    expect(isEditableStatus('sending')).toBe(false)
    expect(canCancelStatus('sending')).toBe(true)
    expect(canCancelStatus('completed')).toBe(false)
    expect(canSendStatus('failed')).toBe(true)
    expect(canSendStatus('cancelled')).toBe(false)
  })
  it('maps recipient tabs to API filters', () => {
    expect(tabToFilters('opened')).toEqual({ opened: 'true' })
    expect(tabToFilters('unopened')).toEqual({ opened: 'false', status: 'sent' })
    expect(tabToFilters('failed')).toEqual({ status: 'failed' })
    expect(tabToFilters('all')).toEqual({})
  })
})

describe('backend-aligned email validation', () => {
  it('rejects what backend IsValidMassEmailAddress rejects', () => {
    for (const bad of ['a@-example.com', 'a@under_score.example', 'a@例子.测试', 'a@b', 'a@example-.com', `${'a'.repeat(251)}@x.co`]) {
      expect(isValidMassEmailAddress(bad), bad).toBe(false)
    }
    expect(isValidMassEmailAddress('a.b+c@sub.example.com')).toBe(true)
  })

  it('splits by backend separators including 、 ， ； and tab', () => {
    const { valid, invalid } = parseManualEmails('a@x.com、b@y.org，c@z.io；d@w.net\te@v.cc a@-bad.com')
    expect(valid).toEqual(['a@x.com', 'b@y.org', 'c@z.io', 'd@w.net', 'e@v.cc'])
    expect(invalid).toEqual(['a@-bad.com'])
  })

  it('keeps invalid items out of the submitted filter', () => {
    const form = createEmptyRecipientForm()
    form.useManual = true
    form.manualText = 'ok@x.com\na@under_score.example'
    expect(buildRecipientFilter(form).manual_emails).toEqual(['ok@x.com'])
  })
})

describe('real backend JSON (user_filter always an object)', () => {
  const manualOnly = {
    include_all_users: false,
    user_filter: {},
    enterprise_filter: { enterprise_ids: [], scope: '' },
    manual_emails: ['a@x.com']
  } as never
  const enterpriseOnly = {
    include_all_users: false,
    user_filter: {},
    enterprise_filter: { enterprise_ids: [4], scope: 'contacts' },
    manual_emails: []
  } as never

  it('does not enable the user filter for manual-only jobs and saves back unchanged', () => {
    const form = recipientFormFromFilter(manualOnly)
    expect(form.useUserFilter).toBe(false)
    expect(form.useManual).toBe(true)
    const f = buildRecipientFilter(form)
    expect(f.user_filter).toBeNull()
    expect(f.enterprise_filter).toBeNull()
    expect(f.manual_emails).toEqual(['a@x.com'])
  })

  it('does not enable the user filter for enterprise-only jobs', () => {
    const form = recipientFormFromFilter(enterpriseOnly)
    expect(form.useUserFilter).toBe(false)
    const f = buildRecipientFilter(form)
    expect(f.user_filter).toBeNull()
    expect(f.enterprise_filter).toEqual({ enterprise_ids: [4], scope: 'contacts' })
  })

  it('treats an empty user_filter alone as no source', () => {
    expect(hasAnyRecipientSource({ include_all_users: false, user_filter: {}, manual_emails: [] })).toBe(false)
  })

  it('backfills real user conditions', () => {
    const form = recipientFormFromFilter({ include_all_users: false, user_filter: { statuses: ['active'] }, manual_emails: [] })
    expect(form.useUserFilter).toBe(true)
    expect(form.userStatuses).toEqual(['active'])
  })
})

describe('empty user filter guard', () => {
  it('flags ticked-but-empty filter and never submits it', () => {
    const form = createEmptyRecipientForm()
    form.useUserFilter = true
    expect(hasEmptyUserFilter(form)).toBe(true)
    expect(buildRecipientFilter(form).user_filter).toBeNull()
    form.userRoles = ['user']
    expect(hasEmptyUserFilter(form)).toBe(false)
    form.userRoles = []
    form.includeAllUsers = true
    expect(hasEmptyUserFilter(form)).toBe(false)
  })
})
