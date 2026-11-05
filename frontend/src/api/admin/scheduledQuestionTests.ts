import { apiClient } from '../client'

export type ScheduledQuestionTestScheduleAction = 'paused' | 'resumed' | 'pause_failed' | 'resume_failed' | 'resume_blocked' | ''

/** Backend-computed outcome of one account in one round (contract §4.2). */
export type ScheduledQuestionTestResult = 'passed' | 'failed' | 'unjudged' | 'error' | 'incomplete' | 'skipped'

/** Why an account was skipped because it ran out of quota / hit a rate limit. */
export type ScheduledQuestionTestSkipReason = 'quota_exceeded' | 'window_cost_exceeded' | 'rate_limited' | ''

export interface ScheduledQuestionTestSummary {
  account_id: number
  account_name: string
  platform?: string
  passed: number
  total: number
  judged: number
  ratio: number | null
  alerting: boolean
  /**
   * Alert state carried by the account. Empty string for skipped accounts: a skip never runs a test,
   * so no alert transition is published and the backend leaves this field blank (it is not omitted).
   */
  state_after: 'ok' | 'alert' | ''
  error: string
  /** True when the account did not get a result for every question in this round. */
  incomplete?: boolean
  /** Scheduling action applied to the account at the end of this round. */
  schedule_action?: ScheduledQuestionTestScheduleAction
  /** True when the account is currently paused by this plan's auto-pause. */
  auto_paused: boolean
  /** Backend-computed result. Absent on runs recorded before v0.3.8 (derive from alerting/ratio/error). */
  result?: ScheduledQuestionTestResult
  /** Reason the round skipped this account; empty when it was tested. */
  skip_reason?: ScheduledQuestionTestSkipReason
  /**
   * RFC3339 timestamp when a skipped account is expected back, or null when no automatic reset is known.
   * A null value means different things per reason: `quota_exceeded` needs an admin quota change, while
   * `rate_limited` / `window_cost_exceeded` simply have an unknown recovery time and are retried next round.
   */
  skip_reset_at?: string | null
  /** Account schedulable flag at the start of the round; null when the account no longer exists. */
  schedulable_before?: boolean | null
  /** Account schedulable flag after this round's scheduling action; null when the account no longer exists. */
  schedulable_after?: boolean | null
}

export interface ScheduledQuestionTestNotificationResult {
  kind: 'alert' | 'recovered' | 'report'
  channel: 'email' | 'feishu'
  success: boolean
  detail: string
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
  report_enabled: boolean
  email_enabled: boolean
  feishu_enabled: boolean
  /** The raw webhook URL is never returned by the API. */
  feishu_webhook_configured: boolean
  feishu_webhook_hint: string
  feishu_secret_configured: boolean
  auto_pause_enabled: boolean
  auto_resume_enabled: boolean
  running: boolean
  last_run_at: string | null
  next_run_at: string | null
  last_status?: string
  passed_accounts?: number
  total_accounts?: number
  alerted_accounts?: number
  /** Accounts skipped because they ran out of quota in the latest round. */
  skipped_accounts?: number
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
  paused_accounts: number
  resumed_accounts: number
  auto_pause_skipped: boolean
  notification_results: ScheduledQuestionTestNotificationResult[]
  /** Accounts skipped because they ran out of quota; absent on runs recorded before v0.3.8. */
  skipped_accounts?: number
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
  report_enabled?: boolean
  email_enabled?: boolean
  feishu_enabled?: boolean
  /** Omit to keep the stored webhook, send '' to clear it. */
  feishu_webhook_url?: string
  /** Omit to keep the stored secret, send '' to clear it. */
  feishu_secret?: string
  auto_pause_enabled?: boolean
  auto_resume_enabled?: boolean
}

export interface ScheduledQuestionTestFeishuTestRequest {
  plan_id?: number
  webhook_url?: string
  /** Omitted/null uses the stored secret of plan_id; '' clears it for this test. */
  secret?: string | null
}

export interface ScheduledQuestionTestFeishuTestResult {
  success: boolean
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

export async function testFeishu(request: ScheduledQuestionTestFeishuTestRequest = {}): Promise<ScheduledQuestionTestFeishuTestResult> {
  const { data } = await apiClient.post<ScheduledQuestionTestFeishuTestResult>('/admin/scheduled-question-tests/feishu-test', request)
  return data ?? { success: false }
}

export const scheduledQuestionTestsAPI = { list, create, update, delete: remove, listRuns, runNow, testFeishu }
export default scheduledQuestionTestsAPI
