/**
 * Display-only normalization for endpoint paths: collapses obvious per-request
 * ID segments (async video task IDs, long hex tokens, UUIDs, pure numeric IDs)
 * into `:id` so historical dirty rows aggregate into one visible bucket.
 * Never use the result for backend filter/query params — it is a render concern.
 */

const VIDTASK_SEGMENT = /^vidtask_[A-Za-z0-9]+$/
const UUID_SEGMENT = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const LONG_HEX_SEGMENT = /^[0-9a-f]{16,}$/i
const NUMERIC_SEGMENT = /^\d+$/

export function normalizeEndpointSegment(segment: string): string {
  if (
    VIDTASK_SEGMENT.test(segment) ||
    UUID_SEGMENT.test(segment) ||
    LONG_HEX_SEGMENT.test(segment) ||
    NUMERIC_SEGMENT.test(segment)
  ) {
    return ':id'
  }
  return segment
}

export function normalizeEndpointPath(endpoint: string | null | undefined): string {
  if (!endpoint) return ''
  return endpoint.split('/').map(normalizeEndpointSegment).join('/')
}
