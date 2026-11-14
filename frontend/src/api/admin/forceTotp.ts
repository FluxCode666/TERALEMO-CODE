import { apiClient } from '../client'

export interface ForceTotpSettings {
  enabled: boolean
  users_without_totp: number
  encryption_key_configured: boolean
}

export async function getForceTotp(): Promise<ForceTotpSettings> {
  const { data } = await apiClient.get<ForceTotpSettings>('/admin/settings/force-totp')
  return data
}

export async function updateForceTotp(enabled: boolean): Promise<ForceTotpSettings> {
  const { data } = await apiClient.put<ForceTotpSettings>('/admin/settings/force-totp', { enabled })
  return data
}
