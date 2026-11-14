import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  del: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, put, delete: del }
}))

import * as api from '@/api/admin/massEmails'

describe('admin mass emails API', () => {
  beforeEach(() => {
    get.mockReset().mockResolvedValue({ data: { items: [] } })
    post.mockReset().mockResolvedValue({ data: {} })
    put.mockReset().mockResolvedValue({ data: {} })
    del.mockReset().mockResolvedValue({ data: { message: 'ok' } })
  })

  it('lists with pagination and filters', async () => {
    await api.list(2, 10, { status: 'draft,scheduled', search: 'x' })
    expect(get).toHaveBeenCalledWith('/admin/mass-emails', {
      params: { page: 2, page_size: 10, status: 'draft,scheduled', search: 'x' },
      signal: undefined
    })
  })

  it('creates, updates and deletes', async () => {
    const body = { subject: 's', body_html: '<p>x</p>', recipient_filter: { include_all_users: true } }
    await api.create({ ...body, send_now: true })
    expect(post).toHaveBeenCalledWith('/admin/mass-emails', { ...body, send_now: true })
    await api.update(3, body)
    expect(put).toHaveBeenCalledWith('/admin/mass-emails/3', body)
    await api.remove(3)
    expect(del).toHaveBeenCalledWith('/admin/mass-emails/3')
  })

  it('previews recipients and sends tests', async () => {
    const filter = { include_all_users: false, manual_emails: ['a@b.co'] }
    await api.previewRecipients(filter, 7)
    expect(post).toHaveBeenCalledWith('/admin/mass-emails/preview-recipients', {
      recipient_filter: filter,
      sample_size: 7
    }, undefined)
    await api.sendTest({ subject: 's', body_html: 'b', to: null })
    expect(post).toHaveBeenCalledWith('/admin/mass-emails/test', { subject: 's', body_html: 'b', to: null })
  })

  it('posts send / cancel / retry actions', async () => {
    await api.send(1)
    await api.cancel(1)
    await api.retryFailed(1)
    await api.retryRecipient(1, 9)
    expect(post.mock.calls.map((c) => c[0])).toEqual([
      '/admin/mass-emails/1/send',
      '/admin/mass-emails/1/cancel',
      '/admin/mass-emails/1/retry-failed',
      '/admin/mass-emails/1/recipients/9/retry'
    ])
  })

  it('omits empty recipient filters', async () => {
    await api.listRecipients(1, 1, 50, { status: 'sent', opened: 'false', search: '' })
    expect(get).toHaveBeenCalledWith('/admin/mass-emails/1/recipients', {
      params: { page: 1, page_size: 50, status: 'sent', opened: 'false' },
      signal: undefined
    })
  })
})
