import { createFullscreenPlatform } from '../../platform/fullscreen'
import { enterDevicePip, readPlayerDevice, restoreDeviceBrightness, setDeviceVolume, setCastMulticast } from '../../api/mobilePlayer'
import { presentationProfile } from '../../platform/presentation'

/** Native calls and capability probes stay here. View size never selects an adapter.
 * Each player owns its adapter's previous maximized state; the session owns its
 * cancellation predicate and decides whether a late operation still belongs to it. */
export function createPlayerPlatform() {
  const profile = presentationProfile()
  const nativeDevice = profile.host === 'native-mobile'
  return {
    profile,
    fullscreen: createFullscreenPlatform(),
    pip: {
      kind: nativeDevice ? 'device' as const : 'element' as const,
      supported(video?: HTMLVideoElement): boolean {
        return nativeDevice ? readPlayerDevice()?.pip === true
          : !!video && document.pictureInPictureEnabled !== false && typeof video.requestPictureInPicture === 'function'
      },
      async enter(video: HTMLVideoElement): Promise<boolean> {
        if (nativeDevice) return enterDevicePip(video.videoWidth || 16, video.videoHeight || 9)
        if (typeof video.requestPictureInPicture !== 'function') return false
        await video.requestPictureInPicture()
        return true
      },
      async exit(video: HTMLVideoElement | null): Promise<void> {
        if (video && document.pictureInPictureElement === video) await document.exitPictureInPicture()
      },
    },
    device: {
      read: () => nativeDevice ? readPlayerDevice() : undefined,
      setVolume: (value: number) => { if (nativeDevice) setDeviceVolume(Math.max(0, Math.min(1, value))) },
      restoreBrightness: () => { if (nativeDevice) restoreDeviceBrightness() },
    },
    cast: { supported: profile.host !== 'browser', multicast: (enabled: boolean) => { if (nativeDevice) setCastMulticast(enabled) } },
  }
}
export type PlayerPlatform = ReturnType<typeof createPlayerPlatform>
