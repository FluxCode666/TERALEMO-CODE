import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import postcss, { type Root } from 'postcss'
import tailwindcss from 'tailwindcss'
import resolveConfig from 'tailwindcss/resolveConfig'
import { beforeAll, describe, expect, it } from 'vitest'
import config from '../../../tailwind.config.js'

describe('shadcn 与现有主题兼容', () => {
  let css: Root

  beforeAll(async () => {
    const read = (path: string) => readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
    const source = read('../../style.css').replace("@import './styles/shadcn.css';", read('../../styles/shadcn.css'))
    const result = await postcss([tailwindcss({
      ...config,
      content: [{ raw: 'card stat-card btn input border ring rounded-lg duration-200 bg-card text-card-foreground shadcn-scope' }],
    })]).process(source, { from: undefined })
    css = result.root
  })

  function declarations(selector: string) {
    const values: Record<string, string> = {}
    css.walkRules(selector, rule => { rule.walkDecls(decl => { values[decl.prop] = decl.value }) })
    return values
  }

  it('不改变旧卡片阴影，也不引入同名阴影颜色', () => {
    for (const selector of ['.card', '.stat-card']) {
      const values = declarations(selector)
      expect(values['--tw-shadow']).toContain('rgba(0, 0, 0, 0.04)')
      expect(values['--tw-shadow-color']).toBeUndefined()
      expect(values['border-radius']).toBe('1rem')
    }
  })

  it('动画 duration 属性仅在 shadcn 作用域内生效', () => {
    expect(declarations('.duration-200')['transition-duration']).toBe('200ms')
    expect(declarations('.duration-200')['animation-duration']).toBeUndefined()
    expect(declarations('.duration-200:where(.shadcn-scope, .shadcn-scope *)')['animation-duration']).toBe('200ms')
  })

  it('保留既有 Teal 色阶、默认边框、ring 和圆角', () => {
    const { theme } = resolveConfig(config)
    expect(theme.colors.primary[500]).toBe('#14b8a6')
    expect(theme.colors.primary[600]).toBe('#0d9488')
    expect(theme.borderColor.DEFAULT).toBe('#e5e7eb')
    expect(theme.ringColor.DEFAULT).toBe('#3b82f6')
    expect(theme.borderRadius.lg).toBe('0.5rem')
    expect(declarations('.bg-card')['background-color']).toContain('var(--card)')
  })
})
