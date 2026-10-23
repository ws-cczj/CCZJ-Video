import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'

const transport = vi.hoisted(() => ({ call: vi.fn() }))
vi.mock('@wailsio/runtime', async original => {
  const runtime = await original<typeof import('@wailsio/runtime')>()
  return { ...runtime, Call: { ...runtime.Call, ByID: transport.call } }
})
import { GetSetting, SetSetting } from '../src/api/app'

it('joins overlapping reads at the real binding adapter without merging writes', async () => {
  const pending = CancellablePromise.withResolvers<string>()
  transport.call.mockReset().mockImplementation(() => pending.promise)
  const first = GetSetting('theme_id'), second = GetSetting('theme_id')
  expect(transport.call).toHaveBeenCalledTimes(1)
  pending.resolve('dark')
  expect(await first).toBe('dark'); expect(await second).toBe('dark')
  transport.call.mockReset().mockImplementation(() => CancellablePromise.resolve())
  await Promise.all([SetSetting('theme_id', 'dark'), SetSetting('theme_id', 'dark')])
  expect(transport.call).toHaveBeenCalledTimes(2)
})
