import { expect, it, vi } from 'vitest'
const device = vi.hoisted(() => ({ available: false }))
vi.mock('../src/api/app', () => ({}))
vi.mock('../src/platform/presentation', () => ({ presentationProfile: () => ({ ui: 'mobile', host: 'native-mobile' }) }))
vi.mock('../src/api/mobilePlayer', () => ({
  readPlayerDevice: () => device.available ? { pip: true, volume: .5, brightness: .5 } : undefined,
  enterDevicePip: vi.fn(), restoreDeviceBrightness: vi.fn(), setDeviceVolume: vi.fn(), setCastMulticast: vi.fn(),
}))
import { createPlayerPlatform } from '../src/player/platform'

it('rechecks native PiP after the bridge becomes ready rather than freezing an unavailable bootstrap read', () => {
  device.available = false
  const platform = createPlayerPlatform()
  device.available = true
  // Native capability probing does not need a media element.
  expect(platform.device.read()?.pip).toBe(true)
  expect(platform.pip.supported()).toBe(true)
})
