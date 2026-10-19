import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import zhAdmin from '@/i18n/locales/zh/admin/enterprises'
import type { Enterprise } from '@/types/enterprise'
import EnterpriseMemberConcurrencyCard from '../components/EnterpriseMemberConcurrencyCard.vue'

const { updateMemberConcurrency, showSuccess, showError } = vi.hoisted(() => ({ updateMemberConcurrency: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin/enterprises', () => ({ enterprisesAPI: { updateMemberConcurrency } }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
const wrappers: VueWrapper[] = []
function render(value: number | null | undefined = null) {
  const enterprise = { id: 12, member_count: 5, member_concurrency: value } as Enterprise
  const wrapper = mount(EnterpriseMemberConcurrencyCard, { attachTo: document.body, props: { enterprise }, global: { plugins: [createI18n({
    legacy: false, locale: 'zh',
    // Vitest aliases vue-i18n to its runtime-only build, so compile the real messages here.
    messageCompiler: message => new Function(`return ${baseCompile(String(message)).code}`)(),
    messages: { zh: { admin: zhAdmin, enterprise: { cancel: '取消', errors: { generic: '操作失败' } }, common: { confirm: '确认' } } }
  })] } })
  wrappers.push(wrapper); return wrapper
}
function alert() { return new DOMWrapper(document.querySelector<HTMLElement>('[role="alertdialog"]')!) }
async function submit(wrapper: VueWrapper, input: string) {
  await wrapper.get('[data-test="member-concurrency-input"]').setValue(input)
  await wrapper.get('form').trigger('submit'); await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  updateMemberConcurrency.mockResolvedValue({ member_concurrency: 20, updated_count: 4 })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('administrator member concurrency card', () => {
  it('shows configured and platform default states and follows refreshed data', async () => {
    const wrapper = render()
    expect(wrapper.getComponent({ name: 'CardDescription' }).classes()).toContain('max-w-3xl')
    expect(wrapper.get('input').element.parentElement!.classList.contains('max-w-sm')).toBe(true)
    expect(wrapper.get('[data-test="member-concurrency-current"]').text()).toBe('未统一设置，新成员使用平台默认并发')
    expect(wrapper.get('[data-test="member-concurrency-clear"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ enterprise: { ...wrapper.props('enterprise'), member_concurrency: 30 } })
    expect(wrapper.get('[data-test="member-concurrency-current"]').text()).toBe('当前统一并发：30')
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('30')
    expect(wrapper.get('[data-test="member-concurrency-clear"]').attributes('disabled')).toBeUndefined()
  })
  it.each(['', '0', '-1', '1.5', '2147483648', 'Infinity', 'abc', '1e3'])('rejects invalid positive integer %s before confirmation', async value => {
    const wrapper = render()
    await submit(wrapper, value)
    expect(wrapper.get('[role="alert"]').text()).toBe('请输入 1～2147483647 之间的正整数。')
    expect(document.querySelector('[role="alertdialog"]')).toBeNull()
    expect(updateMemberConcurrency).not.toHaveBeenCalled()
  })
  it.each([1, 2147483647])('accepts inclusive boundary %s and requires confirmation', async value => {
    const wrapper = render()
    await submit(wrapper, String(value))
    expect(alert().text()).toContain(`5 名当前成员（含停用成员，不含已移出成员）的并发都改为 ${value}`)
    expect(alert().text()).toContain('之后新成员默认使用该值。确认后立即生效')
    expect(updateMemberConcurrency).not.toHaveBeenCalled()
    await alert().get('[data-test="enterprise-confirm-action"]').trigger('click'); await flushPromises()
    expect(updateMemberConcurrency).toHaveBeenCalledWith(12, { concurrency: value })
  })
  it('applies the confirmed numeric value, reports the actual updated count and asks the parent to refresh', async () => {
    const wrapper = render()
    await submit(wrapper, '20')
    await alert().get('[data-test="enterprise-confirm-action"]').trigger('click'); await flushPromises()
    expect(updateMemberConcurrency).toHaveBeenCalledWith(12, { concurrency: 20 })
    expect(showSuccess).toHaveBeenCalledWith('已更新 4 名成员')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(document.querySelector('[role="alertdialog"]')).toBeNull()
  })
  it('only sends null after clearing is confirmed and explains that current members are unchanged', async () => {
    updateMemberConcurrency.mockResolvedValue({ member_concurrency: null, updated_count: 0 })
    const wrapper = render(20)
    await wrapper.get('[data-test="member-concurrency-clear"]').trigger('click'); await flushPromises()
    expect(alert().text()).toContain('现有成员的并发不变，之后新成员回到平台默认并发')
    expect(updateMemberConcurrency).not.toHaveBeenCalled()
    await alert().get('[data-test="enterprise-confirm-action"]').trigger('click'); await flushPromises()
    expect(updateMemberConcurrency).toHaveBeenCalledWith(12, { concurrency: null })
    expect(showSuccess).toHaveBeenCalledWith('已清除统一设置，现有成员并发不变，新成员使用平台默认并发。')
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })
  it('cancels without writing and retains failed operations for retry', async () => {
    const wrapper = render(20)
    await submit(wrapper, '30')
    await alert().get('[data-test="enterprise-confirm-cancel"]').trigger('click'); await flushPromises()
    expect(updateMemberConcurrency).not.toHaveBeenCalled()
    updateMemberConcurrency.mockRejectedValueOnce({ code: 'ENTERPRISE_NOT_FOUND', status: 404 })
    await submit(wrapper, '30')
    await alert().get('[data-test="enterprise-confirm-action"]').trigger('click'); await flushPromises()
    expect(alert().find('[role="alert"]').exists()).toBe(true)
    expect(showSuccess).not.toHaveBeenCalled(); expect(wrapper.emitted('updated')).toBeUndefined()
    expect(showError).toHaveBeenCalledOnce()
  })
})
