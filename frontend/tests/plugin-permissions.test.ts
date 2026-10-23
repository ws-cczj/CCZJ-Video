import { beforeEach, describe, expect, it, vi } from 'vitest'
const fake = vi.hoisted(() => ({ GetSetting: vi.fn(async () => ''), SetSetting: vi.fn(async () => {}), WriteLog: vi.fn(async () => {}), confirm: vi.fn(async () => true) }))
vi.mock('../src/api/app', () => fake)
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))
vi.mock('../src/stores/confirm', () => ({ useConfirmStore: () => ({ confirm: fake.confirm }) }))
beforeEach(() => { vi.resetModules(); Object.values(fake).forEach(fn => fn.mockReset()); fake.GetSetting.mockResolvedValue(''); fake.SetSetting.mockResolvedValue(undefined); fake.WriteLog.mockResolvedValue(undefined); fake.confirm.mockResolvedValue(true) })
describe('cooperative plugin permission ledger', () => {
  it('prompts once for concurrent calls requiring the same permission', async () => {
    const permissions = await import('../src/plugins/permissions')
    permissions.registerPack('pack', 'Pack', ['write'])
    await Promise.all([permissions.requirePermission('pack', 'write', 'one'), permissions.requirePermission('pack', 'write', 'two')])
    expect(fake.confirm).toHaveBeenCalledOnce()
  })
  it('does not authorize a write when consent cannot be persisted', async () => {
    const permissions = await import('../src/plugins/permissions')
    permissions.registerPack('pack', 'Pack', ['write'])
    fake.SetSetting.mockRejectedValueOnce(new Error('disk full'))
    await expect(permissions.requirePermission('pack', 'write', 'action')).rejects.toThrow('disk full')
    expect(permissions.permissionDecision('pack', 'write')).toBe('')
  })
  it('treats a JSON null ledger as no grants without crashing', async () => {
    const permissions = await import('../src/plugins/permissions')
    fake.GetSetting.mockResolvedValueOnce('null')
    await permissions.loadPermissionLedger()
    expect(permissions.permissionDecision('pack', 'write')).toBe('')
  })
  it('does not grant a pending action after its runtime is disposed', async () => {
    const permissions = await import('../src/plugins/permissions')
    const unregister = permissions.registerPack('pack', 'Pack', ['write'])
    let answer!: (value: boolean) => void
    fake.confirm.mockReturnValueOnce(new Promise(resolve => { answer = resolve }))
    const pending = permissions.requirePermission('pack', 'write', 'action')
    await vi.waitFor(() => expect(fake.confirm).toHaveBeenCalled())
    unregister()
    answer(true)
    await expect(pending).rejects.toThrow('no longer active')
    expect(fake.SetSetting).not.toHaveBeenCalled()
  })
})
