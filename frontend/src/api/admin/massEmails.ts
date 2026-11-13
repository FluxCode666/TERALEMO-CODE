/**
 * Admin Mass Email API endpoints
 */

import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'

export type MassEmailStatus = 'draft' | 'scheduled' | 'sending' | 'completed' | 'cancelled' | 'failed'
export type MassEmailRecipientStatus = 'pending' | 'sent' | 'failed'
export type EnterpriseScope = 'contacts' | 'members' | 'both'

export interface UserFilter {
  statuses?: string[]
  roles?: string[]
  group_ids?: number[]
  search?: string
  created_from?: string | null
  created_to?: string | null
}

export interface EnterpriseFilter {
  enterprise_ids: number[]
  scope: EnterpriseScope
}

export interface RecipientFilter {
  include_all_users: boolean
  user_filter?: UserFilter | null
  enterprise_filter?: EnterpriseFilter | null
  manual_emails?: string[]
}

export interface MassEmailCampaign {
  id: number
  subject: string
  body_html: string
  recipient_filter: RecipientFilter
  status: MassEmailStatus
  scheduled_at: string | null
  total_count: number
  sent_count: number
  failed_count: number
  opened_count: number
  open_rate: number
  created_by: number
  created_by_username?: string
  started_at: string | null
  finished_at: string | null
  last_error: string
  created_at: string
  updated_at: string
}

export interface MassEmailStats {
  total: number
  pending: number
  sent: number
  failed: number
  opened: number
  open_rate: number
}

export interface MassEmailDetail {
  campaign: MassEmailCampaign
  stats: MassEmailStats
}

export interface MassEmailRecipient {
  id: number
  campaign_id: number
  email: string
  name: string
  user_id: number | null
  enterprise_id: number | null
  status: MassEmailRecipientStatus
  error: string
  attempts: number
  sent_at: string | null
  first_opened_at: string | null
  last_opened_at: string | null
  open_count: number
  created_at: string
}

export interface PreviewSample {
  email: string
  name: string
  user_id: number | null
  enterprise_id: number | null
  source: 'user' | 'enterprise_contact' | 'enterprise_member' | 'manual'
}

export interface PreviewRecipientsResult {
  total: number
  sample: PreviewSample[]
  by_source: {
    users: number
    enterprise_contacts: number
    enterprise_members: number
    manual: number
  }
  invalid_emails: string[]
}

export interface SaveMassEmailRequest {
  subject: string
  body_html: string
  recipient_filter: RecipientFilter
  scheduled_at?: string | null
}

export interface CreateMassEmailRequest extends SaveMassEmailRequest {
  send_now?: boolean
}

export interface ListMassEmailsFilters {
  status?: string
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface ListRecipientsFilters {
  status?: string
  opened?: 'true' | 'false' | ''
  search?: string
}

const base = '/admin/mass-emails'

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: ListMassEmailsFilters,
  options?: { signal?: AbortSignal }
): Promise<BasePaginationResponse<MassEmailCampaign>> {
  const { data } = await apiClient.get<BasePaginationResponse<MassEmailCampaign>>(base, {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

export async function create(request: CreateMassEmailRequest): Promise<MassEmailCampaign> {
  const { data } = await apiClient.post<MassEmailCampaign>(base, request)
  return data
}

export async function getById(id: number, options?: { signal?: AbortSignal }): Promise<MassEmailDetail> {
  const { data } = await apiClient.get<MassEmailDetail>(`${base}/${id}`, { signal: options?.signal })
  return data
}

export async function update(id: number, request: SaveMassEmailRequest): Promise<MassEmailCampaign> {
  const { data } = await apiClient.put<MassEmailCampaign>(`${base}/${id}`, request)
  return data
}

export async function remove(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`${base}/${id}`)
  return data
}

export async function previewRecipients(
  recipientFilter: RecipientFilter,
  sampleSize: number = 5,
  options?: { signal?: AbortSignal }
): Promise<PreviewRecipientsResult> {
  const { data } = await apiClient.post<PreviewRecipientsResult>(`${base}/preview-recipients`, {
    recipient_filter: recipientFilter,
    sample_size: sampleSize
  }, options)
  return data
}

export async function sendTest(request: {
  subject: string
  body_html: string
  to?: string | null
}): Promise<{ sent: boolean; to: string }> {
  const { data } = await apiClient.post<{ sent: boolean; to: string }>(`${base}/test`, request)
  return data
}

export async function send(id: number): Promise<{ campaign: MassEmailCampaign }> {
  const { data } = await apiClient.post<{ campaign: MassEmailCampaign }>(`${base}/${id}/send`)
  return data
}

export async function cancel(id: number): Promise<{ campaign: MassEmailCampaign }> {
  const { data } = await apiClient.post<{ campaign: MassEmailCampaign }>(`${base}/${id}/cancel`)
  return data
}

export async function retryFailed(
  id: number
): Promise<{ reset: number; campaign: MassEmailCampaign }> {
  const { data } = await apiClient.post<{ reset: number; campaign: MassEmailCampaign }>(
    `${base}/${id}/retry-failed`
  )
  return data
}

export async function retryRecipient(
  id: number,
  recipientId: number
): Promise<{ recipient: MassEmailRecipient }> {
  const { data } = await apiClient.post<{ recipient: MassEmailRecipient }>(
    `${base}/${id}/recipients/${recipientId}/retry`
  )
  return data
}

export async function listRecipients(
  id: number,
  page: number = 1,
  pageSize: number = 50,
  filters?: ListRecipientsFilters,
  options?: { signal?: AbortSignal }
): Promise<BasePaginationResponse<MassEmailRecipient>> {
  const params: Record<string, unknown> = { page, page_size: pageSize }
  if (filters?.status) params.status = filters.status
  if (filters?.opened) params.opened = filters.opened
  if (filters?.search) params.search = filters.search
  const { data } = await apiClient.get<BasePaginationResponse<MassEmailRecipient>>(
    `${base}/${id}/recipients`,
    { params, signal: options?.signal }
  )
  return data
}

export const massEmailsAPI = {
  list,
  create,
  getById,
  update,
  remove,
  previewRecipients,
  sendTest,
  send,
  cancel,
  retryFailed,
  retryRecipient,
  listRecipients
}

export default massEmailsAPI
