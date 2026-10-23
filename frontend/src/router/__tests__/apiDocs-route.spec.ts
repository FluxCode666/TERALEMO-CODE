import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const routerSource = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts'), 'utf8')

describe('API docs route', () => {
  it('registers /docs/api as an authenticated non-admin route right after /async-tasks', () => {
    expect(routerSource).toMatch(
      /path: '\/async-tasks'[\s\S]*?path: '\/docs\/api',\s*name: 'ApiDocs'[\s\S]*?requiresAuth: true,\s*requiresAdmin: false[\s\S]*?titleKey: 'apiDocs\.title',\s*descriptionKey: 'apiDocs\.description'/
    )
  })
})
