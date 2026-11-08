import { afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import type { AsyncTask, AsyncTaskPage } from '@/api/asyncTasks'
import type { CreationSnapshot, CreationSubmitRequest } from '@/features/creation-studio/types'
import { submitImageFixture } from '@/features/creation-studio/fixtures'

export const NOW = 1794038400
export const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value))
const wrappers: VueWrapper[] = []

export function track<T extends VueWrapper>(wrapper: T): T {
  wrappers.push(wrapper)
  return wrapper
}

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
})

export function setupComposable<T>(setup: () => T): T {
  let result!: T
  track(mount(defineComponent({ setup() { result = setup(); return () => h('div') } })))
  return result
}

export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

export function request(overrides: Partial<CreationSubmitRequest> = {}): CreationSubmitRequest {
  return {
    api_key_id: 7, mode: 'image', protocol: 'openai', model: 'gpt-image-2',
    prompt: 'QA lighthouse', params: { size: '1024x1024', quality: 'auto', n: 1 }, ...overrides,
  }
}

export function snapshot(overrides: Partial<CreationSnapshot> = {}): CreationSnapshot {
  return { ...request(), catalog_id: submitImageFixture.catalog_id, task_id: submitImageFixture.task_id,
    images: [], created_at: NOW, ...overrides }
}

export function task(overrides: Partial<AsyncTask> = {}): AsyncTask {
  return { id: 'imgtask_qa', task_id: 'imgtask_qa', kind: 'image', protocol: 'openai',
    model: 'gpt-image-2', status: 'completed', api_key_id: 7, created_at: NOW,
    expires_at: NOW + 86400, ...overrides }
}

export function page(items: AsyncTask[], total = items.length, number = 1): AsyncTaskPage {
  return { items, total, page: number, page_size: 20 }
}
