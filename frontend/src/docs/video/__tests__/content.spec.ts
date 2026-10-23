import { describe, expect, it } from 'vitest'
import zh from '../zh'
import en from '../en'
import type { DocBlock, DocSection, VideoDocsContent } from '../types'

const OPAQUE_RESOURCES = [
  'c2VlZGFuY2U6Y2d0LWV4YW1wbGUtdGFzay1pZA',
  'Z2VtaW5pOm1vZGVscy92ZW8tMy4wLWdlbmVyYXRlLTAwMS9vcGVyYXRpb25zL2V4YW1wbGUtb3BlcmF0aW9uLWlk',
  'bWluaW1heDpleGFtcGxlLXRhc2staWQ'
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
