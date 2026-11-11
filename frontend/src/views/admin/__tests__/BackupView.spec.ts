import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BackupView from '../BackupView.vue'

const { getS3Config, getImageStorageConfig, updateS3Config, updateImageStorageConfig, stepUpRun } = vi.hoisted(() => ({
  getS3Config: vi.fn().mockResolvedValue({ bucket: 'shared-bucket', access_key_id: 'key' }),
  getImageStorageConfig: vi.fn().mockResolvedValue({ config: { enabled: true, reuse_backup_s3: true }, secret_configured: true }),
  updateS3Config: vi.fn().mockResolvedValue({}),
  updateImageStorageConfig: vi.fn().mockResolvedValue({}),
  stepUpRun: vi.fn(async (operation: () => Promise<unknown>) => operation()),
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api', () => ({ adminAPI: { backup: {
  getS3Config, getImageStorageConfig, updateS3Config, updateImageStorageConfig,
  testS3Connection: vi.fn(), testImageStorageConnection: vi.fn(),
} } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

afterEach(() => vi.clearAllMocks())

describe('Object storage settings without database backup', () => {
  it('loads shared S3 and image storage without any backup operations', async () => {
    const wrapper = mount(BackupView, { global: { stubs: { TotpStepUpDialog: true } } })
    await flushPromises()
    expect(getS3Config).toHaveBeenCalledOnce()
    expect(getImageStorageConfig).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('admin.backup.s3.title')
    expect(wrapper.text()).toContain('admin.backup.imageStorage.title')
    expect(wrapper.text()).not.toContain('admin.backup.schedule.title')
    expect(wrapper.find('[data-testid="backup-schedule"]').exists()).toBe(false)
    expect(wrapper.find('table').exists()).toBe(false)
    const saveButtons = wrapper.findAll('button').filter(button => button.text() === 'common.save')
    expect(saveButtons).toHaveLength(2)
    await saveButtons[0].trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledOnce()
    expect(updateS3Config).toHaveBeenCalledOnce()
    await saveButtons[1].trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledTimes(2)
    expect(updateImageStorageConfig).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
})
