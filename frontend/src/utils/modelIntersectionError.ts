import { extractApiErrorMessage } from './apiError'

export function getModelIntersectionErrorMessage(error: unknown): string {
  return extractApiErrorMessage(error, '')
}

export function isSamePlatformModelIntersectionError(error: unknown): boolean {
  return getModelIntersectionErrorMessage(error).toLowerCase().includes('same platform')
}
