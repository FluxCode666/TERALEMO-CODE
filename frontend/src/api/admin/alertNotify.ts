/**
 * Admin Alert Notify Channels API
 * - Feishu bot / generic webhook channels for ops & account quota alerts
 * - Contract: /Users/duegin/alert-notify/CONTRACT.md
 */

import { apiClient } from '../client'

export type AlertNotifyChannelType = 'feishu' | 'webhook'

export type AlertNotifyEvent = 'ops.alert' | 'account.quota_alert'

export type AlertNotifySeverity = 'info' | 'warning' | 'critical'

export interface AlertNotifyChannelView {
  id: string
  name: string
  type: AlertNotifyChannelType
  enabled: boolean
  url_masked: string
  secret_configured: boolean
  events: AlertNotifyEvent[]
  min_severity: AlertNotifySeverity | ''
  send_resolved: boolean
  updated_at?: string
}

export interface AlertNotifyChannelInput {
  /** 为空表示新建渠道，由服务端生成 id */
  id: string
  name: string
  type: AlertNotifyChannelType
  enabled: boolean
  /** 新建时必填；已有渠道传 "" 表示保留原 URL */
  url: string
  /** "" 表示保留原 secret（新建渠道则为无 secret），非空表示替换 */
  secret: string
  /** true 表示删除已存的 secret，优先级高于“保留” */
  clear_secret: boolean
  events: AlertNotifyEvent[]
  min_severity: AlertNotifySeverity | ''
  send_resolved: boolean
}

export interface AlertNotifyChannelsResponse {
  channels: AlertNotifyChannelView[]
  supported_events: AlertNotifyEvent[]
}

export async function getChannels(): Promise<AlertNotifyChannelsResponse> {
  const { data } = await apiClient.get<AlertNotifyChannelsResponse>('/admin/alert-notify/channels')
  return data
}

export async function updateChannels(
  channels: AlertNotifyChannelInput[]
): Promise<AlertNotifyChannelsResponse> {
  const { data } = await apiClient.put<AlertNotifyChannelsResponse>('/admin/alert-notify/channels', {
    channels
  })
  return data
}

export async function testChannel(channel: AlertNotifyChannelInput): Promise<{ success: boolean }> {
  const { data } = await apiClient.post<{ success: boolean }>('/admin/alert-notify/channels/test', channel)
  return data
}

export const alertNotifyAPI = {
  getChannels,
  updateChannels,
  testChannel
}

export default alertNotifyAPI
