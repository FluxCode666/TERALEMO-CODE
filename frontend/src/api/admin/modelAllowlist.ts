import { apiClient } from '../client'

export interface ModelAllowlistSettings {
  enabled: boolean
  entries: string[]
  notes: Record<string, string>
  cloudflare: boolean
  trusted_cdn_proxies: string[]
}

export interface ModelDomainPreview {
  entry: string
  addresses: string[]
  error?: string
  expires_at: string
}

export interface ModelIPObservation {
  ip: string
  user_id: number
  user_email: string
  key_id: number
  key_name: string
  count: number
  last_seen: number
}

export interface ModelAllowlistResponse {
  settings: ModelAllowlistSettings
  domains: ModelDomainPreview[]
  observations: ModelIPObservation[]
  impact: { ips: number; users: number; keys: number }
}

export async function getModelAllowlist(): Promise<ModelAllowlistResponse> {
  const { data } = await apiClient.get<ModelAllowlistResponse>('/admin/settings/model-allowlist')
  return data
}

export async function saveModelAllowlist(settings: ModelAllowlistSettings): Promise<ModelAllowlistResponse> {
  const { data } = await apiClient.put<ModelAllowlistResponse>('/admin/settings/model-allowlist', settings)
  return data
}

export async function toggleModelAllowlist(enabled: boolean): Promise<ModelAllowlistResponse> {
  const { data } = await apiClient.put<ModelAllowlistResponse>('/admin/settings/model-allowlist/enabled', { enabled })
  return data
}

export async function testModelAllowlist(settings: ModelAllowlistSettings, ip: string): Promise<{ allowed: boolean; domains: ModelDomainPreview[] }> {
  const { data } = await apiClient.post('/admin/settings/model-allowlist/test', { settings, ip })
  return data
}
