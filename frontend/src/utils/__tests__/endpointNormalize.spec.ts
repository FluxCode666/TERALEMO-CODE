import { describe, expect, it } from 'vitest'

import { normalizeEndpointPath, normalizeEndpointSegment } from '../endpointNormalize'

describe('normalizeEndpointPath', () => {
  it('normalizes vidtask segments regardless of id charset', () => {
    expect(normalizeEndpointPath('/api/v1/admin/async-tasks/1/vidtask_a1B2c3D4e5F6/refresh'))
      .toBe('/api/v1/admin/async-tasks/:id/:id/refresh')
  })

  it('normalizes 64-char hex task ids', () => {
    const hex = 'a'.repeat(64)
    expect(normalizeEndpointPath(`/api/v1/admin/async-tasks/1/vidtask_${hex}/refresh`))
      .toBe('/api/v1/admin/async-tasks/:id/:id/refresh')
  })

  it('normalizes bare hex segments of 16+ chars', () => {
    expect(normalizeEndpointPath('/v1/files/0123456789abcdef/content'))
      .toBe('/v1/files/:id/content')
    expect(normalizeEndpointPath('/v1/files/0123456789ABCDEF/content'))
      .toBe('/v1/files/:id/content')
  })

  it('normalizes UUID segments', () => {
    expect(normalizeEndpointPath('/v1/sessions/550e8400-e29b-41d4-a716-446655440000/messages'))
      .toBe('/v1/sessions/:id/messages')
  })

  it('normalizes pure numeric segments', () => {
    expect(normalizeEndpointPath('/api/v1/admin/async-tasks/42/videos/2043139464739389440'))
      .toBe('/api/v1/admin/async-tasks/:id/videos/:id')
  })

  it('keeps normal segments untouched (models, versions, actions)', () => {
    expect(normalizeEndpointPath('/v1/messages')).toBe('/v1/messages')
    expect(normalizeEndpointPath('/v1/chat/completions')).toBe('/v1/chat/completions')
    expect(normalizeEndpointPath('/v1beta/models/gemini-2.5-pro:generateContent'))
      .toBe('/v1beta/models/gemini-2.5-pro:generateContent')
    expect(normalizeEndpointPath('/api/v1/admin/async-tasks/refresh')).toBe('/api/v1/admin/async-tasks/refresh')
  })

  it('does not mutilate short hex-like or mixed id segments', () => {
    expect(normalizeEndpointPath('/v1/models/deadbeef')).toBe('/v1/models/deadbeef')
    expect(normalizeEndpointPath('/v1/tasks/task_abc123-def')).toBe('/v1/tasks/task_abc123-def')
    expect(normalizeEndpointPath('/v1/models/gpt-5')).toBe('/v1/models/gpt-5')
  })

  it('handles empty and nullish input', () => {
    expect(normalizeEndpointPath('')).toBe('')
    expect(normalizeEndpointPath(null)).toBe('')
    expect(normalizeEndpointPath(undefined)).toBe('')
    expect(normalizeEndpointPath('/')).toBe('/')
  })

  it('does not require a leading slash', () => {
    expect(normalizeEndpointSegment('vidtask_XyZ123')).toBe(':id')
    expect(normalizeEndpointPath('v1/messages')).toBe('v1/messages')
  })
})
