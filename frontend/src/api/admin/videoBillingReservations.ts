import { apiClient } from '../client'

export type VideoBillingReservationState = 'preparing' | 'accepted' | 'manual' | 'settled' | 'released'

export const VIDEO_BILLING_RESERVATION_STATES: VideoBillingReservationState[] = ['preparing', 'accepted', 'manual', 'settled', 'released']

export interface VideoBillingReservation {
  id: string
  user_id: number
  api_key_id: number
  group_id: number
  account_id: number
  enterprise_id?: number
  subscription_id?: number
  platform: string
  protocol: string
  task_id?: string
  file_id?: string
  state: string
  hold_amount: number
  actual_amount: number
  model?: string
  resolution?: string
  billing_mode?: string
  effective_multiplier: number
  attempts: number
  review_note?: string
  created_at: string
  updated_at: string
}

export interface VideoBillingReservationPage {
  items: VideoBillingReservation[]
  total: number
  page: number
  page_size: number
}

export interface VideoBillingReservationListParams {
  state?: VideoBillingReservationState | ''
  page: number
  page_size: number
}

/** Only reason and actual usage fields; the backend rejects any other field. */
export interface VideoBillingSettlePayload {
  reason: string
  duration_seconds?: number
  video_count?: number
  completion_tokens?: number
}

function reservationPath(id: string) {
  return `/admin/video-billing-reservations/${encodeURIComponent(id)}`
}

export const adminVideoBillingReservationsAPI = {
  async list(params: VideoBillingReservationListParams, signal?: AbortSignal) {
    return (await apiClient.get<VideoBillingReservationPage>('/admin/video-billing-reservations', { params, signal })).data
  },
  async release(id: string, reason: string, signal?: AbortSignal) {
    return (await apiClient.post<VideoBillingReservation>(`${reservationPath(id)}/release`, { reason }, { signal })).data
  },
  async settle(id: string, payload: VideoBillingSettlePayload, signal?: AbortSignal) {
    return (await apiClient.post<VideoBillingReservation>(`${reservationPath(id)}/settle`, payload, { signal })).data
  }
}

export default adminVideoBillingReservationsAPI
