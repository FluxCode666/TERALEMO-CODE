import { apiClient } from '../client'

const ACCOUNT_TEST_QUESTIONS_PATH = '/admin/account-test-questions'

export interface AccountTestQuestion {
  id: number
  name: string
  content: string
  answer_patterns: string[]
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface CreateAccountTestQuestionRequest {
  name: string
  content: string
  answer_patterns?: string[]
  enabled?: boolean
  sort_order?: number
}

export interface UpdateAccountTestQuestionRequest {
  name?: string
  content?: string
  answer_patterns?: string[]
  enabled?: boolean
  sort_order?: number
}

export interface DeleteAccountTestQuestionResponse {
  message: string
}

export async function list(enabled?: boolean): Promise<AccountTestQuestion[]> {
  const { data } = await apiClient.get<AccountTestQuestion[]>(ACCOUNT_TEST_QUESTIONS_PATH, {
    params: enabled === undefined ? undefined : { enabled }
  })
  return data
}

export async function getById(id: number): Promise<AccountTestQuestion> {
  const { data } = await apiClient.get<AccountTestQuestion>(`${ACCOUNT_TEST_QUESTIONS_PATH}/${id}`)
  return data
}

export async function create(
  request: CreateAccountTestQuestionRequest
): Promise<AccountTestQuestion> {
  const { data } = await apiClient.post<AccountTestQuestion>(ACCOUNT_TEST_QUESTIONS_PATH, request)
  return data
}

export async function update(
  id: number,
  request: UpdateAccountTestQuestionRequest
): Promise<AccountTestQuestion> {
  const { data } = await apiClient.put<AccountTestQuestion>(
    `${ACCOUNT_TEST_QUESTIONS_PATH}/${id}`,
    request
  )
  return data
}

export async function deleteQuestion(id: number): Promise<DeleteAccountTestQuestionResponse> {
  const { data } = await apiClient.delete<DeleteAccountTestQuestionResponse>(
    `${ACCOUNT_TEST_QUESTIONS_PATH}/${id}`
  )
  return data
}

export const accountTestQuestionsAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteQuestion
}

export default accountTestQuestionsAPI
