import { describe, expect, it } from 'vitest'
import zh from '../zh'
import en from '../en'
import type { DocBlock, DocSection, VideoDocsContent } from '../types'

const OPAQUE_RESOURCES = [
  'c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA',
  'Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk',
  'bWluaW1heDpleGFtcGxlLXRhc2staWQ',
  'bWluaW1heC1maWxlOmV4YW1wbGUtZmlsZS1pZA'
]

const FORBIDDEN_RAW_ID_URLS = [
  '/v1/video-tasks/cgt-',
  '/v1/video-tasks/example-task-id',
  '/v1/video-tasks/example-operation-id',
  '/v1/video-tasks/models/'
]

function allSections(docs: VideoDocsContent): DocSection[] {
  return [
    ...docs.beforeProtocols,
    ...docs.protocols.flatMap(protocol => protocol.sections),
    ...docs.afterProtocols
  ]
}

function allBlocks(docs: VideoDocsContent): DocBlock[] {
  return allSections(docs).flatMap(section => section.blocks)
}

function serialized(docs: VideoDocsContent): string {
  return JSON.stringify(docs)
}

function protocolExamples(protocol: VideoDocsContent['protocols'][number]) {
  return protocol.sections
    .flatMap(section => section.blocks)
    .filter((block): block is Extract<DocBlock, { type: 'example' }> => block.type === 'example')
}

function responseTables(protocol: VideoDocsContent['protocols'][number]) {
  const endpointSection = protocol.sections.find(section => section.id.endsWith('-endpoints'))
  return (endpointSection?.blocks ?? []).filter((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table').slice(1)
}

function examples(docs: VideoDocsContent) {
  return docs.protocols.flatMap(protocol => protocol.sections.flatMap(section => section.blocks.filter((block): block is Extract<DocBlock, { type: 'example' }> => block.type === 'example')))
}

describe('video docs content', () => {
  it('uses identical protocol ids and names in both locales', () => {
    expect(en.protocols.map(protocol => protocol.id)).toEqual(zh.protocols.map(protocol => protocol.id))
    expect(en.protocols.map(protocol => protocol.name)).toEqual(zh.protocols.map(protocol => protocol.name))
    expect(zh.protocols).toHaveLength(4)
  })

  it('mirrors the section structure between locales', () => {
    expect(allSections(en).map(section => section.id)).toEqual(allSections(zh).map(section => section.id))
    for (const [index, zhSection] of allSections(zh).entries()) {
      const enSection = allSections(en)[index]!
      expect(enSection.blocks.map(block => block.type)).toEqual(zhSection.blocks.map(block => block.type))
    }
  })

  it('keeps table shapes aligned between locales', () => {
    const zhTables = allBlocks(zh).filter((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table')
    const enTables = allBlocks(en).filter((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table')
    expect(enTables).toHaveLength(zhTables.length)
    for (const [index, zhTable] of zhTables.entries()) {
      const enTable = enTables[index]!
      expect(enTable.headers).toHaveLength(zhTable.headers.length)
      expect(enTable.rows).toHaveLength(zhTable.rows.length)
      for (const [rowIndex, zhRow] of zhTable.rows.entries()) {
        expect(enTable.rows[rowIndex]).toHaveLength(zhRow.length)
      }
    }
  })

  it('keeps list item counts aligned between locales', () => {
    const zhLists = allBlocks(zh).filter((block): block is Extract<DocBlock, { type: 'list' }> => block.type === 'list')
    const enLists = allBlocks(en).filter((block): block is Extract<DocBlock, { type: 'list' }> => block.type === 'list')
    expect(enLists).toHaveLength(zhLists.length)
    for (const [index, zhList] of zhLists.entries()) {
      expect(enLists[index]!.items).toHaveLength(zhList.items.length)
    }
  })

  it('provides response parameter tables and complete examples for every protocol', () => {
    const expectedResponseTableCounts = [4, 3, 3, 4]
    for (const [index, protocol] of zh.protocols.entries()) {
      const enProtocol = en.protocols[index]!
      const zhTables = responseTables(protocol)
      const enTables = responseTables(enProtocol)
      expect(zhTables).toHaveLength(expectedResponseTableCounts[index]!)
      expect(enTables).toHaveLength(zhTables.length)
      for (const table of [...zhTables, ...enTables]) {
        expect(table.headers).toHaveLength(3)
        expect(table.rows.length).toBeGreaterThan(0)
        expect(table.rows.every(row => row.every(cell => typeof cell === 'string'))).toBe(true)
      }

      const zhExamples = protocolExamples(protocol)
      const enExamples = protocolExamples(enProtocol)
      expect(zhExamples.length).toBeGreaterThan(0)
      expect(enExamples.map(example => example.id)).toEqual(zhExamples.map(example => example.id))
      for (const example of [...zhExamples, ...enExamples]) {
        expect(example.title).toBeTruthy()
        expect(example.description).toBeTruthy()
        expect(example.request).toContain('curl ')
        expect(example.responses.length).toBeGreaterThan(0)
        expect(example.responses.every(response => Number.isInteger(response.status))).toBe(true)
      }
      for (const localeExamples of [zhExamples, enExamples]) {
        const query = localeExamples.find(example => example.id === 'query')
        expect(query?.responses).toHaveLength(2)
      }
    }
  })

  it('keeps response tables and example response shapes aligned between locales', () => {
    for (const [index, zhProtocol] of zh.protocols.entries()) {
      const enProtocol = en.protocols[index]!
      expect(responseTables(enProtocol).map(table => table.rows.map(row => row.length))).toEqual(
        responseTables(zhProtocol).map(table => table.rows.map(row => row.length))
      )
      expect(protocolExamples(enProtocol).map(example => example.responses.map(response => Boolean(response.body || response.headers))))
        .toEqual(protocolExamples(zhProtocol).map(example => example.responses.map(response => Boolean(response.body || response.headers))))
    }
  })

  it('gives every example a request and at least one copyable response', () => {
    for (const example of [...examples(en), ...examples(zh)]) {
      expect(example.id).toBeTruthy()
      expect(example.title).toBeTruthy()
      expect(example.description).toBeTruthy()
      expect(example.request.trim()).toMatch(/^curl\s+/)
      expect(example.responses.length).toBeGreaterThan(0)
      for (const response of example.responses) {
        expect(response.title).toBeTruthy()
        expect(response.status).toBeGreaterThanOrEqual(100)
        expect(response.status).toBeLessThan(600)
        if (response.status === 204) {
          expect(response.body).toBeUndefined()
          expect(response.headers).toBeUndefined()
        } else {
          expect(response.body || response.headers).toBeTruthy()
        }
      }
    }
  })

  it('documents Seedance cancellation as a 204 empty response in both locales', () => {
    for (const docs of [en, zh]) {
      const cancel = protocolExamples(docs.protocols[0]!).find(example => example.id === 'cancel')!
      expect(cancel.responses).toHaveLength(1)
      expect(cancel.responses[0]!.status).toBe(204)
      expect(cancel.responses[0]!.body).toBeUndefined()
      expect(cancel.responses[0]!.headers).toBeUndefined()

      const endpointSection = docs.protocols[0]!.sections.find(section => section.id.endsWith('-endpoints'))!
      const cancelTable = responseTables(docs.protocols[0]!).find(table => table.rows.some(row => row[0] === 'HTTP status' && row[2].includes('204')))
      expect(cancelTable).toBeDefined()
      expect(cancelTable!.rows.some(row => row[0] === 'response body' && row[1] === 'empty')).toBe(true)
      expect(endpointSection.blocks.some(block => block.type === 'table' && block.rows.some(row => row[0] === 'HTTP status' && row[2].includes('204')))).toBe(true)
    }
  })

  it('formats JSON response examples with indentation for display and copying', () => {
    for (const docs of [en, zh]) {
      const examplesWithJson = examples(docs).filter(example => example.responses.some(response => response.body))
      expect(examplesWithJson.length).toBeGreaterThan(0)
      expect(examplesWithJson.every(example => example.responses.filter(response => response.body).every(response => response.body!.includes('\n  ')))).toBe(true)

      const responseSection = docs.protocols[0]!.sections.find(section => section.id.endsWith('-responses'))!
      const responseCode = responseSection.blocks.find((block): block is Extract<DocBlock, { type: 'code' }> => block.type === 'code')!
      expect(responseCode.code).toContain('\n  ')
    }
  })

  it('keeps MiniMax v2 submission root fields and download error boundaries explicit', () => {
    for (const docs of [en, zh]) {
      const tables = responseTables(docs.protocols.find(protocol => protocol.id === 'minimax-v2')!)
      expect(tables[0]!.rows.map(row => row[0])).toEqual(['task_id'])
      const download = tables[2]!
      const statusDescription = download.rows.find(row => row[0] === 'HTTP status')![2]
      expect(statusDescription).toContain('400')
      expect(statusDescription).toContain('401')
      expect(statusDescription).toContain('404')
      expect(statusDescription).toContain('502')
    }
  })

  it('keeps Gemini submission, response, and query models aligned', () => {
    for (const docs of [en, zh]) {
      const gemini = docs.protocols.find(protocol => protocol.id === 'gemini')!
      const geminiExamples = protocolExamples(gemini)
      for (const id of ['text-to-video', 'first-frame']) {
        const example = geminiExamples.find(item => item.id === id)!
        expect(example.request).toContain('veo-3.0-generate-001')
        expect(example.responses[0]!.body).toContain('models/veo-3.0-generate-001/operations/')
      }
      const query = geminiExamples.find(item => item.id === 'query')!
      expect(query.description).toContain('name')
      expect(query.description).toContain('operation')
      expect(query.request).toContain('models/veo-3.0-generate-001/operations/')
    }
  })

  it('keeps example ids, response counts, and response parameter tables aligned between locales', () => {
    const enExamples = examples(en)
    const zhExamples = examples(zh)
    expect(enExamples.map(example => example.id)).toEqual(zhExamples.map(example => example.id))
    expect(enExamples.map(example => example.responses.length)).toEqual(zhExamples.map(example => example.responses.length))

    for (const docs of [en, zh]) {
      for (const protocol of docs.protocols) {
        const endpointsSection = protocol.sections.find(section => section.id.endsWith('-endpoints'))!
        const endpointTable = endpointsSection.blocks.find((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table' && block.rows.some(row => ['POST', 'GET', 'DELETE'].includes(row[0] || '')))
        expect(endpointTable).toBeDefined()
        const responseTables = endpointsSection.blocks.filter((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table' && ['Field path', '字段路径'].includes(block.headers[0] || ''))
        expect(responseTables.length).toBe(endpointTable!.rows.length + 1) // includes the gateway content download interface
      }
    }
  })

  it('has the same number of error-code rows in both locales', () => {
    const errorRows = (docs: VideoDocsContent) => {
      const section = docs.afterProtocols.find(entry => entry.id === 'errors')!
      const table = section.blocks.find((block): block is Extract<DocBlock, { type: 'table' }> => block.type === 'table')!
      return table.rows.length
    }
    expect(errorRows(zh)).toBe(9)
    expect(errorRows(en)).toBe(errorRows(zh))
  })

  it.each([
    ['zh', zh],
    ['en', en]
  ] as const)('uses opaque download resources in %s and never builds content URLs from raw task ids', (_locale, docs) => {
    const text = serialized(docs)
    for (const resource of OPAQUE_RESOURCES) {
      expect(text).toContain(`/v1/video-tasks/${resource}/content?index=0`)
    }
    for (const forbidden of FORBIDDEN_RAW_ID_URLS) {
      expect(text).not.toContain(forbidden)
    }
  })
})
