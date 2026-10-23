import { describe, expect, it, vi } from 'vitest'

const fakes = vi.hoisted(() => ({ os: 'android' }))
vi.mock('@wailsio/runtime', () => ({
  Window: {}, Dialogs: {},
  System: {
    Environment: async () => ({ OS: fakes.os }),
    IsMobile: () => false, IsWindows: () => false, IsMac: () => false, IsLinux: () => false,
  },
}))

describe('native environment bootstrap before mounting mobile UI', () => {
  it.each([
    ['android', true, false], ['ios', true, false], ['windows', false, true], ['linux', false, true],
  ])('uses the native %s result even when synchronous environment helpers are not initialized', async (os, mobile, desktop) => {
    vi.resetModules()
    fakes.os = os as string
    const runtime = await import('../src/api/runtime')
    await runtime.initializeRuntimeEnvironment()
    expect(runtime.isMobileRuntime()).toBe(mobile)
    expect(runtime.isDesktopRuntime()).toBe(desktop)
  })
})
