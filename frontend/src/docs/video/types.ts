/**
 * Structured content blocks for the user-facing API docs page.
 * Content is static and trusted; blocks render via v-for with no v-html.
 * Code blocks may contain the placeholders {{BASE_URL}} and {{API_KEY}},
 * which the view replaces with the current origin and the selected key.
 */
export interface DocParagraph {
  type: 'p'
  text: string
}

export interface DocList {
  type: 'list'
  items: string[]
}

export interface DocTable {
  type: 'table'
  headers: string[]
  rows: string[][]
}

export interface DocCode {
  type: 'code'
  title: string
  code: string
}

/** Internal router link rendered with RouterLink. */
export interface DocLink {
  type: 'link'
  to: string
  text: string
}

export type DocBlock = DocParagraph | DocList | DocTable | DocCode | DocLink

export interface DocSection {
  id: string
  title: string
  blocks: DocBlock[]
}

export interface DocProtocol {
  id: string
  name: string
  sections: DocSection[]
}

export interface VideoDocsContent {
  /** Category name shown in the catalog, e.g. 视频生成 / Video Generation. */
  category: string
  /** Sections rendered before the protocol tabs: overview, billing, download. */
  beforeProtocols: DocSection[]
  /** Title of the section that hosts the per-protocol tabs. */
  protocolsTitle: string
  protocols: DocProtocol[]
  /** Sections rendered after the protocol tabs: validation rules, error codes. */
  afterProtocols: DocSection[]
}
