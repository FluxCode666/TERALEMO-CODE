import { CalendarDate } from '@internationalized/date'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { RangeCalendar } from '@/components/ui/range-calendar'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'

enableAutoUnmount(afterEach)
const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
beforeEach(() => { HTMLElement.prototype.scrollIntoView = vi.fn() })
afterEach(() => {
  HTMLElement.prototype.scrollIntoView = originalScrollIntoView
  document.body.innerHTML = ''
})

describe('shadcn-vue 关键交互', () => {
  it('Command 支持按显示名称搜索及空结果', async () => {
    const view = mount(defineComponent({
      setup: () => () => h(Command, {}, { default: () => [
        h(CommandInput, { 'aria-label': '搜索成员' }),
        h(CommandList, {}, { default: () => [
          h(CommandEmpty, {}, () => '没有匹配项'),
          h(CommandGroup, {}, () => [
            h(CommandItem, { value: 'u1' }, () => '张三'),
            h(CommandItem, { value: 'u2' }, () => '李四'),
          ]),
        ] }),
      ] }),
    }), { attachTo: document.body })
    await view.get('input').setValue('张')
    await flushPromises()
    expect(view.findAll('[role="option"]').map(item => item.text())).toEqual(['张三'])
    await view.get('input').setValue('不存在')
    await flushPromises()
    expect(view.findAll('[role="option"]')).toHaveLength(0)
    expect(view.text()).toContain('没有匹配项')
    await view.get('input').setValue('')
    await flushPromises()
    expect(view.findAll('[role="option"]')).toHaveLength(2)
    view.unmount()
  })

  it('Popover 的 portal 内容自带主题作用域', async () => {
    const view = mount(defineComponent({
      setup: () => () => h(Popover, { defaultOpen: true }, { default: () => [
        h(PopoverTrigger, {}, () => '筛选'),
        h(PopoverContent, { 'data-test': 'portal' }, () => '弹层内容'),
      ] }),
    }), { attachTo: document.body })
    await flushPromises()
    expect(document.querySelector('[data-test="portal"]')?.classList.contains('shadcn-scope')).toBe(true)
    view.unmount()
  })

  it('RangeCalendar 可选择起止日期，并支持中文日期标签', async () => {
    const update = vi.fn()
    const view = mount(RangeCalendar, {
      props: {
        defaultPlaceholder: new CalendarDate(2026, 10, 1),
        locale: 'zh-CN',
        'onUpdate:modelValue': update,
      },
      attachTo: document.body,
    })
    await nextTick()
    const days = view.findAll<HTMLElement>('[role="button"][data-reka-calendar-cell-trigger]')
    const start = days.find(day => day.text() === '10' && day.attributes('data-outside-view') === undefined)
    const end = days.find(day => day.text() === '15' && day.attributes('data-outside-view') === undefined)
    expect(start).toBeDefined()
    expect(end).toBeDefined()
    await start!.trigger('click')
    await end!.trigger('mouseenter')
    await end!.trigger('click')
    const range = update.mock.calls.at(-1)?.[0]
    expect(range?.start.toString()).toBe('2026-10-10')
    expect(range?.end.toString()).toBe('2026-10-15')
    expect(start!.attributes('aria-label')).toContain('2026')
    view.unmount()
  })

  it('Dialog 关闭按钮使用现有 i18n 标签', async () => {
    // 与项目使用的 runtime-only vue-i18n 保持一致，提供预编译消息函数。
    const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: { common: { close: () => '关闭' } } } })
    const view = mount(defineComponent({
      setup: () => () => h(Dialog, { defaultOpen: true }, { default: () => h(DialogContent, {}, () => [
        h(DialogTitle, {}, () => '确认'),
        h(DialogDescription, {}, () => '操作说明'),
      ]) }),
    }), { attachTo: document.body, global: { plugins: [i18n] } })
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('关闭')
    view.unmount()
  })
})
