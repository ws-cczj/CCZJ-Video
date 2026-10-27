import { createFullscreenPlatform } from '../../platform/fullscreen'
import { enterDevicePip, readPlayerDevice, restoreDeviceBrightness, setDeviceBrightness, setDeviceVolume, setCastMulticast, openSystemCastSettings } from '../../api/mobilePlayer'
import { presentationProfile } from '../../platform/presentation'
import { isIOSRuntime } from '../../api/runtime'

type WebKitVideo = HTMLVideoElement & {
  webkitSupportsPresentationMode?: (mode: string) => boolean
  webkitSetPresentationMode?: (mode: string) => void
  webkitPresentationMode?: string
}

/** Native calls and capability probes stay here. View size never selects an adapter.
 * Each player owns its adapter's previous maximized state; the session owns its
 * cancellation predicate and decides whether a late operation still belongs to it. */
export function createPlayerPlatform() {
  const profile = presentationProfile()
  const nativeDevice = profile.host === 'native-mobile'
  const ios = isIOSRuntime()
  const brightnessOwner = Symbol('player-brightness')
  return {
    profile,
    fullscreen: createFullscreenPlatform(),
    pip: {
      kind: nativeDevice && !ios ? 'device' as const : 'element' as const,
      active(video: HTMLVideoElement | null): boolean {
        return !!video && ((ios && (video as WebKitVideo).webkitPresentationMode === 'picture-in-picture')
          || document.pictureInPictureElement === video)
      },
      supported(video?: HTMLVideoElement): boolean {
        if (ios) return !!video && ((video as WebKitVideo).webkitSupportsPresentationMode?.('picture-in-picture') === true
          || (document.pictureInPictureEnabled === true && typeof video.requestPictureInPicture === 'function'))
        return nativeDevice ? readPlayerDevice()?.pip === true
          : !!video && document.pictureInPictureEnabled !== false && typeof video.requestPictureInPicture === 'function'
      },
      async enter(video: HTMLVideoElement): Promise<boolean> {
        if (ios) {
          const element = video as WebKitVideo
          if (element.webkitSupportsPresentationMode?.('picture-in-picture') && element.webkitSetPresentationMode) {
            element.webkitSetPresentationMode('picture-in-picture')
            return true
          }
          if (document.pictureInPictureEnabled !== true || typeof video.requestPictureInPicture !== 'function') return false
          await video.requestPictureInPicture()
          return true
        }
        if (nativeDevice) return enterDevicePip(video.videoWidth || 16, video.videoHeight || 9)
        if (typeof video.requestPictureInPicture !== 'function') return false
        await video.requestPictureInPicture()
        return true
      },
      async exit(video: HTMLVideoElement | null): Promise<void> {
        if (ios && (video as WebKitVideo | null)?.webkitPresentationMode === 'picture-in-picture') {
          (video as WebKitVideo).webkitSetPresentationMode?.('inline')
          return
        }
        if (video && document.pictureInPictureElement === video) await document.exitPictureInPicture()
      },
    },
    device: {
      volumeSupported: !ios,
      brightnessSupported: nativeDevice && !ios,
      read: () => nativeDevice ? readPlayerDevice() : undefined,
      setVolume: (value: number) => { if (nativeDevice) setDeviceVolume(Math.max(0, Math.min(1, value))) },
      setBrightness: (value: number) => nativeDevice && !ios && setDeviceBrightness(value, brightnessOwner),
      restoreBrightness: () => { if (nativeDevice) restoreDeviceBrightness(brightnessOwner) },
    },
    cast: { supported: profile.host !== 'browser' && !ios, multicast: (enabled: boolean) => { if (nativeDevice) setCastMulticast(enabled) },
      systemSupported:()=>nativeDevice && readPlayerDevice()?.castSettings===true,
      openSystem:()=>nativeDevice && openSystemCastSettings(),
    },
  }
}
export type PlayerPlatform = ReturnType<typeof createPlayerPlatform>
