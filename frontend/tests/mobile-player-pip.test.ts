import { afterEach, expect, it, vi } from 'vitest'
import { enterDevicePip, readPlayerDevice } from '../src/api/mobilePlayer'
afterEach(() => vi.unstubAllGlobals())

it('distinguishes denied PiP permission from missing device support and does not invoke the native entry', () => {
  const enter = vi.fn()
  vi.stubGlobal('window',{wails:{playerState:()=>JSON.stringify({volume:.5,brightness:.5,pip:true,pipAllowed:false}),playerEnterPip:enter}})
  expect(readPlayerDevice()?.pip).toBe(true)
  expect(() => enterDevicePip(1920,1080)).toThrow(expect.objectContaining({code:'PERMISSION_DENIED'}))
  expect(enter).not.toHaveBeenCalled()
})

it('retains compatibility with hosts that report PiP capability without the new permission field', () => {
  const enter = vi.fn()
  vi.stubGlobal('window',{wails:{playerState:()=>JSON.stringify({volume:.5,brightness:.5,pip:true}),playerEnterPip:enter}})
  expect(enterDevicePip(1920,1080)).toBe(true)
  expect(enter).toHaveBeenCalledWith(1920,1080)
})
