import { describe, expect, it } from 'vitest'
import {
  VIDEO_RESOLUTION_OPTIONS,
  applyVideoCapabilities,
  normalizeVideoResolutions,
  readVideoDuration,
  validateVideoDurations
} from '@/utils/videoCapabilities'

describe('videoCapabilities utils', () => {
  it('normalizes resolutions to lowercase canonical order without duplicates', () => {
    expect(normalizeVideoResolutions(['2K', '480P', '2k', '1080p'])).toEqual(['480p', '1080p', '2k'])
    expect(normalizeVideoResolutions(['bogus', '720p'])).toEqual(['720p'])
    expect(normalizeVideoResolutions([])).toEqual([])
    expect(normalizeVideoResolutions('720p')).toEqual([])
    expect(normalizeVideoResolutions(undefined)).toEqual([])
    expect(VIDEO_RESOLUTION_OPTIONS).toEqual(['480p', '720p', '768p', '1080p', '2k'])
  })

  it('reads stored durations only when they are positive integers', () => {
    expect(readVideoDuration(4)).toBe('4')
    expect(readVideoDuration(60)).toBe('60')
    expect(readVideoDuration(0)).toBe('')
    expect(readVideoDuration(1.5)).toBe('')
    expect(readVideoDuration('6')).toBe('')
    expect(readVideoDuration(undefined)).toBe('')
  })

  it('accepts empty or valid durations within 1-60', () => {
    expect(validateVideoDurations('', '')).toBeNull()
    expect(validateVideoDurations('4', '15')).toBeNull()
    expect(validateVideoDurations('1', '')).toBeNull()
    expect(validateVideoDurations('', '60')).toBeNull()
    expect(validateVideoDurations(6, 6)).toBeNull()
  })

  it('rejects non-integer, out-of-range and inverted durations', () => {
    expect(validateVideoDurations('1.5', '')).toBe('minInvalid')
    expect(validateVideoDurations('0', '')).toBe('minInvalid')
    expect(validateVideoDurations('61', '')).toBe('minInvalid')
    expect(validateVideoDurations('abc', '')).toBe('minInvalid')
    expect(validateVideoDurations('', '0')).toBe('maxInvalid')
    expect(validateVideoDurations('', '61')).toBe('maxInvalid')
    expect(validateVideoDurations('', '2.5')).toBe('maxInvalid')
    expect(validateVideoDurations('15', '4')).toBe('minGtMax')
  })

  it('writes only configured keys on create', () => {
    const credentials: Record<string, unknown> = { api_key: 'k' }
    applyVideoCapabilities(credentials, [], '', '', 'create')
    expect(credentials).toEqual({ api_key: 'k' })

    applyVideoCapabilities(credentials, ['1080p'], '4', '15', 'create')
    expect(credentials).toEqual({
      api_key: 'k',
      video_resolutions: ['1080p'],
      video_min_duration_seconds: 4,
      video_max_duration_seconds: 15
    })
  })

  it('writes only the min duration when only min is configured', () => {
    const credentials: Record<string, unknown> = {}
    applyVideoCapabilities(credentials, [], '2', '', 'create')
    expect(credentials).toEqual({ video_min_duration_seconds: 2 })
  })

  it('deletes cleared keys on edit instead of writing empty values', () => {
    const credentials: Record<string, unknown> = {
      video_resolutions: ['768p'],
      video_min_duration_seconds: 4,
      video_max_duration_seconds: 15
    }
    applyVideoCapabilities(credentials, [], '', '', 'edit')
    expect(credentials).toEqual({})

    const untouchedLegacy: Record<string, unknown> = { api_key: 'k' }
    applyVideoCapabilities(untouchedLegacy, [], '', '', 'edit')
    expect(untouchedLegacy).toEqual({ api_key: 'k' })
  })
})
