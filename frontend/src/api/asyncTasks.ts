import { apiClient } from './client'

export interface AsyncTask {
  id: string
  task_id: string
  kind: 'image' | 'video'
  protocol: string
  model?: string
  status: 'processing' | 'completed' | 'failed' | 'cancelled'
  api_key_id: number
  group_id?: number
  result_url?: string
  error?: string
  created_at: number
  completed_at?: number
  expires_at: number
}

export interface AsyncTaskPage {
  items: AsyncTask[]
  total: number
  page: number
  page_size: number
}

export const asyncTasksAPI = {
  async list(params: { page: number; page_size: number; kind: string; status: string; search: string }, signal?: AbortSignal) {
    return (await apiClient.get<AsyncTaskPage>('/user/async-tasks', { params, signal })).data
  },
  async refresh(id: string, signal?: AbortSignal) {
    return (await apiClient.post<AsyncTask>(`/user/async-tasks/${encodeURIComponent(id)}/refresh`, undefined, { signal })).data
  },
  async download(id: string) {
    return (await apiClient.get<Blob>(`/user/async-tasks/${encodeURIComponent(id)}/content`, { responseType: 'blob', timeout: 120000 })).data
  }
}
