import { apiClient } from '../client'
import { createEnterpriseAPI } from '../enterprise'
import type { FetchOptions } from '@/types'
import type { Enterprise, EnterpriseQuery, EnterprisePage, CreateEnterpriseRequest, UpdateEnterpriseRequest } from '@/types/enterprise'

const base = '/admin/enterprises'
export const enterprisesAPI = {
  list: async (params?: EnterpriseQuery, options?: FetchOptions): Promise<EnterprisePage<Enterprise>> => (await apiClient.get<EnterprisePage<Enterprise>>(base, { params, ...options })).data,
  create: async (body: CreateEnterpriseRequest): Promise<Enterprise> => (await apiClient.post<Enterprise>(base, body)).data,
  get: async (id: number, options?: FetchOptions): Promise<Enterprise> => (await (options ? apiClient.get<Enterprise>(`${base}/${id}`, options) : apiClient.get<Enterprise>(`${base}/${id}`))).data,
  update: async (id: number, body: UpdateEnterpriseRequest): Promise<Enterprise> => (await apiClient.put<Enterprise>(`${base}/${id}`, body)).data,
  remove: async (id: number, dissolve?: boolean): Promise<void> => {
    if (dissolve === true) await apiClient.delete(`${base}/${id}`, { params: { dissolve: true } })
    else await apiClient.delete(`${base}/${id}`)
  },
  adjustBalance: async (id: number, body: { operation: 'add' | 'subtract'; amount: number; notes?: string }): Promise<Enterprise> => (await apiClient.post<Enterprise>(`${base}/${id}/balance`, body)).data,
  forEnterprise: (id: number) => createEnterpriseAPI(`${base}/${id}`)
}
export default enterprisesAPI
