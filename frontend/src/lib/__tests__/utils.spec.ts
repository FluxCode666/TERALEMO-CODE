import { describe, expect, it } from 'vitest'
import { cn } from '@/lib/utils'

describe('cn（Tailwind 3）', () => {
  it('支持条件、嵌套数组并忽略空值', () => {
    expect(cn('base', [false, 'active', ['nested']], { hidden: false, visible: true }, undefined, null))
      .toBe('base active nested visible')
  })

  it('后传入的类覆盖冲突项，同时保留不同轴的间距', () => {
    expect(cn('p-4 px-6 text-sm', 'px-2 text-lg')).toBe('p-4 px-2 text-lg')
  })

  it('保留响应式、暗色和状态变体及前景色', () => {
    expect(cn('bg-primary hover:bg-primary/90 dark:bg-muted text-primary-foreground', 'hover:bg-accent'))
      .toBe('bg-primary dark:bg-muted text-primary-foreground hover:bg-accent')
  })

  it('自定义语义圆角和现有阴影能被调用方覆盖', () => {
    expect(cn('rounded-ui-lg shadow-card', 'rounded-none shadow-none')).toBe('rounded-none shadow-none')
    expect(cn('rounded-lg', 'rounded-ui-md')).toBe('rounded-ui-md')
  })
})
