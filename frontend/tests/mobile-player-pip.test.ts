import { afterEach, expect, it, vi } from 'vitest'
import { enterDevicePip, readPlayerDevice } from '../src/api/mobilePlayer'
afterEach(() => vi.unstubAllGlobals())

it('uses the actual native entry to distinguish denied PiP permission from device support', () => {
  const enter = vi.fn(() => { throw Object.assign(new Error('PiP denied'), { code: 'PERMISSION_DENIED' }) })
  vi.stubGlobal('window',{wails:{playerState:()=>JSON.stringify({volume:.5,brightness:.5,pip:true,pipAllowed:false}),playerEnterPip:enter}})
  expect(readPlayerDevice()?.pip).toBe(true)
  expect(() => enterDevicePip(1920,1080)).toThrow(expect.objectContaining({code:'PERMISSION_DENIED'}))
  expect(enter).toHaveBeenCalledWith(1920,1080)
})

it('retains compatibility with hosts that report PiP capability without the new permission field', () => {
  const enter = vi.fn()
  vi.stubGlobal('window',{wails:{playerState:()=>JSON.stringify({volume:.5,brightness:.5,pip:true}),playerEnterPip:enter}})
  expect(enterDevicePip(1920,1080)).toBe(true)
  expect(enter).toHaveBeenCalledWith(1920,1080)
})
