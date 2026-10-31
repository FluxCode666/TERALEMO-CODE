import { apiClient } from '../client'

export interface ScheduledQuestionTestSummary {
  account_id: number
  account_name: string
  platform?: string
  passed: number
  total: number
  judged: number
  ratio: number | null
  alerting: boolean
  state_after: 'ok' | 'alert'
  error: string
}

export interface ScheduledQuestionTestPlan {
  id: number
  name: string
  account_ids: number[]
  question_ids: number[]
  model_id: string
  cron_expression: string
  enabled: boolean
  min_pass_ratio: number
  cooldown_minutes: number
  notify_enabled: boolean
  running: boolean
  last_run_at: string | null
  next_run_at: string | null
  last_status?: string
  passed_accounts?: number
  total_accounts?: number
  alerted_accounts?: number
}

export interface ScheduledQuestionTestRun {
  id: number
  plan_id: number
  status: string
  started_at: string
  finished_at: string | null
  total_accounts: number
  passed_accounts: number
  alerted_accounts: number
  error_message: string
  summary: ScheduledQuestionTestSummary[] | null
  notifications_suppressed: boolean
}

export interface ScheduledQuestionTestAccepted {
  accepted: boolean
  plan_id: number
}

export interface ScheduledQuestionTestRequest {
  name: string
  account_ids: number[]
  question_ids: number[]
  model_id?: string
  cron_expression: string
  enabled?: boolean
  min_pass_ratio?: number
  cooldown_minutes?: number
  notify_enabled?: boolean
}

export async function list(): Promise<ScheduledQuestionTestPlan[]> {
  const { data } = await apiClient.get<ScheduledQuestionTestPlan[]>('/admin/scheduled-question-tests')
  return data ?? []
}

export async function create(request: ScheduledQuestionTestRequest): Promise<ScheduledQuestionTestPlan> {
  const { data } = await apiClient.post<ScheduledQuestionTestPlan>('/admin/scheduled-question-tests', request)
  return data
}

export async function update(id: number, request: Partial<ScheduledQuestionTestRequest>): Promise<ScheduledQuestionTestPlan> {
  const { data } = await apiClient.put<ScheduledQuestionTestPlan>(`/admin/scheduled-question-tests/${id}`, request)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/scheduled-question-tests/${id}`)
}

export async function listRuns(id: number, limit = 50): Promise<ScheduledQuestionTestRun[]> {
  const { data } = await apiClient.get<ScheduledQuestionTestRun[]>(`/admin/scheduled-question-tests/${id}/runs`, { params: { limit } })
  return data ?? []
}

export async function runNow(id: number): Promise<ScheduledQuestionTestAccepted> {
  const { data } = await apiClient.post<ScheduledQuestionTestAccepted>(`/admin/scheduled-question-tests/${id}/run-now`)
  return data
}

export const scheduledQuestionTestsAPI = { list, create, update, delete: remove, listRuns, runNow }
export default scheduledQuestionTestsAPI
