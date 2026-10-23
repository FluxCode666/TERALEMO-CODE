import { apiClient } from '../client'
import type { AsyncTask } from '../asyncTasks'

export interface AdminAsyncTask extends AsyncTask {
  user_id: number
  user_email?: string
  user_name?: string
  key_name?: string
  group_name?: string
}

export interface AdminAsyncTaskFilters {
  page: number
  page_size: number
  kind?: string
  status?: string
  protocol?: string
  search?: string
  user_id?: number
  api_key_id?: number
  group_id?: number
}

export interface AdminAsyncTaskPage {
  items: AdminAsyncTask[]
  total: number
  page: number
  page_size: number
}

function taskPath(task: Pick<AdminAsyncTask, 'user_id' | 'id'>) {
  return `/admin/async-tasks/${task.user_id}/${encodeURIComponent(task.id)}`
}

export const adminAsyncTasksAPI = {
  async list(params: AdminAsyncTaskFilters, signal?: AbortSignal) {
    return (await apiClient.get<AdminAsyncTaskPage>('/admin/async-tasks', { params, signal })).data
  },
  async get(task: Pick<AdminAsyncTask, 'user_id' | 'id'>, signal?: AbortSignal) {
    return (await apiClient.get<AdminAsyncTask>(taskPath(task), { signal })).data
  },
  async refresh(task: Pick<AdminAsyncTask, 'user_id' | 'id'>, signal?: AbortSignal) {
    return (await apiClient.post<AdminAsyncTask>(`${taskPath(task)}/refresh`, undefined, { signal })).data
  },
  async download(task: Pick<AdminAsyncTask, 'user_id' | 'id'>) {
    return (await apiClient.get<Blob>(`${taskPath(task)}/content`, { responseType: 'blob', timeout: 120000 })).data
  }
}

export default adminAsyncTasksAPI
